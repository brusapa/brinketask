// Package config reads the process configuration from environment variables
// (SPEC section 10).
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Config holds every setting the process needs. Settings used by later phases
// (OIDC, VAPID, ...) are added when the code that needs them arrives.
type Config struct {
	DatabaseURL string
	ListenAddr  string
	LogLevel    slog.Level
}

// LookupFunc has the signature of os.LookupEnv. Load takes it as a parameter
// so tests can supply variables without touching the real environment.
type LookupFunc func(key string) (value string, ok bool)

const defaultListenAddr = ":8080"

// Load builds a Config from the variables returned by lookup.
func Load(lookup LookupFunc) (Config, error) {
	cfg := Config{
		ListenAddr: defaultListenAddr,
		LogLevel:   slog.LevelInfo,
	}

	databaseURL, err := secret(lookup, "DATABASE_URL")
	if err != nil {
		return Config{}, err
	}
	if databaseURL == "" {
		return Config{}, errors.New("config: DATABASE_URL or DATABASE_URL_FILE is required")
	}
	cfg.DatabaseURL = databaseURL

	if value, ok := lookup("LISTEN_ADDR"); ok && value != "" {
		cfg.ListenAddr = value
	}

	if value, ok := lookup("LOG_LEVEL"); ok && value != "" {
		// slog.Level parses "debug", "info", "warn" and "error", case-insensitively.
		if err := cfg.LogLevel.UnmarshalText([]byte(value)); err != nil {
			return Config{}, fmt.Errorf("config: LOG_LEVEL: %w", err)
		}
	}

	return cfg, nil
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
		content, err := os.ReadFile(path)
		if err != nil {
			// The error text carries only the path, never the file content.
			return "", fmt.Errorf("config: %s_FILE: %w", name, err)
		}
		return strings.TrimRight(string(content), "\r\n"), nil
	default:
		return "", nil
	}
}
