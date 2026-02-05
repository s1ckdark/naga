package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/dave/clusterctl/internal/domain"
	"github.com/dave/clusterctl/internal/infra/tailscale"
)

func newDeviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device",
		Short: "Manage Tailscale devices",
		Long:  "List, inspect, and manage devices in your Tailscale network",
	}

	cmd.AddCommand(newDeviceListCmd())
	cmd.AddCommand(newDeviceShowCmd())
	cmd.AddCommand(newDeviceCheckCmd())

	return cmd
}

func newDeviceListCmd() *cobra.Command {
	var (
		showOffline bool
		filterOS    string
		filterTag   string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all devices in the Tailscale network",
		Long:  "Displays all devices in your Tailscale network with their status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := getConfig()
			if err != nil {
				return err
			}

			if cfg.Tailscale.APIKey == "" {
				return fmt.Errorf("Tailscale API key not configured. Set TAILSCALE_API_KEY or use --api-key flag")
			}

			client := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			devices, err := client.ListDevices(ctx)
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}

			// Apply filters
			filtered := filterDevices(devices, showOffline, filterOS, filterTag)

			if outputFmt == "json" {
				return outputJSON(filtered)
			}

			// Table output
			printDeviceTable(filtered)
			return nil
		},
	}

	cmd.Flags().BoolVar(&showOffline, "all", false, "Show offline devices too")
	cmd.Flags().StringVar(&filterOS, "os", "", "Filter by OS (linux, macos, windows)")
	cmd.Flags().StringVar(&filterTag, "tag", "", "Filter by tag")

	return cmd
}

func newDeviceShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <device-name-or-id>",
		Short: "Show detailed information about a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := getConfig()
			if err != nil {
				return err
			}

			client := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			device, err := client.GetDevice(ctx, args[0])
			if err != nil {
				return fmt.Errorf("failed to get device: %w", err)
			}

			if outputFmt == "json" {
				return outputJSON(device)
			}

			printDeviceDetails(device)
			return nil
		},
	}

	return cmd
}

func newDeviceCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <device-name-or-id>",
		Short: "Check Ray installation status on a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := getConfig()
			if err != nil {
				return err
			}

			client := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			device, err := client.GetDevice(ctx, args[0])
			if err != nil {
				return fmt.Errorf("failed to get device: %w", err)
			}

			fmt.Printf("Device: %s (%s)\n", device.Name, device.TailscaleIP)
			fmt.Printf("OS: %s\n", device.OS)
			fmt.Printf("Status: %s\n", device.Status)

			// TODO: SSH check for Ray/Python installation
			fmt.Println("\nRay Status: (checking via SSH...)")
			fmt.Println("  Python: not checked yet")
			fmt.Println("  Ray: not checked yet")

			return nil
		},
	}

	return cmd
}

func filterDevices(devices []*domain.Device, showOffline bool, filterOS, filterTag string) []*domain.Device {
	var result []*domain.Device

	for _, d := range devices {
		// Skip offline unless requested
		if !showOffline && d.Status != domain.DeviceStatusOnline {
			continue
		}

		// OS filter
		if filterOS != "" && !strings.Contains(strings.ToLower(d.OS), strings.ToLower(filterOS)) {
			continue
		}

		// Tag filter
		if filterTag != "" {
			hasTag := false
			for _, t := range d.Tags {
				if strings.Contains(t, filterTag) {
					hasTag = true
					break
				}
			}
			if !hasTag {
				continue
			}
		}

		result = append(result, d)
	}

	return result
}

func printDeviceTable(devices []*domain.Device) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tIP\tOS\tSTATUS\tLAST SEEN")
	fmt.Fprintln(w, "----\t--\t--\t------\t---------")

	for _, d := range devices {
		lastSeen := d.LastSeen.Format("2006-01-02 15:04")
		if d.Status == domain.DeviceStatusOnline {
			lastSeen = "now"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			d.GetDisplayName(),
			d.TailscaleIP,
			d.OS,
			d.Status,
			lastSeen,
		)
	}
	w.Flush()

	fmt.Printf("\nTotal: %d devices\n", len(devices))
}

func printDeviceDetails(d *domain.Device) {
	fmt.Printf("Name:        %s\n", d.Name)
	fmt.Printf("Hostname:    %s\n", d.Hostname)
	fmt.Printf("ID:          %s\n", d.ID)
	fmt.Printf("Status:      %s\n", d.Status)
	fmt.Printf("OS:          %s\n", d.OS)
	fmt.Printf("Tailscale IP: %s\n", d.TailscaleIP)

	if len(d.IPAddresses) > 0 {
		fmt.Printf("IP Addresses: %s\n", strings.Join(d.IPAddresses, ", "))
	}

	if len(d.Tags) > 0 {
		fmt.Printf("Tags:        %s\n", strings.Join(d.Tags, ", "))
	}

	fmt.Printf("User:        %s\n", d.User)
	fmt.Printf("SSH Enabled: %v\n", d.SSHEnabled)
	fmt.Printf("Last Seen:   %s\n", d.LastSeen.Format(time.RFC3339))
	fmt.Printf("Created:     %s\n", d.CreatedAt.Format(time.RFC3339))
}
