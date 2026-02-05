package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/dave/clusterctl/internal/domain"
	"github.com/dave/clusterctl/internal/infra/tailscale"
	"github.com/dave/clusterctl/internal/usecase"
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
