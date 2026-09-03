package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	t.Setenv(envListenAddr, "")
	t.Setenv(envDatabaseURL, "postgres://user:pass@localhost:5432/ucollection")
	t.Setenv(envEnvironment, "")

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
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv(envListenAddr, ":9090")
	t.Setenv(envDatabaseURL, "postgres://user:pass@localhost:5432/ucollection")
	t.Setenv(envEnvironment, "test")

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
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	t.Setenv(envDatabaseURL, "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error for missing DATABASE_URL")
	}
}
