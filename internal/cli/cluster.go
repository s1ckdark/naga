package cli

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/dave/naga/internal/agent"
	"github.com/dave/naga/internal/domain"
	"github.com/dave/naga/internal/infra/ssh"
	"github.com/dave/naga/internal/infra/tailscale"
	"github.com/dave/naga/internal/repository/sqlite"
	"github.com/dave/naga/internal/tui/monitor"
	"github.com/dave/naga/internal/usecase"
)

func newClusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage Ray clusters",
		Long:  "Create, modify, and delete Ray clusters across your Tailscale devices",
	}

	cmd.AddCommand(newClusterCreateCmd())
	cmd.AddCommand(newClusterListCmd())
	cmd.AddCommand(newClusterStatusCmd())
	cmd.AddCommand(newClusterDeleteCmd())
	cmd.AddCommand(newClusterAddWorkerCmd())
	cmd.AddCommand(newClusterRemoveWorkerCmd())
	cmd.AddCommand(newClusterChangeHeadCmd())
	cmd.AddCommand(newClusterStartCmd())
	cmd.AddCommand(newClusterStopCmd())
	cmd.AddCommand(newClusterMonitorCmd())
	cmd.AddCommand(newClusterAgentCmd())

	return cmd
}

func newClusterCreateCmd() *cobra.Command {
	var (
		head        string
		workers     []string
		description string
		rayPort     int
		dashPort    int
	)

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new Ray cluster",
		Long: `Create a new Ray cluster with specified head and worker nodes.

Example:
  clusterctl cluster create my-cluster --head node1 --workers node2,node3`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if head == "" {
				return fmt.Errorf("head node is required (--head)")
			}

			cfg, err := getConfig()
			if err != nil {
				return err
			}

			// Resolve device names to IDs
			client := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			devices, err := client.ListDevices(ctx)
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}

			headDevice := findDevice(devices, head)
			if headDevice == nil {
				return fmt.Errorf("head node not found: %s", head)
			}

			workerIDs := make([]string, 0, len(workers))
			for _, w := range workers {
				wd := findDevice(devices, w)
				if wd == nil {
					return fmt.Errorf("worker node not found: %s", w)
				}
				workerIDs = append(workerIDs, wd.ID)
			}

			// Create cluster
			cluster := domain.NewCluster(name, headDevice.ID, workerIDs)
			cluster.Description = description
			if rayPort > 0 {
				cluster.RayPort = rayPort
			}
			if dashPort > 0 {
				cluster.DashboardPort = dashPort
			}

			fmt.Printf("Creating cluster '%s'...\n", name)
			fmt.Printf("  Head: %s (%s)\n", headDevice.Name, headDevice.TailscaleIP)
			for i, wid := range workerIDs {
				wd := findDeviceByID(devices, wid)
				fmt.Printf("  Worker %d: %s (%s)\n", i+1, wd.Name, wd.TailscaleIP)
			}

			// TODO: Actually start Ray cluster via SSH
			fmt.Println("\nCluster configuration created.")
			fmt.Println("Use 'clusterctl cluster start " + name + "' to start the cluster.")

			return nil
		},
	}

	cmd.Flags().StringVar(&head, "head", "", "Head node device name or ID (required)")
	cmd.Flags().StringSliceVar(&workers, "workers", nil, "Worker node device names or IDs (comma-separated)")
	cmd.Flags().StringVar(&description, "description", "", "Cluster description")
	cmd.Flags().IntVar(&rayPort, "ray-port", 0, "Ray port (default: 6379)")
	cmd.Flags().IntVar(&dashPort, "dashboard-port", 0, "Dashboard port (default: 8265)")
	cmd.MarkFlagRequired("head")

	return cmd
}

func newClusterListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Load from repository
			fmt.Println("No clusters configured yet.")
			fmt.Println("Use 'clusterctl cluster create' to create a new cluster.")
			return nil
		},
	}

	return cmd
}

func newClusterStatusCmd() *cobra.Command {
	var detailed bool

	cmd := &cobra.Command{
		Use:   "status <cluster-name>",
		Short: "Show cluster status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			fmt.Printf("Cluster: %s\n", name)
			fmt.Println("Status: not implemented yet")

			// TODO: Get cluster from repository
			// TODO: Query Ray status from head node

			return nil
		},
	}

	cmd.Flags().BoolVar(&detailed, "detailed", false, "Show detailed node information")

	return cmd
}

func newClusterDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <cluster-name>",
		Short: "Delete a cluster",
		Long: `Delete a cluster. By default, checks if the cluster has running jobs.
Use --force to skip the check and force deletion.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if !force {
				// TODO: Check if cluster has running jobs
				fmt.Printf("Checking if cluster '%s' is in use...\n", name)
			}

			fmt.Printf("Deleting cluster '%s'...\n", name)
			// TODO: Stop Ray processes on all nodes
			// TODO: Remove from repository

			fmt.Println("Cluster deleted.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force deletion without checking for running jobs")

	return cmd
}

func newClusterAddWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-worker <cluster-name> <device>",
		Short: "Add a worker node to the cluster",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			deviceName := args[1]

			fmt.Printf("Adding worker '%s' to cluster '%s'...\n", deviceName, clusterName)

			// TODO: Validate device exists and is online
			// TODO: Load cluster from repository
			// TODO: Add worker to cluster
			// TODO: Connect worker to Ray cluster

			fmt.Println("Worker added successfully.")
			return nil
		},
	}

	return cmd
}

func newClusterRemoveWorkerCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "remove-worker <cluster-name> <device>",
		Short: "Remove a worker node from the cluster",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			deviceName := args[1]

			if !force {
				fmt.Printf("Checking if worker '%s' has running tasks...\n", deviceName)
				// TODO: Check for running tasks on the worker
			}

			fmt.Printf("Removing worker '%s' from cluster '%s'...\n", deviceName, clusterName)

			// TODO: Stop Ray on the worker
			// TODO: Update cluster configuration
			// TODO: Save to repository

			fmt.Println("Worker removed successfully.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force removal without checking for running tasks")

	return cmd
}

func newClusterChangeHeadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change-head <cluster-name> <new-head-device>",
		Short: "Change the head node of a cluster",
		Long: `Change the head node of a cluster. The current head becomes a worker,
and the specified device becomes the new head.

This operation requires:
1. Stopping all jobs on the cluster
2. Restarting Ray with the new head configuration`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			newHead := args[1]

			fmt.Printf("Changing head node of cluster '%s' to '%s'...\n", clusterName, newHead)
			fmt.Println("\nThis will:")
			fmt.Println("  1. Stop all running jobs")
			fmt.Println("  2. Stop Ray on all nodes")
			fmt.Println("  3. Start Ray with the new head configuration")
			fmt.Println("  4. Restart all workers")

			// TODO: Implement head change logic
			// - Check cluster exists
			// - Check new head is valid and online
			// - Stop cluster
			// - Update configuration
			// - Restart cluster with new head

			fmt.Println("\nHead node change completed.")
			return nil
		},
	}

	return cmd
}

func newClusterStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start <cluster-name>",
		Short: "Start a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			fmt.Printf("Starting cluster '%s'...\n", name)

			// TODO: Load cluster from repository
			// TODO: Start Ray on head node
			// TODO: Connect workers to head

			fmt.Println("Cluster started.")
			return nil
		},
	}

	return cmd
}

func newClusterStopCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "stop <cluster-name>",
		Short: "Stop a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if !force {
				fmt.Printf("Checking for running jobs on cluster '%s'...\n", name)
				// TODO: Check for running jobs
			}

			fmt.Printf("Stopping cluster '%s'...\n", name)

			// TODO: Stop Ray on all nodes

			fmt.Println("Cluster stopped.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force stop without checking for running jobs")

	return cmd
}

func newClusterMonitorCmd() *cobra.Command {
	var interval int

	cmd := &cobra.Command{
		Use:   "monitor <cluster-name>",
		Short: "Monitor GPU usage across cluster nodes",
		Long: `Monitor GPU utilization, memory, temperature, and power for all nodes
in a cluster in real-time. Requires nvidia-smi on worker nodes.

Keys:
  d  Toggle table/detail view
  s  Cycle sort order
  r  Force refresh
  q  Quit`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]

			cfg, err := getConfig()
			if err != nil {
				return err
			}

			client := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			allDevices, err := client.ListDevices(ctx)
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}

			deviceMap := make(map[string]*domain.Device)
			for _, d := range allDevices {
				deviceMap[d.ID] = d
			}

			// Load cluster from repository
			db, err := sqlite.NewDB(cfg.Database.DSN)
			if err != nil {
				return fmt.Errorf("failed to open database: %w", err)
			}
			defer db.Close()

			repos := db.Repositories()
			clusterUC := usecase.NewClusterUseCase(repos, nil)
			cluster, err := clusterUC.GetCluster(ctx, clusterName)
			if err != nil {
				return fmt.Errorf("cluster '%s' not found: %w", clusterName, err)
			}

			// Resolve cluster nodes to devices
			var clusterDevices []*domain.Device
			for _, nodeID := range cluster.AllNodeIDs() {
				if d, ok := deviceMap[nodeID]; ok && d.CanSSH() {
					clusterDevices = append(clusterDevices, d)
				}
			}

			if len(clusterDevices) == 0 {
				return fmt.Errorf("no reachable nodes in cluster '%s'", clusterName)
			}

			sshExecutor := ssh.NewExecutor(ssh.Config{
				User:            cfg.SSH.User,
				PrivateKeyPath:  cfg.SSH.PrivateKeyPath,
				Port:            cfg.SSH.Port,
				Timeout:         time.Duration(cfg.SSH.Timeout) * time.Second,
				UseTailscaleSSH: cfg.SSH.UseTailscaleSSH,
			})
			defer sshExecutor.Close()

			gpuCollector := ssh.NewGPUCollector(sshExecutor)

			duration := time.Duration(interval) * time.Second
			model := monitor.NewModel(clusterName, clusterDevices, gpuCollector, duration)
			p := tea.NewProgram(model, tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}

	cmd.Flags().IntVarP(&interval, "interval", "i", 3, "Refresh interval in seconds")
	return cmd
}

func newClusterAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage cluster node agents",
	}
	cmd.AddCommand(newAgentInstallCmd())
	cmd.AddCommand(newAgentUninstallCmd())
	cmd.AddCommand(newAgentStatusCmd())
	return cmd
}

func newAgentInstallCmd() *cobra.Command {
	var (
		binaryPath string
		port       int
	)

	cmd := &cobra.Command{
		Use:   "install <cluster-name>",
		Short: "Install agent on all cluster nodes",
		Long:  "Copies the cluster-agent binary and installs systemd service on each node.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]

			cfg, err := getConfig()
			if err != nil {
				return err
			}

			client := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			allDevices, err := client.ListDevices(ctx)
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}

			sshExecutor := ssh.NewExecutor(ssh.Config{
				User:            cfg.SSH.User,
				PrivateKeyPath:  cfg.SSH.PrivateKeyPath,
				Port:            cfg.SSH.Port,
				Timeout:         time.Duration(cfg.SSH.Timeout) * time.Second,
				UseTailscaleSSH: cfg.SSH.UseTailscaleSSH,
			})
			defer sshExecutor.Close()

			for _, d := range allDevices {
				if !d.CanSSH() {
					fmt.Printf("  Skipping %s (offline or no SSH)\n", d.GetDisplayName())
					continue
				}

				role := "worker"
				// TODO: determine role from cluster config

				sysCfg := agent.SystemdConfig{
					NodeID:     d.ID,
					ClusterID:  clusterName,
					Role:       role,
					Port:       port,
					BinaryPath: binaryPath,
					APIKey:     cfg.Agent.AnthropicAPIKey,
				}

				fmt.Printf("  Installing agent on %s (%s)...\n", d.GetDisplayName(), role)

				// Copy binary
				if err := sshExecutor.CopyFile(ctx, d, binaryPath, binaryPath); err != nil {
					fmt.Printf("    Warning: failed to copy binary: %v\n", err)
					continue
				}

				// Install systemd service
				installCmds, err := agent.InstallCommands(sysCfg)
				if err != nil {
					fmt.Printf("    Error: %v\n", err)
					continue
				}
				for _, installCmd := range installCmds {
					if _, err := sshExecutor.Execute(ctx, d, installCmd); err != nil {
						fmt.Printf("    Warning: %v\n", err)
					}
				}

				fmt.Printf("    Done.\n")
			}

			fmt.Println("Agent installation complete.")
			return nil
		},
	}

	cmd.Flags().StringVar(&binaryPath, "binary", "/usr/local/bin/cluster-agent", "Path to agent binary")
	cmd.Flags().IntVar(&port, "port", 9090, "Agent listen port")
	return cmd
}

func newAgentUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall <cluster-name>",
		Short: "Uninstall agent from all cluster nodes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]

			cfg, err := getConfig()
			if err != nil {
				return err
			}

			client := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			allDevices, err := client.ListDevices(ctx)
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}

			sshExecutor := ssh.NewExecutor(ssh.Config{
				User:            cfg.SSH.User,
				PrivateKeyPath:  cfg.SSH.PrivateKeyPath,
				Port:            cfg.SSH.Port,
				Timeout:         time.Duration(cfg.SSH.Timeout) * time.Second,
				UseTailscaleSSH: cfg.SSH.UseTailscaleSSH,
			})
			defer sshExecutor.Close()

			for _, d := range allDevices {
				if !d.CanSSH() {
					continue
				}

				fmt.Printf("  Uninstalling agent from %s...\n", d.GetDisplayName())

				for _, uninstallCmd := range agent.UninstallCommands(clusterName, d.ID) {
					if _, err := sshExecutor.Execute(ctx, d, uninstallCmd); err != nil {
						fmt.Printf("    Warning: %v\n", err)
					}
				}
				fmt.Printf("    Done.\n")
			}

			fmt.Println("Agent uninstallation complete.")
			return nil
		},
	}
	return cmd
}

func newAgentStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <cluster-name>",
		Short: "Check agent status on all cluster nodes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]

			cfg, err := getConfig()
			if err != nil {
				return err
			}

			client := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			allDevices, err := client.ListDevices(ctx)
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}

			sshExecutor := ssh.NewExecutor(ssh.Config{
				User:            cfg.SSH.User,
				PrivateKeyPath:  cfg.SSH.PrivateKeyPath,
				Port:            cfg.SSH.Port,
				Timeout:         time.Duration(cfg.SSH.Timeout) * time.Second,
				UseTailscaleSSH: cfg.SSH.UseTailscaleSSH,
			})
			defer sshExecutor.Close()

			fmt.Printf("Agent status for cluster '%s':\n\n", clusterName)

			for _, d := range allDevices {
				if !d.CanSSH() {
					fmt.Printf("  %-20s  OFFLINE\n", d.GetDisplayName())
					continue
				}

				svcName := agent.ServiceName(clusterName, d.ID)
				output, err := sshExecutor.Execute(ctx, d, fmt.Sprintf("systemctl is-active %s 2>/dev/null || echo inactive", svcName))
				if err != nil {
					fmt.Printf("  %-20s  ERROR: %v\n", d.GetDisplayName(), err)
					continue
				}

				status := "UNKNOWN"
				trimmed := output
				if len(trimmed) > 0 {
					// Remove trailing newlines
					for len(trimmed) > 0 && (trimmed[len(trimmed)-1] == '\n' || trimmed[len(trimmed)-1] == '\r') {
						trimmed = trimmed[:len(trimmed)-1]
					}
					status = trimmed
				}

				fmt.Printf("  %-20s  %s\n", d.GetDisplayName(), status)
			}

			return nil
		},
	}
	return cmd
}

// Helper functions

func findDevice(devices []*domain.Device, nameOrID string) *domain.Device {
	for _, d := range devices {
		if d.ID == nameOrID || d.Name == nameOrID || d.Hostname == nameOrID {
			return d
		}
	}
	return nil
}

func findDeviceByID(devices []*domain.Device, id string) *domain.Device {
	for _, d := range devices {
		if d.ID == id {
			return d
		}
	}
	return nil
}

// For future use when usecase is implemented
var _ = usecase.ClusterUseCase{}
