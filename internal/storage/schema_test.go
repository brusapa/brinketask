package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/brusapa/brinketask/internal/storage/storagetest"
)

// The extension behind accent-insensitive search (D-42) is installed.
func TestUnaccentIsAvailable(t *testing.T) {
	pool := storagetest.NewPool(t)
	var got string
	if err := pool.QueryRow(context.Background(), "SELECT lower(unaccent('Árbol Ñandú'))").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "arbol nandu" {
		t.Errorf("unaccent = %q, want %q", got, "arbol nandu")
	}
}

// A live tag name is unique per user ignoring case; a deleted tag frees it.
func TestTagNamesAreUniquePerUser(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	exec := func(sql string) error {
		_, err := pool.Exec(ctx, sql)
		return err
	}
	for _, sql := range []string{
		`INSERT INTO users (id, oidc_issuer, oidc_subject, created_at, updated_at)
		 VALUES ('00000000-0000-7000-8000-000000000001', 'i', 'a', now(), now()),
		        ('00000000-0000-7000-8000-000000000002', 'i', 'b', now(), now())`,
		`INSERT INTO tags (id, owner_id, name, version, seq, created_at, updated_at)
		 VALUES ('00000000-0000-7000-8000-0000000000a1', '00000000-0000-7000-8000-000000000001', 'Work', 1, 1, now(), now())`,
	} {
		if err := exec(sql); err != nil {
			t.Fatal(err)
		}
	}

	insert := func(id, owner, name string, seq int) error {
		_, err := pool.Exec(ctx, `INSERT INTO tags (id, owner_id, name, version, seq, created_at, updated_at)
			VALUES ($1, $2, $3, 1, $4, now(), now())`, id, owner, name, seq)
		return err
	}
	if err := insert("00000000-0000-7000-8000-0000000000a2", "00000000-0000-7000-8000-000000000001", "WORK", 2); err == nil {
		t.Error("a second live tag named WORK was accepted for the same user")
	}
	if err := insert("00000000-0000-7000-8000-0000000000a3", "00000000-0000-7000-8000-000000000002", "work", 3); err != nil {
		t.Errorf("another user's tag with the same name rejected: %v", err)
	}
	if err := exec(`UPDATE tags SET deleted_at = now() WHERE id = '00000000-0000-7000-8000-0000000000a1'`); err != nil {
		t.Fatal(err)
	}
	if err := insert("00000000-0000-7000-8000-0000000000a4", "00000000-0000-7000-8000-000000000001", "work", 4); err != nil {
		t.Errorf("name of a deleted tag not reusable: %v", err)
	}
}

// Positions order by code point whatever the database's locale (D-51): the
// collation is fixed on the columns.
func TestPositionsCompareByCodePoint(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)

	rows, err := pool.Query(ctx, `
		SELECT c.relname, coll.collname
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_collation coll ON coll.oid = a.attcollation
		WHERE a.attname = 'position' AND c.relname IN ('lists', 'tasks', 'checklist_items')
		ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for rows.Next() {
		var table, collation string
		if err := rows.Scan(&table, &collation); err != nil {
			t.Fatal(err)
		}
		got[table] = collation
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"lists", "tasks", "checklist_items"} {
		if got[table] != "C" {
			t.Errorf("%s.position collation = %q, want C", table, got[table])
		}
	}

	// In code point order uppercase letters come before lowercase ones; a
	// linguistic collation would put "a0" first.
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, oidc_issuer, oidc_subject, created_at, updated_at)
		VALUES ('00000000-0000-7000-8000-000000000001', 'i', 'a', now(), now());
		INSERT INTO lists (id, owner_id, name, position, is_inbox, version, seq, created_at, updated_at) VALUES
		  ('00000000-0000-7000-8000-0000000000b1', '00000000-0000-7000-8000-000000000001', 'x', 'a0', false, 1, 1, now(), now()),
		  ('00000000-0000-7000-8000-0000000000b2', '00000000-0000-7000-8000-000000000001', 'y', 'Zz', false, 1, 2, now(), now()),
		  ('00000000-0000-7000-8000-0000000000b3', '00000000-0000-7000-8000-000000000001', 'z', 'a00', false, 1, 3, now(), now())`)
	if err != nil {
		t.Fatal(err)
	}
	var order string
	if err := pool.QueryRow(ctx, "SELECT string_agg(position, ' ' ORDER BY position) FROM lists").Scan(&order); err != nil {
		t.Fatal(err)
	}
	if order != "Zz a0 a00" {
		t.Errorf("order = %q, want %q", order, "Zz a0 a00")
	}
}

// A recurring task always has the start of its series (D-56).
func TestRecurringTaskNeedsSeriesStart(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, oidc_issuer, oidc_subject, created_at, updated_at)
		VALUES ('00000000-0000-7000-8000-000000000001', 'i', 'a', now(), now());
		INSERT INTO lists (id, owner_id, name, position, is_inbox, version, seq, created_at, updated_at)
		VALUES ('00000000-0000-7000-8000-0000000000b1', '00000000-0000-7000-8000-000000000001', 'x', 'a0', false, 1, 1, now(), now())`)
	if err != nil {
		t.Fatal(err)
	}
	insert := func(seq int, start *string) error {
		_, err := pool.Exec(ctx, `INSERT INTO tasks (id, list_id, title, status, priority, position, due_date, rrule,
			recurrence_start, repeat_from, version, seq, created_at, updated_at)
			VALUES (gen_random_uuid(), '00000000-0000-7000-8000-0000000000b1', 't', 'open', 0, 'a0',
			'2026-10-09', 'FREQ=DAILY', $2::date, 'due', 1, $1, now(), now())`, seq, start)
		return err
	}
	if err := insert(2, nil); err == nil || !strings.Contains(err.Error(), "tasks_rrule_needs_start") {
		t.Errorf("a rule without a series start: err = %v", err)
	}
	start := "2026-10-09"
	if err := insert(3, &start); err != nil {
		t.Errorf("a rule with a series start: %v", err)
	}
}
