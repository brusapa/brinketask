package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// env returns a LookupFunc backed by a map, standing in for the process environment.
func env(vars map[string]string) LookupFunc {
	return func(key string) (string, bool) {
		value, ok := vars[key]
		return value, ok
	}
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": "postgres://db"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL != "postgres://db" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080", cfg.ListenAddr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want INFO", cfg.LogLevel)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"DATABASE_URL": "postgres://db",
		"LISTEN_ADDR":  "127.0.0.1:9000",
		"LOG_LEVEL":    "debug",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:9000" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want DEBUG", cfg.LogLevel)
	}
}

func TestLoadDatabaseURLFromFile(t *testing.T) {
	// The trailing newline is what `echo url > file` produces; it must not
	// end up in the value.
	path := writeFile(t, "postgres://from-file\n")
	cfg, err := Load(env(map[string]string{"DATABASE_URL_FILE": path}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL != "postgres://from-file" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestLoadErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	tests := []struct {
		name    string
		vars    map[string]string
		wantErr string
	}{
		{
			name:    "database URL missing",
			vars:    map[string]string{},
			wantErr: "DATABASE_URL or DATABASE_URL_FILE is required",
		},
		{
			name:    "database URL empty",
			vars:    map[string]string{"DATABASE_URL": ""},
			wantErr: "DATABASE_URL or DATABASE_URL_FILE is required",
		},
		{
			name: "both value and file",
			vars: map[string]string{
				"DATABASE_URL":      "postgres://db",
				"DATABASE_URL_FILE": "/run/secrets/db",
			},
			wantErr: "set only one of DATABASE_URL and DATABASE_URL_FILE",
		},
		{
			name:    "unreadable file",
			vars:    map[string]string{"DATABASE_URL_FILE": missing},
			wantErr: "DATABASE_URL_FILE",
		},
		{
			name:    "invalid log level",
			vars:    map[string]string{"DATABASE_URL": "postgres://db", "LOG_LEVEL": "loud"},
			wantErr: "LOG_LEVEL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(tt.vars))
			if err == nil {
				t.Fatalf("Load succeeded, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
