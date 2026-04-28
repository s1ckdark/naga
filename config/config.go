package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds all configuration for the application
type Config struct {
	// Tailscale settings
	Tailscale TailscaleConfig `mapstructure:"tailscale"`

	// SSH settings
	SSH SSHConfig `mapstructure:"ssh"`

	// Ray settings
	Ray RayConfig `mapstructure:"ray"`

	// Server settings
	Server ServerConfig `mapstructure:"server"`

	// Database settings
	Database DatabaseConfig `mapstructure:"database"`

	// Logging settings
	Log LogConfig `mapstructure:"log"`

	// Agent settings
	Agent AgentConfig `mapstructure:"agent"`
}

// TailscaleConfig holds Tailscale API settings
type TailscaleConfig struct {
	APIKey            string `mapstructure:"api_key"`
	Tailnet           string `mapstructure:"tailnet"`
	BaseURL           string `mapstructure:"base_url"`
	OAuthClientID     string `mapstructure:"oauth_client_id"`
	OAuthClientSecret string `mapstructure:"oauth_client_secret"`
}

// SSHConfig holds SSH connection settings
type SSHConfig struct {
	User           string `mapstructure:"user"`
	PrivateKeyPath string `mapstructure:"private_key_path"`
	Port           int    `mapstructure:"port"`
	Timeout        int    `mapstructure:"timeout"` // seconds
	UseTailscaleSSH bool  `mapstructure:"use_tailscale_ssh"`
}

// RayConfig holds Ray orch settings
type RayConfig struct {
	DefaultPort          int    `mapstructure:"default_port"`
	DefaultDashboardPort int    `mapstructure:"default_dashboard_port"`
	PythonPath           string `mapstructure:"python_path"`
	AutoInstall          bool   `mapstructure:"auto_install"`
	DefaultVersion       string `mapstructure:"default_version"`
}

// ServerConfig holds web server settings
type ServerConfig struct {
	Host           string   `mapstructure:"host"`
	Port           int      `mapstructure:"port"`
	APIKey         string   `mapstructure:"api_key"`
	CORSOrigins    []string `mapstructure:"cors_origins"`
}

// DatabaseConfig holds database settings
type DatabaseConfig struct {
	Driver string `mapstructure:"driver"` // sqlite, postgres
	DSN    string `mapstructure:"dsn"`
}

// LogConfig holds logging settings
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"` // json, text
}

// AgentConfig holds agent settings
type AgentConfig struct {
	HeartbeatInterval   int      `mapstructure:"heartbeat_interval"`
	HealthCheckInterval int      `mapstructure:"healthcheck_interval"`
	FailureTimeout      int      `mapstructure:"failure_timeout"`
	CheckpointDir       string   `mapstructure:"checkpoint_dir"`
	AnthropicAPIKey     string   `mapstructure:"anthropic_api_key"` // deprecated: use AI.Default
	AgentPort           int      `mapstructure:"agent_port"`
	AIProvider          string   `mapstructure:"ai_provider"`       // deprecated: use AI.Default.Provider
	OllamaEndpoint      string   `mapstructure:"ollama_endpoint"`   // deprecated: use AI.Default.Endpoint
	OllamaModel         string   `mapstructure:"ollama_model"`      // deprecated: use AI.Default.Model
	LMStudioEndpoint    string   `mapstructure:"lmstudio_endpoint"` // deprecated: use AI.Default.Endpoint
	LMStudioModel       string   `mapstructure:"lmstudio_model"`    // deprecated: use AI.Default.Model
	AI                  AIConfig `mapstructure:"ai"`
}

// ProviderConfig describes one AI provider instance.
// When Provider is "claude"/"openai"/"zai" the APIKey field is required.
// When Provider is "ollama"/"lmstudio"/"openai_compatible" the Endpoint field is required.
type ProviderConfig struct {
	Provider string `mapstructure:"provider"`
	APIKey   string `mapstructure:"api_key"`
	Endpoint string `mapstructure:"endpoint"`
	Model    string `mapstructure:"model"`
}

// AIConfig routes AI calls to providers per role.
// Default applies to any role without a non-empty override.
//
// AlwaysConsult promotes the AI from a tiebreaker to the primary scheduler:
// when true, every task assignment goes through the AI provider (subject to
// aiCallBudget), not just rule-based ties. Per-task `aiSchedule` overrides
// this default both ways.
type AIConfig struct {
	Default            ProviderConfig  `mapstructure:"default"`
	HeadSelection      *ProviderConfig `mapstructure:"head_selection"`
	TaskScheduling     *ProviderConfig `mapstructure:"task_scheduling"`
	CapacityEstimation *ProviderConfig `mapstructure:"capacity_estimation"`
	AlwaysConsult      bool            `mapstructure:"always_consult"`
}

// Resolve returns the ProviderConfig for a given role, falling back to Default
// when no override is set or the override has an empty Provider.
// role must be one of: "head", "schedule", "capacity".
func (a AIConfig) Resolve(role string) ProviderConfig {
	var override *ProviderConfig
	switch role {
	case "head":
		override = a.HeadSelection
	case "schedule":
		override = a.TaskScheduling
	case "capacity":
		override = a.CapacityEstimation
	}
	if override != nil && override.Provider != "" {
		return *override
	}
	return a.Default
}

// migrateLegacyAgentAI copies deprecated single-provider fields into the new
// AIConfig.Default structure. No-op when AI.Default.Provider is already set.
func migrateLegacyAgentAI(agent *AgentConfig) {
	if agent.AI.Default.Provider != "" {
		return
	}
	switch agent.AIProvider {
	case "claude":
		agent.AI.Default = ProviderConfig{Provider: "claude", APIKey: agent.AnthropicAPIKey}
	case "openai":
		// Legacy code reused AnthropicAPIKey as OpenAI key; preserve that quirk.
		agent.AI.Default = ProviderConfig{Provider: "openai", APIKey: agent.AnthropicAPIKey}
	case "ollama":
		agent.AI.Default = ProviderConfig{
			Provider: "ollama",
			Endpoint: agent.OllamaEndpoint,
			Model:    agent.OllamaModel,
		}
	case "lmstudio":
		agent.AI.Default = ProviderConfig{
			Provider: "lmstudio",
			Endpoint: agent.LMStudioEndpoint,
			Model:    agent.LMStudioModel,
		}
	}
}

// DefaultConfig returns a Config with default values
func DefaultConfig() *Config {
	return &Config{
		Tailscale: TailscaleConfig{
			Tailnet: "-",
			BaseURL: "https://api.tailscale.com",
		},
		SSH: SSHConfig{
			User:            os.Getenv("USER"),
			PrivateKeyPath:  filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa"),
			Port:            22,
			Timeout:         30,
			UseTailscaleSSH: false,
		},
		Ray: RayConfig{
			DefaultPort:          6379,
			DefaultDashboardPort: 8265,
			PythonPath:           "python3",
			AutoInstall:          true,
			DefaultVersion:       "2.9.0",
		},
		Server: ServerConfig{
			Host:        "127.0.0.1",
			Port:        8080,
			CORSOrigins: []string{"http://localhost:8080"},
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(getConfigDir(), "hydra.db"),
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
		Agent: AgentConfig{
			HeartbeatInterval:   3,
			HealthCheckInterval: 5,
			FailureTimeout:      15,
			CheckpointDir:       "/tmp/ray-checkpoints",
			AgentPort:           9090,
		},
	}
}

// Load loads configuration from file and environment variables
func Load() (*Config, error) {
	cfg := DefaultConfig()

	// Reset the global viper instance so prior in-memory Set() calls from
	// Save() do not bleed into this load (relevant when called multiple times
	// within the same process, e.g. after Save transitions).
	viper.Reset()

	// Set config name and paths
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(getConfigDir())
	viper.AddConfigPath(".")

	// Environment variables. Canonical prefix is HYDRA_; the legacy NAGA_
	// prefix is honored for every explicit binding so existing shells, systemd
	// units, and CI configs keep working without a flag day. `viper.BindEnv`
	// accepts multiple names and the first one actually set wins.
	viper.SetEnvPrefix("HYDRA")
	viper.AutomaticEnv()

	viper.BindEnv("tailscale.api_key", "TAILSCALE_API_KEY")
	viper.BindEnv("tailscale.oauth_client_id", "TAILSCALE_OAUTH_CLIENT_ID")
	viper.BindEnv("tailscale.oauth_client_secret", "TAILSCALE_OAUTH_CLIENT_SECRET")
	viper.BindEnv("ssh.user", "HYDRA_SSH_USER", "NAGA_SSH_USER")
	viper.BindEnv("ssh.private_key_path", "HYDRA_SSH_KEY", "NAGA_SSH_KEY")
	viper.BindEnv("database.dsn", "HYDRA_DATABASE_DSN", "NAGA_DATABASE_DSN")
	viper.BindEnv("server.api_key", "HYDRA_API_KEY", "NAGA_API_KEY")

	// Try to read config file
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
		// Config file not found, use defaults and env vars
	}

	// Unmarshal config
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	migrateLegacyAgentAI(&cfg.Agent)

	return cfg, nil
}

// Save saves the current configuration to file
func Save(cfg *Config) error {
	configDir := getConfigDir()
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Reset global viper so no stale in-memory keys from a previous Save call
	// within the same process can leak into the written YAML.
	viper.Reset()

	viper.Set("tailscale.api_key", cfg.Tailscale.APIKey)
	viper.Set("tailscale.tailnet", cfg.Tailscale.Tailnet)
	viper.Set("tailscale.base_url", cfg.Tailscale.BaseURL)
	viper.Set("ssh.user", cfg.SSH.User)
	viper.Set("ssh.private_key_path", cfg.SSH.PrivateKeyPath)
	viper.Set("ssh.port", cfg.SSH.Port)
	viper.Set("ssh.timeout", cfg.SSH.Timeout)
	viper.Set("ssh.use_tailscale_ssh", cfg.SSH.UseTailscaleSSH)
	viper.Set("ray.default_port", cfg.Ray.DefaultPort)
	viper.Set("ray.default_dashboard_port", cfg.Ray.DefaultDashboardPort)
	viper.Set("ray.auto_install", cfg.Ray.AutoInstall)
	viper.Set("server.host", cfg.Server.Host)
	viper.Set("server.port", cfg.Server.Port)
	viper.Set("database.driver", cfg.Database.Driver)
	viper.Set("database.dsn", cfg.Database.DSN)
	viper.Set("log.level", cfg.Log.Level)
	viper.Set("log.format", cfg.Log.Format)
	viper.Set("agent.ai_provider", cfg.Agent.AIProvider)
	viper.Set("agent.anthropic_api_key", cfg.Agent.AnthropicAPIKey)
	viper.Set("agent.ollama_endpoint", cfg.Agent.OllamaEndpoint)
	viper.Set("agent.ollama_model", cfg.Agent.OllamaModel)
	viper.Set("agent.lmstudio_endpoint", cfg.Agent.LMStudioEndpoint)
	viper.Set("agent.lmstudio_model", cfg.Agent.LMStudioModel)

	// AI config — new role-based structure
	viper.Set("agent.ai.default.provider", cfg.Agent.AI.Default.Provider)
	viper.Set("agent.ai.default.api_key", cfg.Agent.AI.Default.APIKey)
	viper.Set("agent.ai.default.endpoint", cfg.Agent.AI.Default.Endpoint)
	viper.Set("agent.ai.default.model", cfg.Agent.AI.Default.Model)
	setRoleOverride("agent.ai.head_selection", cfg.Agent.AI.HeadSelection)
	setRoleOverride("agent.ai.task_scheduling", cfg.Agent.AI.TaskScheduling)
	setRoleOverride("agent.ai.capacity_estimation", cfg.Agent.AI.CapacityEstimation)
	viper.Set("agent.ai.always_consult", cfg.Agent.AI.AlwaysConsult)

	configPath := filepath.Join(configDir, "config.yaml")
	return viper.WriteConfigAs(configPath)
}

// getConfigDir returns the configuration directory path.
// HYDRA_CONFIG_DIR is the canonical name; NAGA_CONFIG_DIR is honored as a
// legacy alias so existing dev environments aren't broken by the rename.
func getConfigDir() string {
	if dir := os.Getenv("HYDRA_CONFIG_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("NAGA_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hydra")
}

// GetConfigDir exports the config directory path
func GetConfigDir() string {
	return getConfigDir()
}

// setRoleOverride sets a role-override block in viper, or clears it if nil.
func setRoleOverride(key string, p *ProviderConfig) {
	if p == nil {
		// Explicitly clear nested keys; viper.Set(key, nil) alone does not
		// shadow already-set sub-keys within the same process.
		viper.Set(key+".provider", "")
		viper.Set(key+".api_key", "")
		viper.Set(key+".endpoint", "")
		viper.Set(key+".model", "")
		viper.Set(key, nil)
		return
	}
	viper.Set(key+".provider", p.Provider)
	viper.Set(key+".api_key", p.APIKey)
	viper.Set(key+".endpoint", p.Endpoint)
	viper.Set(key+".model", p.Model)
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Tailscale.APIKey == "" && c.Tailscale.OAuthClientID == "" {
		return fmt.Errorf("TAILSCALE_API_KEY or OAuth credentials required")
	}
	return nil
}
