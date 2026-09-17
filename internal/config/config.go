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
	// internal/tronclient). Defaults from NETWORK; overridable directly
	// for Nile testnet, self-hosted nodes, or TronGrid-compatible proxies
	// not covered by the mainnet/shasta presets.
	TronGridBaseURL string
	// TronGridAPIKey is optional — TronGrid allows unauthenticated
	// requests at a lower rate limit (see internal/tronclient.NewClient).
	TronGridAPIKey string
	// USDTContractAddress is the TRC20 contract internal/scanner watches
	// for Transfer events (see internal/scanner). Defaults from NETWORK;
	// overridable directly for the same non-preset cases as
	// TronGridBaseURL.
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
	envNetwork             = "NETWORK"
	envTronGridBaseURL     = "TRONGRID_BASE_URL"
	envTronGridAPIKey      = "TRONGRID_API_KEY" //nolint:gosec // this is an env var name, not a credential
	envUSDTContractAddress = "USDT_CONTRACT_ADDRESS"
	envPublicOrigin        = "PUBLIC_ORIGIN"
	envAdminUsername       = "ADMIN_USERNAME"
	envAdminPassword       = "ADMIN_PASSWORD" //nolint:gosec // this is an env var name, not a credential
	envCookieSecure        = "COOKIE_SECURE"

	defaultListenAddr   = ":8080"
	defaultEnvironment  = "production"
	defaultCookieSecure = true

	// networkMainnet and networkShasta are the only NETWORK values Load
	// recognizes. Nile testnet or self-hosted/proxy endpoints are reached
	// via explicit TRONGRID_BASE_URL/USDT_CONTRACT_ADDRESS overrides
	// instead of a third preset (見 docs/進度.md 階段08 NETWORK enum討論).
	networkMainnet = "mainnet"
	networkShasta  = "shasta"

	mainnetTronGridBaseURL     = "https://api.trongrid.io"
	mainnetUSDTContractAddress = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	// shastaUSDTContractAddress is Shasta 測試網水龍頭內建的 USDT 測試代幣合約
	// （非Tether官方發行，但地址固定，各家水龍頭/文件皆沿用同一組）。
	shastaTronGridBaseURL     = "https://api.shasta.trongrid.io"
	shastaUSDTContractAddress = "TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs"
)

// Load reads Config from environment variables, applying defaults for
// optional values. DatabaseURL and PublicOrigin are required.
//
// NETWORK ("mainnet", the default, or "shasta") selects the default
// TronGridBaseURL/USDTContractAddress pair; either can still be
// overridden directly for Nile testnet, a self-hosted node, or a
// TronGrid-compatible proxy.
func Load() (Config, error) {
	network := getEnvDefault(envNetwork, networkMainnet)

	var networkTronGridBaseURL, networkUSDTContractAddress string
	switch network {
	case networkMainnet:
		networkTronGridBaseURL = mainnetTronGridBaseURL
		networkUSDTContractAddress = mainnetUSDTContractAddress
	case networkShasta:
		networkTronGridBaseURL = shastaTronGridBaseURL
		networkUSDTContractAddress = shastaUSDTContractAddress
	default:
		return Config{}, fmt.Errorf("config: %s = %q is not recognized (want %q or %q)", envNetwork, network, networkMainnet, networkShasta)
	}

	cfg := Config{
		ListenAddr:          getEnvDefault(envListenAddr, defaultListenAddr),
		DatabaseURL:         os.Getenv(envDatabaseURL),
		Environment:         getEnvDefault(envEnvironment, defaultEnvironment),
		TronGridBaseURL:     getEnvDefault(envTronGridBaseURL, networkTronGridBaseURL),
		TronGridAPIKey:      os.Getenv(envTronGridAPIKey),
		USDTContractAddress: getEnvDefault(envUSDTContractAddress, networkUSDTContractAddress),
		PublicOrigin:        os.Getenv(envPublicOrigin),
		AdminUsername:       os.Getenv(envAdminUsername),
		AdminPassword:       os.Getenv(envAdminPassword),
		CookieSecure:        getEnvBoolDefault(envCookieSecure, defaultCookieSecure),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: %s is required", envDatabaseURL)
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
