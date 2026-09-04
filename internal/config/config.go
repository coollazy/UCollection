// Package config loads UCollection's runtime configuration from
// environment variables. See docs/CLAUDE.md and
// docs/開發流程框架-03-技術架構設計.md 第1節.
package config

import (
	"fmt"
	"os"
	"strconv"
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
	// PublicOrigin is the merchant-facing origin (e.g.
	// https://pay.merchant.com), used to build checkout page URLs (see
	// internal/api) and by internal/auth's Origin-header CSRF check (技術架構
	// 設計第9節).
	PublicOrigin string
	// AdminUsername/AdminPassword bootstrap the single admin_account row
	// the first time the app starts against an empty database (see
	// internal/auth.EnsureAdminAccount, 技術架構設計第9節「帳號模型」). Not
	// required by Load() itself — only required at that specific runtime
	// moment, which Load() can't know ahead of querying the DB.
	AdminUsername string
	AdminPassword string
	// CookieSecure controls the session cookie's Secure attribute (技術架構
	// 設計第9節「Session模型」). Defaults to true; deployments without TLS in
	// front (e.g. local testing) must set COOKIE_SECURE=false.
	CookieSecure bool
}

const (
	envListenAddr          = "LISTEN_ADDR"
	envDatabaseURL         = "DATABASE_URL"
	envEnvironment         = "ENVIRONMENT"
	envTronGridBaseURL     = "TRONGRID_BASE_URL"
	envTronGridAPIKey      = "TRONGRID_API_KEY" //nolint:gosec // this is an env var name, not a credential
	envUSDTContractAddress = "USDT_CONTRACT_ADDRESS"
	envPublicOrigin        = "PUBLIC_ORIGIN"
	envAdminUsername       = "ADMIN_USERNAME"
	envAdminPassword       = "ADMIN_PASSWORD" //nolint:gosec // this is an env var name, not a credential
	envCookieSecure        = "COOKIE_SECURE"

	defaultListenAddr      = ":8080"
	defaultEnvironment     = "production"
	defaultTronGridBaseURL = "https://api.trongrid.io"
	defaultCookieSecure    = true
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
		PublicOrigin:        os.Getenv(envPublicOrigin),
		AdminUsername:       os.Getenv(envAdminUsername),
		AdminPassword:       os.Getenv(envAdminPassword),
		CookieSecure:        getEnvBoolDefault(envCookieSecure, defaultCookieSecure),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: %s is required", envDatabaseURL)
	}
	if cfg.USDTContractAddress == "" {
		return Config{}, fmt.Errorf("config: %s is required", envUSDTContractAddress)
	}
	if cfg.PublicOrigin == "" {
		return Config{}, fmt.Errorf("config: %s is required", envPublicOrigin)
	}

	return cfg, nil
}

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBoolDefault(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
