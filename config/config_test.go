package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Tailscale.BaseURL != "https://api.tailscale.com" {
		t.Errorf("Tailscale.BaseURL = %q", cfg.Tailscale.BaseURL)
	}
	if cfg.SSH.Port != 22 {
		t.Errorf("SSH.Port = %d, want 22", cfg.SSH.Port)
	}
	if cfg.SSH.Timeout != 30 {
		t.Errorf("SSH.Timeout = %d, want 30", cfg.SSH.Timeout)
	}
	if cfg.SSH.UseTailscaleSSH {
		t.Error("SSH.UseTailscaleSSH should default to false")
	}
	if cfg.Ray.DefaultPort != 6379 {
		t.Errorf("Ray.DefaultPort = %d, want 6379", cfg.Ray.DefaultPort)
	}
	if cfg.Ray.DefaultDashboardPort != 8265 {
		t.Errorf("Ray.DefaultDashboardPort = %d, want 8265", cfg.Ray.DefaultDashboardPort)
	}
	if cfg.Ray.PythonPath != "python3" {
		t.Errorf("Ray.PythonPath = %q", cfg.Ray.PythonPath)
	}
	if cfg.Ray.DefaultVersion != "2.9.0" {
		t.Errorf("Ray.DefaultVersion = %q", cfg.Ray.DefaultVersion)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Errorf("Database.Driver = %q", cfg.Database.Driver)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q", cfg.Log.Level)
	}
	if cfg.Log.Format != "text" {
		t.Errorf("Log.Format = %q", cfg.Log.Format)
	}
}

func TestConfig_Validate(t *testing.T) {
	// No API key or OAuth -> error
	cfg := DefaultConfig()
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should fail without API key or OAuth")
	}

	// With API key -> ok
	cfg.Tailscale.APIKey = "tskey-1234"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() with API key: %v", err)
	}

	// With OAuth -> ok
	cfg2 := DefaultConfig()
	cfg2.Tailscale.OAuthClientID = "client-id"
	if err := cfg2.Validate(); err != nil {
		t.Errorf("Validate() with OAuth: %v", err)
	}
}

func TestGetConfigDir_EnvOverride(t *testing.T) {
	original := os.Getenv("CLUSTERCTL_CONFIG_DIR")
	defer os.Setenv("CLUSTERCTL_CONFIG_DIR", original)

	os.Setenv("CLUSTERCTL_CONFIG_DIR", "/tmp/test-config")
	if dir := GetConfigDir(); dir != "/tmp/test-config" {
		t.Errorf("GetConfigDir() = %q, want /tmp/test-config", dir)
	}
}

func TestGetConfigDir_Default(t *testing.T) {
	original := os.Getenv("CLUSTERCTL_CONFIG_DIR")
	defer os.Setenv("CLUSTERCTL_CONFIG_DIR", original)

	os.Unsetenv("CLUSTERCTL_CONFIG_DIR")
	dir := GetConfigDir()
	if dir == "" {
		t.Error("GetConfigDir() should return non-empty default")
	}
}
