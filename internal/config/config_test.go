package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	t.Setenv(envListenAddr, "")
	t.Setenv(envDatabaseURL, "postgres://user:pass@localhost:5432/ucollection")
	t.Setenv(envEnvironment, "")
	t.Setenv(envTronGridBaseURL, "")
	t.Setenv(envTronGridAPIKey, "")
	t.Setenv(envUSDTContractAddress, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t")

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
	if cfg.TronGridBaseURL != defaultTronGridBaseURL {
		t.Errorf("TronGridBaseURL = %q, want %q", cfg.TronGridBaseURL, defaultTronGridBaseURL)
	}
	if cfg.TronGridAPIKey != "" {
		t.Errorf("TronGridAPIKey = %q, want empty (optional)", cfg.TronGridAPIKey)
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv(envListenAddr, ":9090")
	t.Setenv(envDatabaseURL, "postgres://user:pass@localhost:5432/ucollection")
	t.Setenv(envEnvironment, "test")
	t.Setenv(envTronGridBaseURL, "https://shasta.trongrid.io")
	t.Setenv(envTronGridAPIKey, "test-key")
	t.Setenv(envUSDTContractAddress, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.Environment != "test" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "test")
	}
	if cfg.TronGridBaseURL != "https://shasta.trongrid.io" {
		t.Errorf("TronGridBaseURL = %q, want override", cfg.TronGridBaseURL)
	}
	if cfg.TronGridAPIKey != "test-key" {
		t.Errorf("TronGridAPIKey = %q, want test-key", cfg.TronGridAPIKey)
	}
	if cfg.USDTContractAddress != "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t" {
		t.Errorf("USDTContractAddress = %q, want override", cfg.USDTContractAddress)
	}
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	t.Setenv(envDatabaseURL, "")
	t.Setenv(envUSDTContractAddress, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for missing DATABASE_URL")
	}
}

func TestLoad_MissingUSDTContractAddress(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://user:pass@localhost:5432/ucollection")
	t.Setenv(envUSDTContractAddress, "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for missing USDT_CONTRACT_ADDRESS")
	}
}
