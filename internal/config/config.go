package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Defaults used when the matching environment variable is not set.
const (
	DefaultAdmin    = "admin"
	DefaultPassword = "admin"
	DefaultPort     = "8080"
)

// Config holds the runtime configuration for the file manager.
type Config struct {
	// HomeDir is the absolute root folder exposed by the app. It is the
	// resolved (symlink-free) form of the HOME_DIR setting.
	HomeDir string
	// Admin and Password gate HTTP Basic authentication.
	Admin    string
	Password string
	// Port is the TCP port the HTTP server listens on.
	Port string
}

// Load builds a Config from the process environment. A .env file in the
// current working directory is loaded first, but real environment variables
// always take precedence over values from the file.
func Load() (*Config, error) {
	loadDotEnv(".env")

	home := strings.TrimSpace(os.Getenv("HOME_DIR"))
	if home == "" {
		return nil, errors.New("HOME_DIR is not set; add it to .env or export it")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return nil, fmt.Errorf("resolve HOME_DIR: %w", err)
	}
	// Compare later containment checks against the real path.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	return &Config{
		HomeDir:  abs,
		Admin:    envOr("ADMIN", DefaultAdmin),
		Password: envOr("PASSWORD", DefaultPassword),
		Port:     envOr("PORT", DefaultPort),
	}, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv reads simple KEY=VALUE lines from name, ignoring comments and
// blank lines. Existing environment variables are never overwritten.
func loadDotEnv(name string) {
	f, err := os.Open(name)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}
