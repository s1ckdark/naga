package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"github.com/dave/clusterctl/config"
)

var (
	cfgFile   string
	apiKey    string
	outputFmt string
)

// NewRootCmd creates the root command
func NewRootCmd(version, buildTime string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "clusterctl",
		Short: "Manage Tailscale devices and Ray clusters",
		Long: `clusterctl is a CLI tool for managing devices in your Tailscale network
and creating/managing Ray clusters across those devices.

It provides:
  - Device listing and monitoring
  - Ray cluster creation, modification, and deletion
  - Resource monitoring (CPU, Memory, Disk)
  - Head/Worker node management`,
		Version: fmt.Sprintf("%s (built: %s)", version, buildTime),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig()
		},
	}

	// Persistent flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ~/.clusterctl/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "Tailscale API key")
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "table", "Output format (table, json, yaml)")

	// Bind flags to viper
	viper.BindPFlag("tailscale.api_key", rootCmd.PersistentFlags().Lookup("api-key"))

	// Add subcommands
	rootCmd.AddCommand(newDeviceCmd())
	rootCmd.AddCommand(newClusterCmd())
	rootCmd.AddCommand(newMonitorCmd())
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newServerCmd())

	return rootCmd
}

func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	}

	_, err := config.Load()
	if err != nil {
		// Only warn if config file was explicitly specified
		if cfgFile != "" {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	return nil
}

// getConfig loads and returns the configuration
func getConfig() (*config.Config, error) {
	return config.Load()
}

// outputResult outputs the result in the specified format
func outputResult(data interface{}) error {
	switch outputFmt {
	case "json":
		return outputJSON(data)
	case "yaml":
		return outputYAML(data)
	default:
		return outputTable(data)
	}
}

func outputJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func outputYAML(data interface{}) error {
	bytes, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	fmt.Print(string(bytes))
	return nil
}

func outputTable(data interface{}) error {
	// Default implementation - subcommands override this
	fmt.Printf("%+v\n", data)
	return nil
}
