// Package config loads UCollection's runtime configuration from
// environment variables. See docs/CLAUDE.md and
// docs/開發流程框架-03-技術架構設計.md 第1節.
package config

import (
	"fmt"
	"os"
)

// Config holds the settings needed to start the app binary.
type Config struct {
	// ListenAddr is the address the HTTP server binds to, e.g. ":8080".
	ListenAddr string
	// DatabaseURL is a PostgreSQL connection string (see internal/store).
	DatabaseURL string
	// Environment distinguishes production from test deployments.
	Environment string
}

const (
	envListenAddr  = "LISTEN_ADDR"
	envDatabaseURL = "DATABASE_URL"
	envEnvironment = "ENVIRONMENT"

	defaultListenAddr  = ":8080"
	defaultEnvironment = "production"
)

// Load reads Config from environment variables, applying defaults for
// optional values. DatabaseURL is required.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr:  getEnvDefault(envListenAddr, defaultListenAddr),
		DatabaseURL: os.Getenv(envDatabaseURL),
		Environment: getEnvDefault(envEnvironment, defaultEnvironment),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: %s is required", envDatabaseURL)
	}

	return cfg, nil
}

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
