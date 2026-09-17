package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	t.Setenv(envListenAddr, "")
	t.Setenv(envDatabaseURL, "postgres://user:pass@localhost:5432/ucollection")
	t.Setenv(envEnvironment, "")
	t.Setenv(envNetwork, "")
	t.Setenv(envTronGridBaseURL, "")
	t.Setenv(envTronGridAPIKey, "")
	t.Setenv(envUSDTContractAddress, "")
	t.Setenv(envPublicOrigin, "https://pay.merchant.example")
	t.Setenv(envAdminUsername, "")
	t.Setenv(envAdminPassword, "")
	t.Setenv(envCookieSecure, "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddr != defaultListenAddr {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, defaultListenAddr)
	}
	if cfg.Environment != defaultEnvironment {
		t.Errorf("Environment = %q, want %q", cfg.Environment, defaultEnvironment)
	}
	if cfg.TronGridBaseURL != mainnetTronGridBaseURL {
		t.Errorf("TronGridBaseURL = %q, want %q (default NETWORK=mainnet)", cfg.TronGridBaseURL, mainnetTronGridBaseURL)
	}
	if cfg.USDTContractAddress != mainnetUSDTContractAddress {
		t.Errorf("USDTContractAddress = %q, want %q (default NETWORK=mainnet)", cfg.USDTContractAddress, mainnetUSDTContractAddress)
	}
	if cfg.TronGridAPIKey != "" {
		t.Errorf("TronGridAPIKey = %q, want empty (optional)", cfg.TronGridAPIKey)
	}
	if cfg.AdminUsername != "" || cfg.AdminPassword != "" {
		t.Errorf("AdminUsername/AdminPassword = %q/%q, want empty (only required at first-boot bootstrap, not by Load())", cfg.AdminUsername, cfg.AdminPassword)
	}
	if cfg.CookieSecure != defaultCookieSecure {
		t.Errorf("CookieSecure = %v, want default %v", cfg.CookieSecure, defaultCookieSecure)
	}
}

func TestLoad_NetworkShasta(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://user:pass@localhost:5432/ucollection")
	t.Setenv(envNetwork, "shasta")
	t.Setenv(envTronGridBaseURL, "")
	t.Setenv(envUSDTContractAddress, "")
	t.Setenv(envPublicOrigin, "https://pay.merchant.example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.TronGridBaseURL != shastaTronGridBaseURL {
		t.Errorf("TronGridBaseURL = %q, want %q (NETWORK=shasta)", cfg.TronGridBaseURL, shastaTronGridBaseURL)
	}
	if cfg.USDTContractAddress != shastaUSDTContractAddress {
		t.Errorf("USDTContractAddress = %q, want %q (NETWORK=shasta)", cfg.USDTContractAddress, shastaUSDTContractAddress)
	}
}

func TestLoad_NetworkUnrecognized(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://user:pass@localhost:5432/ucollection")
	t.Setenv(envNetwork, "nile")
	t.Setenv(envPublicOrigin, "https://pay.merchant.example")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for unrecognized NETWORK value")
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv(envListenAddr, ":9090")
	t.Setenv(envDatabaseURL, "postgres://user:pass@localhost:5432/ucollection")
	t.Setenv(envEnvironment, "test")
	// NETWORK=shasta here to prove explicit TRONGRID_BASE_URL/
	// USDT_CONTRACT_ADDRESS still win over the network preset (e.g. for
	// Nile testnet or a self-hosted node).
	t.Setenv(envNetwork, "shasta")
	t.Setenv(envTronGridBaseURL, "https://nile.trongrid.io")
	t.Setenv(envTronGridAPIKey, "test-key")
	t.Setenv(envUSDTContractAddress, "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf")
	t.Setenv(envPublicOrigin, "https://pay.merchant.example")
	t.Setenv(envAdminUsername, "admin")
	t.Setenv(envAdminPassword, "bootstrap-password")
	t.Setenv(envCookieSecure, "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AdminUsername != "admin" || cfg.AdminPassword != "bootstrap-password" {
		t.Errorf("AdminUsername/AdminPassword = %q/%q, want overrides", cfg.AdminUsername, cfg.AdminPassword)
	}
	if cfg.CookieSecure {
		t.Errorf("CookieSecure = %v, want false (override)", cfg.CookieSecure)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.Environment != "test" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "test")
	}
	if cfg.TronGridBaseURL != "https://nile.trongrid.io" {
		t.Errorf("TronGridBaseURL = %q, want override", cfg.TronGridBaseURL)
	}
	if cfg.TronGridAPIKey != "test-key" {
		t.Errorf("TronGridAPIKey = %q, want test-key", cfg.TronGridAPIKey)
	}
	if cfg.USDTContractAddress != "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf" {
		t.Errorf("USDTContractAddress = %q, want override", cfg.USDTContractAddress)
	}
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	t.Setenv(envDatabaseURL, "")
	t.Setenv(envPublicOrigin, "https://pay.merchant.example")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for missing DATABASE_URL")
	}
}

func TestLoad_MissingPublicOrigin(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://user:pass@localhost:5432/ucollection")
	t.Setenv(envPublicOrigin, "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for missing PUBLIC_ORIGIN")
	}
}
