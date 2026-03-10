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
	APIKey     string `mapstructure:"api_key"`
	Tailnet    string `mapstructure:"tailnet"`
	BaseURL    string `mapstructure:"base_url"`
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

// RayConfig holds Ray cluster settings
type RayConfig struct {
	DefaultPort          int    `mapstructure:"default_port"`
	DefaultDashboardPort int    `mapstructure:"default_dashboard_port"`
	PythonPath           string `mapstructure:"python_path"`
	AutoInstall          bool   `mapstructure:"auto_install"`
	DefaultVersion       string `mapstructure:"default_version"`
}

// ServerConfig holds web server settings
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
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
	HeartbeatInterval   int    `mapstructure:"heartbeat_interval"`
	HealthCheckInterval int    `mapstructure:"healthcheck_interval"`
	FailureTimeout      int    `mapstructure:"failure_timeout"`
	CheckpointDir       string `mapstructure:"checkpoint_dir"`
	AnthropicAPIKey     string `mapstructure:"anthropic_api_key"`
	AgentPort           int    `mapstructure:"agent_port"`
}

// DefaultConfig returns a Config with default values
func DefaultConfig() *Config {
	return &Config{
		Tailscale: TailscaleConfig{
			BaseURL: "https://api.tailscale.com",
		},
		SSH: SSHConfig{
			User:            os.Getenv("USER"),
			PrivateKeyPath:  filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa"),
			Port:            22,
			Timeout:         30,
			UseTailscaleSSH: true,
		},
		Ray: RayConfig{
			DefaultPort:          6379,
			DefaultDashboardPort: 8265,
			PythonPath:           "python3",
			AutoInstall:          true,
			DefaultVersion:       "2.9.0",
		},
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(getConfigDir(), "clusterctl.db"),
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

	// Set config name and paths
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(getConfigDir())
	viper.AddConfigPath(".")

	// Environment variables
	viper.SetEnvPrefix("CLUSTERCTL")
	viper.AutomaticEnv()

	// Map environment variables
	viper.BindEnv("tailscale.api_key", "TAILSCALE_API_KEY")
	viper.BindEnv("tailscale.tailnet", "TAILSCALE_TAILNET")
	viper.BindEnv("tailscale.oauth_client_id", "TAILSCALE_OAUTH_CLIENT_ID")
	viper.BindEnv("tailscale.oauth_client_secret", "TAILSCALE_OAUTH_CLIENT_SECRET")
	viper.BindEnv("ssh.user", "CLUSTERCTL_SSH_USER")
	viper.BindEnv("ssh.private_key_path", "CLUSTERCTL_SSH_KEY")
	viper.BindEnv("database.dsn", "CLUSTERCTL_DATABASE_DSN")

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

	return cfg, nil
}

// Save saves the current configuration to file
func Save(cfg *Config) error {
	configDir := getConfigDir()
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

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

	configPath := filepath.Join(configDir, "config.yaml")
	return viper.WriteConfigAs(configPath)
}

// getConfigDir returns the configuration directory path
func getConfigDir() string {
	if dir := os.Getenv("CLUSTERCTL_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clusterctl")
}

// GetConfigDir exports the config directory path
func GetConfigDir() string {
	return getConfigDir()
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Tailscale.APIKey == "" && c.Tailscale.OAuthClientID == "" {
		return fmt.Errorf("tailscale API key or OAuth credentials required")
	}
	return nil
}
