// Package config reads the process configuration from environment variables
// (SPEC section 10).
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/brusapa/brinketask/internal/webpush"
)

// Config holds every setting the process needs (SPEC section 10).
type Config struct {
	DatabaseURL string
	ListenAddr  string
	LogLevel    slog.Level

	// MetricsListenAddr is where /metrics listens (D-72); empty disables
	// metrics.
	MetricsListenAddr string

	// PublicURL is the origin the browser uses to reach the server, e.g.
	// "https://tasks.example.com", without a trailing slash. The OIDC
	// redirect URL and the same-origin check derive from it.
	PublicURL string

	OIDC OIDC

	// Sessions end after SessionIdleTimeout without use, and after
	// SessionMaxAge in any case (SPEC section 7, D-29).
	SessionIdleTimeout time.Duration
	SessionMaxAge      time.Duration

	Push Push

	// The reminder scheduler polls every SchedulerInterval; a reminder
	// handed over more than ReminderMaxLateness late is skipped (SPEC
	// section 6, D-29).
	SchedulerInterval   time.Duration
	ReminderMaxLateness time.Duration
}

// Push identifies the server to Web Push services (VAPID, RFC 8292).
type Push struct {
	Keys webpush.Keys
	// Subject is the contact the push services see: mailto: or https:.
	Subject string
	// TestEndpointPrefix, for the end-to-end test only, is a URL prefix
	// whose endpoints are accepted although they break the SSRF rule: the
	// fake push service runs on localhost over http (D-73). Never set in
	// production.
	TestEndpointPrefix string
}

// OIDC identifies the server as a confidential client of the OIDC provider
// (D-13).
type OIDC struct {
	// Issuer is the provider's issuer URL; discovery reads
	// Issuer + "/.well-known/openid-configuration".
	Issuer       string
	ClientID     string
	ClientSecret string
}

// LookupFunc has the signature of os.LookupEnv. Load takes it as a parameter
// so tests can supply variables without touching the real environment.
type LookupFunc func(key string) (value string, ok bool)

const (
	defaultListenAddr         = ":8080"
	defaultMetricsListenAddr  = ":9090"
	defaultSessionIdleTimeout = 7 * 24 * time.Hour
	defaultSessionMaxAge      = 30 * 24 * time.Hour
	defaultSchedulerInterval  = 15 * time.Second
	defaultMaxLateness        = 12 * time.Hour
)

// Load builds a Config from the variables returned by lookup. It reports the
// first problem it finds.
func Load(lookup LookupFunc) (Config, error) {
	cfg := Config{
		ListenAddr:          defaultListenAddr,
		MetricsListenAddr:   defaultMetricsListenAddr,
		LogLevel:            slog.LevelInfo,
		SessionIdleTimeout:  defaultSessionIdleTimeout,
		SessionMaxAge:       defaultSessionMaxAge,
		SchedulerInterval:   defaultSchedulerInterval,
		ReminderMaxLateness: defaultMaxLateness,
	}
	var err error

	if cfg.DatabaseURL, err = requiredSecret(lookup, "DATABASE_URL"); err != nil {
		return Config{}, err
	}

	if value, ok := lookup("LISTEN_ADDR"); ok && value != "" {
		cfg.ListenAddr = value
	}

	// Set but empty means "no metrics", so unset and empty differ here.
	if value, ok := lookup("METRICS_LISTEN_ADDR"); ok {
		cfg.MetricsListenAddr = value
	}

	if value, ok := lookup("LOG_LEVEL"); ok && value != "" {
		// slog.Level parses "debug", "info", "warn" and "error", case-insensitively.
		if err := cfg.LogLevel.UnmarshalText([]byte(value)); err != nil {
			return Config{}, fmt.Errorf("config: LOG_LEVEL: %w", err)
		}
	}

	publicURL, _ := lookup("PUBLIC_URL")
	if cfg.PublicURL, err = parsePublicURL(publicURL); err != nil {
		return Config{}, fmt.Errorf("config: PUBLIC_URL: %w", err)
	}

	issuer, _ := lookup("OIDC_ISSUER")
	if cfg.OIDC.Issuer, err = parseIssuer(issuer); err != nil {
		return Config{}, fmt.Errorf("config: OIDC_ISSUER: %w", err)
	}
	if cfg.OIDC.ClientID, err = requiredSecret(lookup, "OIDC_CLIENT_ID"); err != nil {
		return Config{}, err
	}
	if cfg.OIDC.ClientSecret, err = requiredSecret(lookup, "OIDC_CLIENT_SECRET"); err != nil {
		return Config{}, err
	}

	if err := duration(lookup, "SESSION_IDLE_TIMEOUT", &cfg.SessionIdleTimeout); err != nil {
		return Config{}, err
	}
	if err := duration(lookup, "SESSION_MAX_AGE", &cfg.SessionMaxAge); err != nil {
		return Config{}, err
	}

	if cfg.Push, err = loadPush(lookup); err != nil {
		return Config{}, err
	}
	if err := duration(lookup, "SCHEDULER_INTERVAL", &cfg.SchedulerInterval); err != nil {
		return Config{}, err
	}
	if err := duration(lookup, "REMINDER_MAX_LATENESS", &cfg.ReminderMaxLateness); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// loadPush reads the VAPID key pair, as `brinketask vapid-keys` prints it
// (D-68), and the contact subject.
func loadPush(lookup LookupFunc) (Push, error) {
	public, _ := lookup("VAPID_PUBLIC_KEY")
	if public == "" {
		return Push{}, errors.New("config: VAPID_PUBLIC_KEY is required")
	}
	private, err := requiredSecret(lookup, "VAPID_PRIVATE_KEY")
	if err != nil {
		return Push{}, err
	}
	keys, err := webpush.ParseKeys(public, private)
	if err != nil {
		// ParseKeys errors never quote the private key.
		return Push{}, fmt.Errorf("config: VAPID keys: %w", err)
	}
	subject, _ := lookup("VAPID_SUBJECT")
	if !strings.HasPrefix(subject, "mailto:") && !strings.HasPrefix(subject, "https://") {
		return Push{}, errors.New("config: VAPID_SUBJECT must be a mailto: or https: URL")
	}
	push := Push{Keys: keys, Subject: subject}
	if prefix, _ := lookup("PUSH_TEST_ENDPOINT_PREFIX"); prefix != "" {
		// A prefix that ends at a path "/" cannot be stretched to another
		// host ("http://localhost:1" would also match "http://localhost:18").
		u, err := url.Parse(prefix)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			!strings.HasSuffix(u.Path, "/") || u.RawQuery != "" || u.Fragment != "" {
			return Push{}, errors.New("config: PUSH_TEST_ENDPOINT_PREFIX must be an http(s) URL ending in /")
		}
		push.TestEndpointPrefix = prefix
	}
	return push, nil
}

// parsePublicURL accepts an origin: scheme and host, optionally a port, and
// no path, query or fragment, because the server is mounted at the root of
// its origin. The scheme must be https (SPEC section 2: the session cookie
// is Secure, and service workers need it), except on a loopback host,
// where browsers treat http as secure too; that is what local development
// uses.
func parsePublicURL(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Host == "" || u.Opaque != "" {
		return "", errors.New("must be an absolute URL such as https://tasks.example.com")
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("must be an origin, without path, query, fragment or credentials")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return "", errors.New("must use https (http is accepted only for localhost)")
	}
	return u.Scheme + "://" + u.Host, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// parseIssuer accepts an absolute http or https URL. It is kept exactly as
// given (apart from a trailing slash), because the provider's tokens must
// carry this same string as their "iss" claim.
func parseIssuer(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", errors.New("must be an absolute http or https URL")
	}
	return strings.TrimSuffix(raw, "/"), nil
}

// duration parses an optional Go duration ("90m", "168h") into *target,
// leaving the default in place when the variable is unset.
func duration(lookup LookupFunc, name string, target *time.Duration) error {
	value, ok := lookup(name)
	if !ok || value == "" {
		return nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("config: %s: %w", name, err)
	}
	if d <= 0 {
		return fmt.Errorf("config: %s: must be positive", name)
	}
	*target = d
	return nil
}

// requiredSecret is secret for a variable that must be set.
func requiredSecret(lookup LookupFunc, name string) (string, error) {
	value, err := secret(lookup, name)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("config: %s or %s_FILE is required", name, name)
	}
	return value, nil
}

// secret returns the value of a secret variable. Every secret can be given
// either directly in NAME or as a path to a file in NAME_FILE, which is how
// container orchestrators mount secrets. Setting both is rejected because it
// is ambiguous. A trailing newline in the file is removed, since editors and
// `echo` add one. An unset secret returns "" and no error; the caller decides
// whether it is required.
func secret(lookup LookupFunc, name string) (string, error) {
	value, hasValue := lookup(name)
	path, hasPath := lookup(name + "_FILE")
	hasValue = hasValue && value != ""
	hasPath = hasPath && path != ""

	switch {
	case hasValue && hasPath:
		return "", fmt.Errorf("config: set only one of %s and %s_FILE", name, name)
	case hasValue:
		return value, nil
	case hasPath:
		// G304 warns about reading a path held in a variable. This path is
		// set by the operator who deploys the server, not by a request.
		content, err := os.ReadFile(path) //nolint:gosec // G304: operator-supplied path

		if err != nil {
			// The error text carries only the path, never the file content.
			return "", fmt.Errorf("config: %s_FILE: %w", name, err)
		}
		return strings.TrimRight(string(content), "\r\n"), nil
	default:
		return "", nil
	}
}
