package account_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/account"
	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/storage/storagetest"
)

var start = time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC)

func ptr(s string) *string { return &s }

func alice() account.Identity {
	return account.Identity{
		Issuer:      "https://id.example.com",
		Subject:     "sub-alice",
		Email:       ptr("alice@example.com"),
		DisplayName: ptr("Alice"),
	}
}

// count runs a SELECT count(*) and returns the result.
func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func currentSeq(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var seq int64
	if err := pool.QueryRow(context.Background(), "SELECT seq FROM sync_state").Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

func TestFirstLoginCreatesUserAndInbox(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	svc := account.NewService(pool, clock.NewFixed(start))

	userID, err := svc.SignIn(ctx, alice())
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	var email, name, timezone, reminderTime string
	var createdAt time.Time
	err = pool.QueryRow(ctx, `SELECT email, display_name, timezone, to_char(all_day_reminder_time, 'HH24:MI'), created_at
		FROM users WHERE id = $1`, userID).Scan(&email, &name, &timezone, &reminderTime, &createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if email != "alice@example.com" || name != "Alice" {
		t.Errorf("claims = %q, %q", email, name)
	}
	// SPEC section 4 defaults.
	if timezone != "Europe/Madrid" || reminderTime != "09:00" {
		t.Errorf("defaults = %q, %q, want Europe/Madrid, 09:00", timezone, reminderTime)
	}
	if !createdAt.Equal(start) {
		t.Errorf("created_at = %v, want the injected clock's %v", createdAt, start)
	}

	// One inbox, owned through list_members, as a fresh syncable row (D-07).
	var listName, position, role string
	var version int
	var seq int64
	var listCreated time.Time
	err = pool.QueryRow(ctx, `SELECT l.name, l.position, l.version, l.seq, l.created_at, m.role
		FROM lists l JOIN list_members m ON m.list_id = l.id
		WHERE l.owner_id = $1 AND l.is_inbox AND m.user_id = $1`, userID).
		Scan(&listName, &position, &version, &seq, &listCreated, &role)
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if listName != "Inbox" || position != "a0" || role != "owner" {
		t.Errorf("inbox = %q at %q with role %q", listName, position, role)
	}
	if version != 1 {
		t.Errorf("inbox version = %d, want 1", version)
	}
	if seq < 1 || seq != currentSeq(t, pool) {
		t.Errorf("inbox seq = %d, want the newly taken %d", seq, currentSeq(t, pool))
	}
	if !listCreated.Equal(start) {
		t.Errorf("inbox created_at = %v, want %v", listCreated, start)
	}
}

// Logging in again is idempotent: same user, still one inbox, and no write
// to any syncable resource (the seq counter does not move). The
// informational claims follow the provider.
func TestLaterLoginRefreshesClaimsOnly(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	clk := clock.NewFixed(start)
	svc := account.NewService(pool, clk)

	first, err := svc.SignIn(ctx, alice())
	if err != nil {
		t.Fatal(err)
	}
	seqAfterFirst := currentSeq(t, pool)

	// Same claims, a day later: nothing changes, not even updated_at.
	clk.Advance(24 * time.Hour)
	second, err := svc.SignIn(ctx, alice())
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("second login returned user %v, want %v", second, first)
	}
	var updatedAt time.Time
	if err := pool.QueryRow(ctx, "SELECT updated_at FROM users WHERE id = $1", first).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if !updatedAt.Equal(start) {
		t.Errorf("updated_at = %v after a login with unchanged claims, want %v", updatedAt, start)
	}

	// New email and name at the provider, another day later.
	clk.Advance(24 * time.Hour)
	changed := alice()
	changed.Email = ptr("alice@new.example.com")
	changed.DisplayName = nil
	if _, err := svc.SignIn(ctx, changed); err != nil {
		t.Fatal(err)
	}
	var email string
	var name *string
	err = pool.QueryRow(ctx, "SELECT email, display_name, updated_at FROM users WHERE id = $1", first).
		Scan(&email, &name, &updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if email != "alice@new.example.com" || name != nil {
		t.Errorf("claims = %q, %v; want the new email and no name", email, name)
	}
	if want := start.Add(48 * time.Hour); !updatedAt.Equal(want) {
		t.Errorf("updated_at = %v, want %v", updatedAt, want)
	}

	if n := count(t, pool, "SELECT count(*) FROM users"); n != 1 {
		t.Errorf("%d users, want 1", n)
	}
	if n := count(t, pool, "SELECT count(*) FROM lists"); n != 1 {
		t.Errorf("%d lists, want 1", n)
	}
	if seq := currentSeq(t, pool); seq != seqAfterFirst {
		t.Errorf("seq moved from %d to %d on later logins", seqAfterFirst, seq)
	}
}

// D-14: the user is (issuer, subject). The same email under another
// subject, or the same subject at another issuer, is another user.
func TestIdentityIsIssuerAndSubject(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	svc := account.NewService(pool, clock.NewFixed(start))

	base, err := svc.SignIn(ctx, alice())
	if err != nil {
		t.Fatal(err)
	}

	otherSubject := alice()
	otherSubject.Subject = "sub-impostor"
	otherIssuer := alice()
	otherIssuer.Issuer = "https://other-id.example.com"

	for _, identity := range []account.Identity{otherSubject, otherIssuer} {
		id, err := svc.SignIn(ctx, identity)
		if err != nil {
			t.Fatal(err)
		}
		if id == base {
			t.Errorf("identity %s/%s resolved to the existing user", identity.Issuer, identity.Subject)
		}
	}
	if n := count(t, pool, "SELECT count(*) FROM lists WHERE is_inbox"); n != 3 {
		t.Errorf("%d inboxes, want 3", n)
	}
}

// Two tabs finishing the first login at the same moment must not create two
// users or two inboxes.
func TestConcurrentFirstLogins(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	svc := account.NewService(pool, clock.NewFixed(start))

	const logins = 10
	ids := make([]uuid.UUID, logins)
	errs := make([]error, logins)
	var wg sync.WaitGroup
	for i := range logins {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids[i], errs[i] = svc.SignIn(ctx, alice())
		}()
	}
	wg.Wait()

	for i := range logins {
		if errs[i] != nil {
			t.Fatalf("login %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Errorf("login %d returned %v, login 0 returned %v", i, ids[i], ids[0])
		}
	}
	if n := count(t, pool, "SELECT count(*) FROM users"); n != 1 {
		t.Errorf("%d users, want 1", n)
	}
	if n := count(t, pool, "SELECT count(*) FROM lists"); n != 1 {
		t.Errorf("%d lists, want 1", n)
	}
	if n := count(t, pool, "SELECT count(*) FROM list_members"); n != 1 {
		t.Errorf("%d memberships, want 1", n)
	}
	if seq := currentSeq(t, pool); seq != 1 {
		t.Errorf("seq = %d, want 1: only one inbox was written", seq)
	}
}

// Sign-ups of different users at the same time each take their own seq,
// with no gaps (D-07).
func TestConcurrentSignUpsTakeDistinctSeqs(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	svc := account.NewService(pool, clock.NewFixed(start))

	const users = 10
	errs := make([]error, users)
	var wg sync.WaitGroup
	for i := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			identity := alice()
			identity.Subject = fmt.Sprintf("sub-%d", i)
			_, errs[i] = svc.SignIn(ctx, identity)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("sign-up %d: %v", i, err)
		}
	}

	rows, err := pool.Query(ctx, "SELECT seq FROM lists ORDER BY seq")
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, seq)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if !slices.Equal(seqs, want) {
		t.Errorf("inbox seqs = %v, want %v", seqs, want)
	}
}
