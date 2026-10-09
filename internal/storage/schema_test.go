package storage_test

import (
	"context"
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
