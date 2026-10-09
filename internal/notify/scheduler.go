package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/storage/dbgen"
	"github.com/brusapa/brinketask/internal/tasks"
	"github.com/brusapa/brinketask/internal/webpush"
)

// Delivery statuses (SPEC section 4).
const (
	statusPending = "pending"
	statusSkipped = "skipped"
)

// maxAttempts is how often a delivery is tried before it is failed (D-66).
const maxAttempts = 5

// firstRetry is the wait after the first failure; each later one doubles:
// 30 s, 1 min, 2 min, 4 min (D-66).
const firstRetry = 30 * time.Second

// batch is how many reminders one transaction fires.
const batch = 100

// Scheduler turns due reminders into notifications (SPEC section 6, D-16):
// it polls PostgreSQL, so it needs no broker, and every claim skips rows
// another scheduler holds, so several replicas can run it at once.
type Scheduler struct {
	pool        *pgxpool.Pool
	clock       clock.Clock
	tasks       *tasks.Service
	sender      Sender
	logger      *slog.Logger
	maxLateness time.Duration
}

// NewScheduler returns a scheduler that skips reminders more than
// maxLateness late.
func NewScheduler(pool *pgxpool.Pool, clk clock.Clock, taskService *tasks.Service, sender Sender,
	maxLateness time.Duration, logger *slog.Logger,
) *Scheduler {
	return &Scheduler{pool: pool, clock: clk, tasks: taskService, sender: sender, logger: logger, maxLateness: maxLateness}
}

// Run ticks every interval until ctx ends. A failed tick is logged and the
// next one tries again.
func (s *Scheduler) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			s.logger.Error("scheduler tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick fires the reminders that are due, then sends the deliveries that
// are due, new ones and retries.
func (s *Scheduler) Tick(ctx context.Context) error {
	fired, err := s.fireDue(ctx)
	if err != nil {
		return err
	}
	sent, err := s.sendDue(ctx)
	if fired > 0 || sent > 0 {
		s.logger.Debug("scheduler tick", "fired", fired, "deliveries", sent)
	}
	return err
}

// fireDue claims due reminders in batches. For each it creates one
// delivery per active device of the members of its list, or a skipped one
// when it is too late (SPEC section 6), and records it as fired. A
// reminder whose user has no device is consumed all the same (D-70).
func (s *Scheduler) fireDue(ctx context.Context) (int, error) {
	total := 0
	for {
		n := 0
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			q := dbgen.New(tx)
			now := s.clock.Now()
			due, err := q.ClaimDueReminders(ctx, dbgen.ClaimDueRemindersParams{Now: &now, Lim: batch})
			if err != nil {
				return err
			}
			n = len(due)
			for _, r := range due {
				if err := s.fire(ctx, q, r, now); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return total, fmt.Errorf("notify: fire reminders: %w", err)
		}
		total += n
		if n < batch {
			return total, nil
		}
	}
}

func (s *Scheduler) fire(ctx context.Context, q *dbgen.Queries, r dbgen.Reminder, now time.Time) error {
	fireAt := *r.NextFireAt
	devices, err := q.ActiveSubscriptionsForTask(ctx, r.TaskID)
	if err != nil {
		return err
	}
	status, next := statusPending, &now
	if now.Sub(fireAt) > s.maxLateness {
		// After an outage: a reminder this old is no use (SPEC section 6).
		status, next = statusSkipped, nil
	}
	for _, device := range devices {
		err := q.InsertDelivery(ctx, dbgen.InsertDeliveryParams{
			ReminderID: r.ID, FireAt: fireAt, SubscriptionID: device, Status: status, NextAttemptAt: next, Now: now,
		})
		if err != nil {
			return err
		}
	}
	return s.tasks.FireReminder(ctx, q, r)
}

// sendDue sends due deliveries one per transaction, so the row stays
// locked while its push service answers and a slow one holds up no other.
func (s *Scheduler) sendDue(ctx context.Context) (int, error) {
	total := 0
	for {
		found := false
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			q := dbgen.New(tx)
			now := s.clock.Now()
			d, err := q.ClaimDueDelivery(ctx, &now)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			found = true
			return s.send(ctx, q, d)
		})
		if err != nil {
			return total, fmt.Errorf("notify: send deliveries: %w", err)
		}
		if !found {
			return total, nil
		}
		total++
	}
}

func (s *Scheduler) send(ctx context.Context, q *dbgen.Queries, d dbgen.ClaimDueDeliveryRow) error {
	payload, err := reminderPayload(d.TaskID, d.ReminderID, d.Title, d.ListName, d.IsInbox,
		d.DueDate, d.DueTime, d.DueTz, d.Timezone, d.FireAt)
	if err != nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	sendErr := s.sender.Send(sendCtx, webpush.Subscription{Endpoint: d.Endpoint, P256dh: d.P256dh, Auth: d.Auth}, payload)
	now := s.clock.Now()
	if sendErr == nil {
		if err := q.MarkDeliverySent(ctx, dbgen.MarkDeliverySentParams{Now: &now, ID: d.ID}); err != nil {
			return err
		}
		return q.SubscriptionSucceeded(ctx, dbgen.SubscriptionSucceededParams{Now: &now, ID: d.SubscriptionID})
	}

	// Only the push service's answer is kept and logged, never the payload.
	message := sendErr.Error()
	attempts := int(d.Attempts) + 1
	switch {
	case errors.Is(sendErr, webpush.ErrGone):
		// The browser unsubscribed: no retry, and no more deliveries.
		s.logger.Info("device gone, disabling it", "device", d.SubscriptionID)
		if err := q.DisableSubscription(ctx, dbgen.DisableSubscriptionParams{Now: &now, ID: d.SubscriptionID}); err != nil {
			return err
		}
		return q.MarkDeliveryFailed(ctx, dbgen.MarkDeliveryFailedParams{Error: &message, ID: d.ID})
	case attempts >= maxAttempts:
		s.logger.Warn("delivery failed", "delivery", d.ID, "attempts", attempts, "error", message)
		return q.MarkDeliveryFailed(ctx, dbgen.MarkDeliveryFailedParams{Error: &message, ID: d.ID})
	default:
		next := now.Add(firstRetry << (attempts - 1))
		return q.MarkDeliveryRetry(ctx, dbgen.MarkDeliveryRetryParams{NextAttemptAt: &next, Error: &message, ID: d.ID})
	}
}
