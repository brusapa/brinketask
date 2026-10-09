package config

import (
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brusapa/brinketask/internal/webpush"
)

// env returns a LookupFunc backed by a map, standing in for the process environment.
func env(vars map[string]string) LookupFunc {
	return func(key string) (string, bool) {
		value, ok := vars[key]
		return value, ok
	}
}

// A VAPID key pair made once for the package's tests.
var testPublicKey, testPrivateKey = func() (string, string) {
	public, private, err := webpush.GenerateKeys()
	if err != nil {
		panic(err)
	}
	return public, private
}()

// required returns the smallest valid environment, plus overrides. An
// override with the value "<unset>" removes the variable.
func required(overrides map[string]string) map[string]string {
	vars := map[string]string{
		"DATABASE_URL":       "postgres://db",
		"PUBLIC_URL":         "https://tasks.example.com",
		"OIDC_ISSUER":        "https://id.example.com",
		"OIDC_CLIENT_ID":     "brinketask",
		"OIDC_CLIENT_SECRET": "client-secret-for-tests",
		"VAPID_PUBLIC_KEY":   testPublicKey,
		"VAPID_PRIVATE_KEY":  testPrivateKey,
		"VAPID_SUBJECT":      "mailto:ops@example.com",
	}
	maps.Copy(vars, overrides)
	for name, value := range vars {
		if value == "<unset>" {
			delete(vars, name)
		}
	}
	return vars
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
	cfg, err := Load(env(required(nil)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL != "postgres://db" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080", cfg.ListenAddr)
	}
	if cfg.MetricsListenAddr != ":9090" {
		t.Errorf("MetricsListenAddr = %q, want :9090", cfg.MetricsListenAddr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want INFO", cfg.LogLevel)
	}
	if cfg.PublicURL != "https://tasks.example.com" {
		t.Errorf("PublicURL = %q", cfg.PublicURL)
	}
	want := OIDC{Issuer: "https://id.example.com", ClientID: "brinketask", ClientSecret: "client-secret-for-tests"}
	if cfg.OIDC != want {
		t.Errorf("OIDC = %+v, want %+v", cfg.OIDC, want)
	}
	// D-29: 7 days idle, 30 days in total.
	if cfg.SessionIdleTimeout != 168*time.Hour {
		t.Errorf("SessionIdleTimeout = %v, want 168h", cfg.SessionIdleTimeout)
	}
	if cfg.SessionMaxAge != 720*time.Hour {
		t.Errorf("SessionMaxAge = %v, want 720h", cfg.SessionMaxAge)
	}
	// D-29: polls every 15 s; 12 h of lateness at most.
	if cfg.SchedulerInterval != 15*time.Second || cfg.ReminderMaxLateness != 12*time.Hour {
		t.Errorf("scheduler %v, lateness %v", cfg.SchedulerInterval, cfg.ReminderMaxLateness)
	}
	if cfg.Push.Keys.Public != testPublicKey || cfg.Push.Subject != "mailto:ops@example.com" {
		t.Errorf("Push = %+v", cfg.Push)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(required(map[string]string{
		"LISTEN_ADDR":           "127.0.0.1:9000",
		"LOG_LEVEL":             "debug",
		"SESSION_IDLE_TIMEOUT":  "90m",
		"SESSION_MAX_AGE":       "48h",
		"SCHEDULER_INTERVAL":    "5s",
		"REMINDER_MAX_LATENESS": "1h",
	})))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:9000" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want DEBUG", cfg.LogLevel)
	}
	if cfg.SessionIdleTimeout != 90*time.Minute {
		t.Errorf("SessionIdleTimeout = %v, want 90m", cfg.SessionIdleTimeout)
	}
	if cfg.SessionMaxAge != 48*time.Hour {
		t.Errorf("SessionMaxAge = %v, want 48h", cfg.SessionMaxAge)
	}
	if cfg.SchedulerInterval != 5*time.Second || cfg.ReminderMaxLateness != time.Hour {
		t.Errorf("scheduler %v, lateness %v", cfg.SchedulerInterval, cfg.ReminderMaxLateness)
	}
}

// Every secret of SPEC section 10 that this phase reads accepts _FILE.
func TestLoadSecretsFromFiles(t *testing.T) {
	// The trailing newline is what `echo value > file` produces; it must
	// not end up in the value.
	cfg, err := Load(env(required(map[string]string{
		"DATABASE_URL":            "<unset>",
		"DATABASE_URL_FILE":       writeFile(t, "postgres://from-file\n"),
		"OIDC_CLIENT_ID":          "<unset>",
		"OIDC_CLIENT_ID_FILE":     writeFile(t, "id-from-file\n"),
		"OIDC_CLIENT_SECRET":      "<unset>",
		"OIDC_CLIENT_SECRET_FILE": writeFile(t, "secret-from-file\r\n"),
		"VAPID_PRIVATE_KEY":       "<unset>",
		"VAPID_PRIVATE_KEY_FILE":  writeFile(t, testPrivateKey+"\n"),
	})))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL != "postgres://from-file" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.OIDC.ClientID != "id-from-file" {
		t.Errorf("ClientID = %q", cfg.OIDC.ClientID)
	}
	if cfg.OIDC.ClientSecret != "secret-from-file" {
		t.Errorf("ClientSecret = %q", cfg.OIDC.ClientSecret)
	}
	if cfg.Push.Keys.Public != testPublicKey {
		t.Errorf("VAPID keys from file not read")
	}
}

func TestLoadPublicURL(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"https://tasks.example.com", "https://tasks.example.com"},
		{"https://tasks.example.com/", "https://tasks.example.com"},
		{"https://tasks.example.com:8443", "https://tasks.example.com:8443"},
		// Browsers treat loopback http as a secure context.
		{"http://localhost:8080", "http://localhost:8080"},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"http://[::1]:8080", "http://[::1]:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			cfg, err := Load(env(required(map[string]string{"PUBLIC_URL": tt.value})))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.PublicURL != tt.want {
				t.Errorf("PublicURL = %q, want %q", cfg.PublicURL, tt.want)
			}
		})
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
			vars:    required(map[string]string{"DATABASE_URL": "<unset>"}),
			wantErr: "DATABASE_URL or DATABASE_URL_FILE is required",
		},
		{
			name:    "database URL empty",
			vars:    required(map[string]string{"DATABASE_URL": ""}),
			wantErr: "DATABASE_URL or DATABASE_URL_FILE is required",
		},
		{
			name:    "both value and file",
			vars:    required(map[string]string{"DATABASE_URL_FILE": "/run/secrets/db"}),
			wantErr: "set only one of DATABASE_URL and DATABASE_URL_FILE",
		},
		{
			name:    "unreadable file",
			vars:    required(map[string]string{"DATABASE_URL": "<unset>", "DATABASE_URL_FILE": missing}),
			wantErr: "DATABASE_URL_FILE",
		},
		{
			name:    "invalid log level",
			vars:    required(map[string]string{"LOG_LEVEL": "loud"}),
			wantErr: "LOG_LEVEL",
		},
		{
			name:    "public URL missing",
			vars:    required(map[string]string{"PUBLIC_URL": "<unset>"}),
			wantErr: "PUBLIC_URL: required",
		},
		{
			name:    "public URL over http",
			vars:    required(map[string]string{"PUBLIC_URL": "http://tasks.example.com"}),
			wantErr: "PUBLIC_URL: must use https",
		},
		{
			name:    "public URL with a path",
			vars:    required(map[string]string{"PUBLIC_URL": "https://example.com/tasks"}),
			wantErr: "PUBLIC_URL: must be an origin",
		},
		{
			name:    "public URL not absolute",
			vars:    required(map[string]string{"PUBLIC_URL": "tasks.example.com"}),
			wantErr: "PUBLIC_URL: must be an absolute URL",
		},
		{
			name:    "issuer missing",
			vars:    required(map[string]string{"OIDC_ISSUER": "<unset>"}),
			wantErr: "OIDC_ISSUER: required",
		},
		{
			name:    "issuer not a URL",
			vars:    required(map[string]string{"OIDC_ISSUER": "id.example.com"}),
			wantErr: "OIDC_ISSUER: must be an absolute http or https URL",
		},
		{
			name:    "client id missing",
			vars:    required(map[string]string{"OIDC_CLIENT_ID": "<unset>"}),
			wantErr: "OIDC_CLIENT_ID or OIDC_CLIENT_ID_FILE is required",
		},
		{
			name:    "client secret missing",
			vars:    required(map[string]string{"OIDC_CLIENT_SECRET": "<unset>"}),
			wantErr: "OIDC_CLIENT_SECRET or OIDC_CLIENT_SECRET_FILE is required",
		},
		{
			name:    "client secret in both forms",
			vars:    required(map[string]string{"OIDC_CLIENT_SECRET_FILE": "/run/secrets/oidc"}), //nolint:gosec // G101: a path, not a credential
			wantErr: "set only one of OIDC_CLIENT_SECRET and OIDC_CLIENT_SECRET_FILE",
		},
		{
			name:    "idle timeout not a duration",
			vars:    required(map[string]string{"SESSION_IDLE_TIMEOUT": "7d"}),
			wantErr: "SESSION_IDLE_TIMEOUT",
		},
		{
			name:    "VAPID public key missing",
			vars:    required(map[string]string{"VAPID_PUBLIC_KEY": "<unset>"}),
			wantErr: "VAPID_PUBLIC_KEY is required",
		},
		{
			name:    "VAPID private key missing",
			vars:    required(map[string]string{"VAPID_PRIVATE_KEY": "<unset>"}),
			wantErr: "VAPID_PRIVATE_KEY or VAPID_PRIVATE_KEY_FILE is required",
		},
		{
			name:    "VAPID keys of different pairs",
			vars:    required(map[string]string{"VAPID_PUBLIC_KEY": otherPublicKey()}),
			wantErr: "VAPID keys: webpush: the public key does not match",
		},
		{
			name:    "VAPID subject not a URL",
			vars:    required(map[string]string{"VAPID_SUBJECT": "ops@example.com"}),
			wantErr: "VAPID_SUBJECT must be a mailto: or https: URL",
		},
		{
			name:    "scheduler interval not a duration",
			vars:    required(map[string]string{"SCHEDULER_INTERVAL": "often"}),
			wantErr: "SCHEDULER_INTERVAL",
		},
		{
			name:    "max age not positive",
			vars:    required(map[string]string{"SESSION_MAX_AGE": "0s"}),
			wantErr: "SESSION_MAX_AGE: must be positive",
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

// Errors about secrets name the variable but never carry its value.
func TestLoadErrorsDoNotLeakSecrets(t *testing.T) {
	_, err := Load(env(required(map[string]string{"OIDC_CLIENT_SECRET_FILE": "/run/secrets/oidc"}))) //nolint:gosec // G101: a path, not a credential
	if err == nil {
		t.Fatal("Load succeeded, want an error")
	}
	if strings.Contains(err.Error(), "client-secret-for-tests") {
		t.Errorf("error %q contains the secret", err)
	}
}

func otherPublicKey() string {
	public, _, err := webpush.GenerateKeys()
	if err != nil {
		panic(err)
	}
	return public
}

// The VAPID private key never appears in errors.
func TestVAPIDErrorsDoNotLeakTheKey(t *testing.T) {
	_, err := Load(env(required(map[string]string{"VAPID_PUBLIC_KEY": otherPublicKey()})))
	if err == nil || strings.Contains(err.Error(), testPrivateKey) {
		t.Errorf("error = %v", err)
	}
}

// D-72: an empty METRICS_LISTEN_ADDR turns metrics off.
func TestMetricsCanBeTurnedOff(t *testing.T) {
	cfg, err := Load(env(required(map[string]string{"METRICS_LISTEN_ADDR": ""})))
	if err != nil || cfg.MetricsListenAddr != "" {
		t.Errorf("MetricsListenAddr = %q (%v), want empty", cfg.MetricsListenAddr, err)
	}
	cfg, err = Load(env(required(map[string]string{"METRICS_LISTEN_ADDR": "127.0.0.1:9100"})))
	if err != nil || cfg.MetricsListenAddr != "127.0.0.1:9100" {
		t.Errorf("MetricsListenAddr = %q (%v)", cfg.MetricsListenAddr, err)
	}
}
