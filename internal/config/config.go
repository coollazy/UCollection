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
	// TronGridBaseURL is the base URL for TronGrid API calls (see
	// internal/tronclient). Overridable for testnets (Shasta) or
	// self-hosted TronGrid-compatible endpoints.
	TronGridBaseURL string
	// TronGridAPIKey is optional — TronGrid allows unauthenticated
	// requests at a lower rate limit (see internal/tronclient.NewClient).
	TronGridAPIKey string
	// USDTContractAddress is the TRC20 contract internal/scanner watches
	// for Transfer events (see internal/scanner).
	USDTContractAddress string
}

const (
	envListenAddr          = "LISTEN_ADDR"
	envDatabaseURL         = "DATABASE_URL"
	envEnvironment         = "ENVIRONMENT"
	envTronGridBaseURL     = "TRONGRID_BASE_URL"
	envTronGridAPIKey      = "TRONGRID_API_KEY" //nolint:gosec // this is an env var name, not a credential
	envUSDTContractAddress = "USDT_CONTRACT_ADDRESS"

	defaultListenAddr      = ":8080"
	defaultEnvironment     = "production"
	defaultTronGridBaseURL = "https://api.trongrid.io"
)

// Load reads Config from environment variables, applying defaults for
// optional values. DatabaseURL and USDTContractAddress are required.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr:          getEnvDefault(envListenAddr, defaultListenAddr),
		DatabaseURL:         os.Getenv(envDatabaseURL),
		Environment:         getEnvDefault(envEnvironment, defaultEnvironment),
		TronGridBaseURL:     getEnvDefault(envTronGridBaseURL, defaultTronGridBaseURL),
		TronGridAPIKey:      os.Getenv(envTronGridAPIKey),
		USDTContractAddress: os.Getenv(envUSDTContractAddress),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: %s is required", envDatabaseURL)
	}
	if cfg.USDTContractAddress == "" {
		return Config{}, fmt.Errorf("config: %s is required", envUSDTContractAddress)
	}

	return cfg, nil
}

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
