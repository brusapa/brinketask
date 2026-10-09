// Package account owns users: automatic sign-up on the first login
// (SPEC section 7) and the profile served by /me.
package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// The inbox is created by the server, so its fields cannot come from the
// client. Its name is an internal value: the web client always shows its
// own translated label for the list with is_inbox set, which keeps
// user-facing text out of the server (CLAUDE.md, i18n). "a0" is the first
// key of the usual fractional-index scheme (D-12).
const (
	inboxName     = "Inbox"
	inboxPosition = "a0"
)

// Identity is who the OIDC provider says the user is. Issuer and Subject
// identify the user (D-14); Email and DisplayName are informational and
// may be nil.
type Identity struct {
	Issuer      string
	Subject     string
	Email       *string
	DisplayName *string
}

// Service reads and writes users.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
	// settingsChanged runs in the transaction of UpdateSettings: pending
	// reminders depend on the zone and default time (SPEC section 6).
	settingsChanged SettingsHook
}

// SettingsHook is called, in the same transaction, after a user's
// settings change.
type SettingsHook func(ctx context.Context, q *dbgen.Queries, userID uuid.UUID) error

// OnSettingsChanged sets the hook UpdateSettings calls. main wires it to
// the reminder recomputation of package tasks, which this package does not
// import.
func (s *Service) OnSettingsChanged(hook SettingsHook) {
	s.settingsChanged = hook
}

// NewService returns a Service backed by pool, reading time from clk.
func NewService(pool *pgxpool.Pool, clk clock.Clock) *Service {
	return &Service{pool: pool, clock: clk}
}

// SignIn returns the user with the given identity, creating the user, their
// inbox and their owner membership on the first login, all in one
// transaction. On later logins it refreshes email and display name.
//
// It is idempotent and safe under concurrency: the upsert locks the user's
// row, so a second concurrent login of the same user waits, then finds the
// inbox already there.
func (s *Service) SignIn(ctx context.Context, identity Identity) (uuid.UUID, error) {
	now := s.clock.Now()
	var userID uuid.UUID

	// pgx.BeginFunc commits if the function returns nil and rolls back
	// otherwise.
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := dbgen.New(tx)
		var err error
		userID, err = q.UpsertUser(ctx, dbgen.UpsertUserParams{
			OidcIssuer:  identity.Issuer,
			OidcSubject: identity.Subject,
			Email:       identity.Email,
			DisplayName: identity.DisplayName,
			Now:         now,
		})
		if err != nil {
			return fmt.Errorf("upsert user: %w", err)
		}

		_, err = q.GetInboxID(ctx, userID)
		switch {
		case err == nil:
			return nil // returning user
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("find inbox: %w", err)
		}
		return createInbox(ctx, q, userID, now)
	})
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("account: sign in: %w", err)
	}
	return userID, nil
}

// createInbox adds the user's inbox and their owner membership. The list is
// a syncable resource, so it starts at version 1 with a new seq (D-07).
func createInbox(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, now time.Time) error {
	listID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("inbox id: %w", err)
	}
	seq, err := q.NextSeq(ctx)
	if err != nil {
		return fmt.Errorf("next seq: %w", err)
	}
	err = q.InsertList(ctx, dbgen.InsertListParams{
		ID:       listID,
		OwnerID:  userID,
		Name:     inboxName,
		Position: inboxPosition,
		IsInbox:  true,
		Seq:      seq,
		Now:      now,
	})
	if err != nil {
		return fmt.Errorf("insert inbox: %w", err)
	}
	err = q.InsertListMember(ctx, dbgen.InsertListMemberParams{ListID: listID, UserID: userID, Role: "owner"})
	if err != nil {
		return fmt.Errorf("insert inbox member: %w", err)
	}
	return nil
}
