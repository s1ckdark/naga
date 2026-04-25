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
	original := os.Getenv("NAGA_CONFIG_DIR")
	defer os.Setenv("NAGA_CONFIG_DIR", original)

	os.Setenv("NAGA_CONFIG_DIR", "/tmp/test-config")
	if dir := GetConfigDir(); dir != "/tmp/test-config" {
		t.Errorf("GetConfigDir() = %q, want /tmp/test-config", dir)
	}
}

func TestGetConfigDir_Default(t *testing.T) {
	original := os.Getenv("NAGA_CONFIG_DIR")
	defer os.Setenv("NAGA_CONFIG_DIR", original)

	os.Unsetenv("NAGA_CONFIG_DIR")
	dir := GetConfigDir()
	if dir == "" {
		t.Error("GetConfigDir() should return non-empty default")
	}
}

func TestAIConfig_Resolve_FallsBackToDefault(t *testing.T) {
	cfg := AIConfig{
		Default: ProviderConfig{Provider: "claude", APIKey: "sk-default"},
	}
	got := cfg.Resolve("head")
	if got.Provider != "claude" || got.APIKey != "sk-default" {
		t.Errorf("Resolve(head) = %+v; want claude/sk-default default fallback", got)
	}
}

func TestAIConfig_Resolve_UsesRoleOverride(t *testing.T) {
	override := ProviderConfig{Provider: "ollama", Endpoint: "http://localhost:11434", Model: "llama3"}
	cfg := AIConfig{
		Default:        ProviderConfig{Provider: "claude", APIKey: "sk-default"},
		TaskScheduling: &override,
	}
	got := cfg.Resolve("schedule")
	if got.Provider != "ollama" || got.Endpoint != "http://localhost:11434" {
		t.Errorf("Resolve(schedule) = %+v; want ollama override", got)
	}
	if got := cfg.Resolve("head"); got.Provider != "claude" {
		t.Errorf("Resolve(head) should fall back to default, got %+v", got)
	}
}

func TestAIConfig_Resolve_EmptyOverrideFallsThrough(t *testing.T) {
	empty := ProviderConfig{}
	cfg := AIConfig{
		Default:       ProviderConfig{Provider: "claude", APIKey: "sk-default"},
		HeadSelection: &empty,
	}
	if got := cfg.Resolve("head"); got.Provider != "claude" {
		t.Errorf("Resolve should ignore empty-Provider override; got %+v", got)
	}
}

func TestMigrateLegacyAgentAI_ClaudeKey(t *testing.T) {
	agent := AgentConfig{
		AIProvider:      "claude",
		AnthropicAPIKey: "sk-ant-legacy",
	}
	migrateLegacyAgentAI(&agent)
	if agent.AI.Default.Provider != "claude" {
		t.Errorf("AI.Default.Provider = %q; want claude", agent.AI.Default.Provider)
	}
	if agent.AI.Default.APIKey != "sk-ant-legacy" {
		t.Errorf("AI.Default.APIKey = %q; want sk-ant-legacy", agent.AI.Default.APIKey)
	}
}

func TestMigrateLegacyAgentAI_OpenAIReusesKey(t *testing.T) {
	agent := AgentConfig{
		AIProvider:      "openai",
		AnthropicAPIKey: "sk-openai-legacy",
	}
	migrateLegacyAgentAI(&agent)
	if agent.AI.Default.Provider != "openai" || agent.AI.Default.APIKey != "sk-openai-legacy" {
		t.Errorf("openai legacy migration failed: %+v", agent.AI.Default)
	}
}

func TestMigrateLegacyAgentAI_OllamaEndpoint(t *testing.T) {
	agent := AgentConfig{
		AIProvider:     "ollama",
		OllamaEndpoint: "http://localhost:11434",
		OllamaModel:    "llama3",
	}
	migrateLegacyAgentAI(&agent)
	if agent.AI.Default.Provider != "ollama" ||
		agent.AI.Default.Endpoint != "http://localhost:11434" ||
		agent.AI.Default.Model != "llama3" {
		t.Errorf("ollama legacy migration failed: %+v", agent.AI.Default)
	}
}

func TestMigrateLegacyAgentAI_SkipsWhenAIDefaultPresent(t *testing.T) {
	agent := AgentConfig{
		AIProvider:      "claude",
		AnthropicAPIKey: "sk-legacy",
		AI: AIConfig{
			Default: ProviderConfig{Provider: "openai", APIKey: "sk-new"},
		},
	}
	migrateLegacyAgentAI(&agent)
	if agent.AI.Default.Provider != "openai" {
		t.Errorf("migration overwrote existing AI.Default: %+v", agent.AI.Default)
	}
}

func TestConfig_SaveAndLoad_AIConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("NAGA_CONFIG_DIR", tmpDir)
	// Tailscale.APIKey required by Validate(); not used here but keep the file loadable.
	cfg := DefaultConfig()
	cfg.Tailscale.APIKey = "tskey-roundtrip"
	override := ProviderConfig{Provider: "ollama", Endpoint: "http://localhost:11434", Model: "llama3"}
	cfg.Agent.AI = AIConfig{
		Default:        ProviderConfig{Provider: "claude", APIKey: "sk-default", Model: "claude-sonnet-4-6"},
		TaskScheduling: &override,
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Agent.AI.Default.Provider != "claude" || loaded.Agent.AI.Default.APIKey != "sk-default" {
		t.Errorf("Default not persisted: %+v", loaded.Agent.AI.Default)
	}
	if loaded.Agent.AI.TaskScheduling == nil || loaded.Agent.AI.TaskScheduling.Provider != "ollama" {
		t.Errorf("TaskScheduling override not persisted: %+v", loaded.Agent.AI.TaskScheduling)
	}
}

func TestConfig_SaveAndLoad_AIConfig_ClearsRoleOverride(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("NAGA_CONFIG_DIR", tmpDir)

	cfg := DefaultConfig()
	cfg.Tailscale.APIKey = "tskey-clear"
	override := ProviderConfig{Provider: "ollama", Endpoint: "http://localhost:11434"}
	cfg.Agent.AI = AIConfig{
		Default:        ProviderConfig{Provider: "claude", APIKey: "sk-default"},
		TaskScheduling: &override,
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save 1: %v", err)
	}

	// Now transition TaskScheduling to nil and Save again in the same process.
	cfg.Agent.AI.TaskScheduling = nil
	if err := Save(cfg); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Agent.AI.TaskScheduling != nil && loaded.Agent.AI.TaskScheduling.Provider != "" {
		t.Errorf("TaskScheduling sub-keys not cleared on nil transition: %+v", loaded.Agent.AI.TaskScheduling)
	}
}
