package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/dave/naga/internal/infra/tailscale"
)

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Monitor device resources",
		Long:  "Monitor CPU, Memory, and Disk usage on devices",
	}

	cmd.AddCommand(newMonitorShowCmd())
	cmd.AddCommand(newMonitorWatchCmd())

	return cmd
}

func newMonitorShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <device-name-or-id>",
		Short: "Show resource usage for a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deviceName := args[0]

			cfg, err := getConfig()
			if err != nil {
				return err
			}

			client := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			device, err := client.GetDevice(ctx, deviceName)
			if err != nil {
				return fmt.Errorf("failed to get device: %w", err)
			}

			fmt.Printf("Device: %s (%s)\n", device.Name, device.TailscaleIP)
			fmt.Printf("OS: %s\n", device.OS)
			fmt.Println()

			// TODO: Collect metrics via SSH
			fmt.Println("Resource Usage:")
			fmt.Println("  (SSH collection not yet implemented)")
			fmt.Println()
			fmt.Println("  CPU:    - %")
			fmt.Println("  Memory: - / - GB")
			fmt.Println("  Disk:   - / - GB")

			return nil
		},
	}

	return cmd
}

func newMonitorWatchCmd() *cobra.Command {
	var interval int

	cmd := &cobra.Command{
		Use:   "watch [device-name-or-id...]",
		Short: "Watch resource usage in real-time",
		Long: `Watch resource usage for one or more devices in real-time.
If no devices specified, watches all online devices.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := getConfig()
			if err != nil {
				return err
			}

			client := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			devices, err := client.ListDevices(ctx)
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}

			// Filter to requested devices or online devices
			var targetDevices []string
			if len(args) > 0 {
				targetDevices = args
			} else {
				for _, d := range devices {
					if d.IsOnline() {
						targetDevices = append(targetDevices, d.Name)
					}
				}
			}

			fmt.Printf("Watching %d devices (interval: %ds)\n", len(targetDevices), interval)
			fmt.Println("Press Ctrl+C to stop")
			fmt.Println()

			// TODO: Implement real-time monitoring with SSH
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "DEVICE\tCPU\tMEMORY\tDISK")
			fmt.Fprintln(w, "------\t---\t------\t----")

			for _, name := range targetDevices {
				fmt.Fprintf(w, "%s\t-%%\t-%%\t-%%\n", name)
			}
			w.Flush()

			fmt.Println("\n(Real-time monitoring not yet implemented)")

			return nil
		},
	}

	cmd.Flags().IntVarP(&interval, "interval", "i", 5, "Refresh interval in seconds")

	return cmd
}
