package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/dave/naga/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Long:  "View and modify clusterctl configuration",
	}

	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigInitCmd())

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			// Mask sensitive values
			maskedAPIKey := maskSecret(cfg.Tailscale.APIKey)
			maskedOAuthSecret := maskSecret(cfg.Tailscale.OAuthClientSecret)

			fmt.Println("Current Configuration:")
			fmt.Println()
			fmt.Println("Tailscale:")
			fmt.Printf("  API Key:      %s\n", maskedAPIKey)
			fmt.Printf("  Tailnet:      %s\n", cfg.Tailscale.Tailnet)
			fmt.Printf("  Base URL:     %s\n", cfg.Tailscale.BaseURL)
			fmt.Printf("  OAuth Client: %s\n", cfg.Tailscale.OAuthClientID)
			fmt.Printf("  OAuth Secret: %s\n", maskedOAuthSecret)
			fmt.Println()
			fmt.Println("SSH:")
			fmt.Printf("  User:            %s\n", cfg.SSH.User)
			fmt.Printf("  Private Key:     %s\n", cfg.SSH.PrivateKeyPath)
			fmt.Printf("  Port:            %d\n", cfg.SSH.Port)
			fmt.Printf("  Timeout:         %ds\n", cfg.SSH.Timeout)
			fmt.Printf("  Tailscale SSH:   %v\n", cfg.SSH.UseTailscaleSSH)
			fmt.Println()
			fmt.Println("Ray:")
			fmt.Printf("  Default Port:      %d\n", cfg.Ray.DefaultPort)
			fmt.Printf("  Dashboard Port:    %d\n", cfg.Ray.DefaultDashboardPort)
			fmt.Printf("  Python Path:       %s\n", cfg.Ray.PythonPath)
			fmt.Printf("  Auto Install:      %v\n", cfg.Ray.AutoInstall)
			fmt.Printf("  Default Version:   %s\n", cfg.Ray.DefaultVersion)
			fmt.Println()
			fmt.Println("Server:")
			fmt.Printf("  Host: %s\n", cfg.Server.Host)
			fmt.Printf("  Port: %d\n", cfg.Server.Port)
			fmt.Println()
			fmt.Println("Database:")
			fmt.Printf("  Driver: %s\n", cfg.Database.Driver)
			fmt.Printf("  DSN:    %s\n", cfg.Database.DSN)
			fmt.Println()
			fmt.Printf("Config Dir: %s\n", config.GetConfigDir())

			return nil
		},
	}

	return cmd
}

func newConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value. Available keys:
  api-key          Tailscale API key
  tailnet          Tailscale tailnet name
  ssh-user         SSH username
  ssh-key          SSH private key path
  ssh-port         SSH port
  ssh-timeout      SSH connection timeout`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			cfg, err := config.Load()
			if err != nil {
				cfg = config.DefaultConfig()
			}

			switch key {
			case "api-key":
				cfg.Tailscale.APIKey = value
			case "tailnet":
				cfg.Tailscale.Tailnet = value
			case "ssh-user":
				cfg.SSH.User = value
			case "ssh-key":
				cfg.SSH.PrivateKeyPath = value
			case "ssh-port":
				var port int
				if _, err := fmt.Sscanf(value, "%d", &port); err != nil {
					return fmt.Errorf("invalid port: %s", value)
				}
				cfg.SSH.Port = port
			case "ssh-timeout":
				var timeout int
				if _, err := fmt.Sscanf(value, "%d", &timeout); err != nil {
					return fmt.Errorf("invalid timeout: %s", value)
				}
				cfg.SSH.Timeout = timeout
			default:
				return fmt.Errorf("unknown config key: %s", key)
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("Set %s = %s\n", key, maskIfSensitive(key, value))
			return nil
		},
	}

	return cmd
}

func newConfigInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.DefaultConfig()

			fmt.Println("Cluster Manager Configuration")
			fmt.Println("=============================")
			fmt.Println()

			// Tailscale API Key
			fmt.Print("Tailscale API Key: ")
			var apiKey string
			fmt.Scanln(&apiKey)
			if apiKey != "" {
				cfg.Tailscale.APIKey = apiKey
			}

			// Tailnet
			fmt.Print("Tailnet (e.g., example.com or tail1234.ts.net): ")
			var tailnet string
			fmt.Scanln(&tailnet)
			if tailnet != "" {
				cfg.Tailscale.Tailnet = tailnet
			}

			// SSH User
			fmt.Printf("SSH User [%s]: ", cfg.SSH.User)
			var sshUser string
			fmt.Scanln(&sshUser)
			if sshUser != "" {
				cfg.SSH.User = sshUser
			}

			// Save configuration
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Println()
			fmt.Printf("Configuration saved to %s/config.yaml\n", config.GetConfigDir())
			fmt.Println()
			fmt.Println("You can now use clusterctl:")
			fmt.Println("  clusterctl device list")
			fmt.Println("  clusterctl cluster create my-cluster --head node1 --workers node2")

			return nil
		},
	}

	return cmd
}

func maskSecret(s string) string {
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

func maskIfSensitive(key, value string) string {
	if key == "api-key" || key == "oauth-secret" {
		return maskSecret(value)
	}
	return value
}

// Ensure viper is used (for the linter)
var _ = viper.Get
