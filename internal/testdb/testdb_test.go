package testdb

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// connect opens a connection to url and closes it when the test ends.
func connect(t *testing.T, url string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// exists reports whether the server has a database with that name.
func exists(t *testing.T, name string) bool {
	t.Helper()
	server, err := ServerURL()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	err = connect(t, server).QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&found)
	if err != nil {
		t.Fatalf("look up database: %v", err)
	}
	return found
}

func TestCreateGivesDistinctEmptyDatabasesAndDropRemovesThem(t *testing.T) {
	ctx := context.Background()
	first, dropFirst, err := Create(ctx, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, dropSecond, err := Create(ctx, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if first.Name == second.Name {
		t.Fatalf("both databases are named %q", first.Name)
	}

	// The URL leads to the database of that name, which has no tables.
	conn := connect(t, first.URL)
	var current string
	var tables int
	err = conn.QueryRow(ctx, `SELECT current_database(),
		(SELECT count(*) FROM pg_tables WHERE schemaname NOT IN ('pg_catalog', 'information_schema'))`).
		Scan(&current, &tables)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if current != first.Name || tables != 0 {
		t.Errorf("connected to %q with %d tables, want %q with 0", current, tables, first.Name)
	}

	// Dropping works with a connection still open (WITH (FORCE)).
	dropFirst()
	dropSecond()
	if exists(t, first.Name) || exists(t, second.Name) {
		t.Errorf("databases still exist after drop")
	}
}

func TestCreateCopiesTheTemplate(t *testing.T) {
	ctx := context.Background()
	template, dropTemplate, err := Create(ctx, "")
	if err != nil {
		t.Fatalf("Create template: %v", err)
	}
	t.Cleanup(dropTemplate)
	conn := connect(t, template.URL)
	if _, err := conn.Exec(ctx, "CREATE TABLE marker (n int); INSERT INTO marker VALUES (42)"); err != nil {
		t.Fatalf("fill template: %v", err)
	}
	// A database is a template only while nobody is connected to it.
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	clone, dropClone, err := Create(ctx, template.Name)
	if err != nil {
		t.Fatalf("Create from template: %v", err)
	}
	t.Cleanup(dropClone)
	var n int
	if err := connect(t, clone.URL).QueryRow(ctx, "SELECT n FROM marker").Scan(&n); err != nil {
		t.Fatalf("read clone: %v", err)
	}
	if n != 42 {
		t.Errorf("clone holds %d, want 42", n)
	}
}

func TestStartDropsTheDatabaseAfterTheTest(t *testing.T) {
	var name string
	t.Run("uses a database", func(t *testing.T) {
		err := connect(t, Start(t)).QueryRow(context.Background(), "SELECT current_database()").Scan(&name)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
	})
	if name == "" || exists(t, name) {
		t.Errorf("database %q still exists after its test", name)
	}
}

// Without TEST_DATABASE_URL nothing is created and the error tells how to
// run the tests; Start turns it into a test failure (t.Fatalf), never a
// skip.
func TestWithoutServerTheErrorExplainsWhatToDo(t *testing.T) {
	// t.Setenv restores the variable when the test ends.
	t.Setenv(EnvVar, "")
	_, drop, err := Create(context.Background(), "")
	drop() // must be safe to call after an error
	if err == nil {
		t.Fatal("Create succeeded without a server")
	}
	for _, want := range []string{EnvVar, "with-test-postgres"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
