package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/dave/clusterctl/internal/domain"
	"github.com/dave/clusterctl/internal/repository"
)

// ClusterUseCase handles cluster-related business logic
type ClusterUseCase struct {
	repos      *repository.Repositories
	rayManager RayManager
}

// RayManager interface for Ray cluster operations
type RayManager interface {
	// StartHead starts Ray as head node
	StartHead(ctx context.Context, device *domain.Device, port, dashboardPort int) error

	// StartWorker starts Ray as worker node
	StartWorker(ctx context.Context, device *domain.Device, headAddress string) error

	// StopRay stops Ray on a device
	StopRay(ctx context.Context, device *domain.Device) error

	// GetClusterInfo gets Ray cluster information from head node
	GetClusterInfo(ctx context.Context, headDevice *domain.Device) (*domain.RayClusterInfo, error)

	// CheckRayInstalled checks if Ray is installed on a device
	CheckRayInstalled(ctx context.Context, device *domain.Device) (bool, string, error)

	// InstallRay installs Ray on a device
	InstallRay(ctx context.Context, device *domain.Device, version string) error

	// HasRunningJobs checks if there are running jobs on the cluster
	HasRunningJobs(ctx context.Context, headDevice *domain.Device) (bool, error)
}

// NewClusterUseCase creates a new ClusterUseCase
func NewClusterUseCase(repos *repository.Repositories, rayManager RayManager) *ClusterUseCase {
	return &ClusterUseCase{
		repos:      repos,
		rayManager: rayManager,
	}
}

// CreateCluster creates a new cluster configuration
func (uc *ClusterUseCase) CreateCluster(ctx context.Context, name string, headID string, workerIDs []string) (*domain.Cluster, error) {
	// Check if cluster name already exists
	existing, _ := uc.repos.Clusters.GetByName(ctx, name)
	if existing != nil {
		return nil, domain.ErrClusterAlreadyExist
	}

	// Check if head node is already in a cluster
	existingCluster, _ := uc.repos.Clusters.GetClusterByDeviceID(ctx, headID)
	if existingCluster != nil {
		return nil, fmt.Errorf("head node is already in cluster: %s", existingCluster.Name)
	}

	// Check if any worker is already in a cluster
	for _, wid := range workerIDs {
		existingCluster, _ := uc.repos.Clusters.GetClusterByDeviceID(ctx, wid)
		if existingCluster != nil {
			return nil, fmt.Errorf("worker %s is already in cluster: %s", wid, existingCluster.Name)
		}
	}

	// Create cluster
	cluster := domain.NewCluster(name, headID, workerIDs)

	if err := uc.repos.Clusters.Create(ctx, cluster); err != nil {
		return nil, fmt.Errorf("failed to create cluster: %w", err)
	}

	return cluster, nil
}

// GetCluster retrieves a cluster by name
func (uc *ClusterUseCase) GetCluster(ctx context.Context, name string) (*domain.Cluster, error) {
	return uc.repos.Clusters.GetByName(ctx, name)
}

// ListClusters retrieves all clusters
func (uc *ClusterUseCase) ListClusters(ctx context.Context) ([]*domain.Cluster, error) {
	return uc.repos.Clusters.GetAll(ctx)
}

// StartCluster starts a Ray cluster
func (uc *ClusterUseCase) StartCluster(ctx context.Context, name string, devices map[string]*domain.Device) error {
	cluster, err := uc.repos.Clusters.GetByName(ctx, name)
	if err != nil {
		return domain.ErrClusterNotFound
	}

	if cluster.Status == domain.ClusterStatusRunning {
		return fmt.Errorf("cluster is already running")
	}

	// Update status
	cluster.Status = domain.ClusterStatusStarting
	now := time.Now()
	cluster.StartedAt = &now
	if err := uc.repos.Clusters.Update(ctx, cluster); err != nil {
		return err
	}

	// Get head device
	headDevice := devices[cluster.HeadNodeID]
	if headDevice == nil {
		cluster.SetError("head device not found")
		uc.repos.Clusters.Update(ctx, cluster)
		return fmt.Errorf("head device not found")
	}

	// Start head node
	if err := uc.rayManager.StartHead(ctx, headDevice, cluster.RayPort, cluster.DashboardPort); err != nil {
		cluster.SetError(fmt.Sprintf("failed to start head: %v", err))
		uc.repos.Clusters.Update(ctx, cluster)
		return fmt.Errorf("failed to start head node: %w", err)
	}

	// Start workers
	headAddress := fmt.Sprintf("%s:%d", headDevice.TailscaleIP, cluster.RayPort)
	for _, workerID := range cluster.WorkerIDs {
		workerDevice := devices[workerID]
		if workerDevice == nil {
			continue
		}

		if err := uc.rayManager.StartWorker(ctx, workerDevice, headAddress); err != nil {
			// Log error but continue with other workers
			fmt.Printf("Warning: failed to start worker %s: %v\n", workerDevice.Name, err)
		}
	}

	// Update status
	cluster.Status = domain.ClusterStatusRunning
	cluster.DashboardURL = fmt.Sprintf("http://%s:%d", headDevice.TailscaleIP, cluster.DashboardPort)
	cluster.UpdatedAt = time.Now()

	return uc.repos.Clusters.Update(ctx, cluster)
}

// StopCluster stops a Ray cluster
func (uc *ClusterUseCase) StopCluster(ctx context.Context, name string, devices map[string]*domain.Device, force bool) error {
	cluster, err := uc.repos.Clusters.GetByName(ctx, name)
	if err != nil {
		return domain.ErrClusterNotFound
	}

	if !force {
		// Check for running jobs
		headDevice := devices[cluster.HeadNodeID]
		if headDevice != nil {
			hasJobs, err := uc.rayManager.HasRunningJobs(ctx, headDevice)
			if err == nil && hasJobs {
				return domain.ErrClusterInUse
			}
		}
	}

	// Update status
	cluster.Status = domain.ClusterStatusStopping
	if err := uc.repos.Clusters.Update(ctx, cluster); err != nil {
		return err
	}

	// Stop all workers first
	for _, workerID := range cluster.WorkerIDs {
		workerDevice := devices[workerID]
		if workerDevice != nil {
			uc.rayManager.StopRay(ctx, workerDevice)
		}
	}

	// Stop head
	headDevice := devices[cluster.HeadNodeID]
	if headDevice != nil {
		uc.rayManager.StopRay(ctx, headDevice)
	}

	// Update status
	cluster.Status = domain.ClusterStatusStopped
	now := time.Now()
	cluster.StoppedAt = &now
	cluster.UpdatedAt = now

	return uc.repos.Clusters.Update(ctx, cluster)
}

// DeleteCluster deletes a cluster
func (uc *ClusterUseCase) DeleteCluster(ctx context.Context, name string, devices map[string]*domain.Device, force bool) error {
	cluster, err := uc.repos.Clusters.GetByName(ctx, name)
	if err != nil {
		return domain.ErrClusterNotFound
	}

	// Stop cluster if running
	if cluster.IsRunning() {
		if err := uc.StopCluster(ctx, name, devices, force); err != nil && !force {
			return err
		}
	}

	return uc.repos.Clusters.Delete(ctx, cluster.ID)
}

// AddWorker adds a worker to the cluster
func (uc *ClusterUseCase) AddWorker(ctx context.Context, clusterName string, deviceID string, device *domain.Device, headDevice *domain.Device) error {
	cluster, err := uc.repos.Clusters.GetByName(ctx, clusterName)
	if err != nil {
		return domain.ErrClusterNotFound
	}

	// Check if device is already in another cluster
	existingCluster, _ := uc.repos.Clusters.GetClusterByDeviceID(ctx, deviceID)
	if existingCluster != nil && existingCluster.ID != cluster.ID {
		return fmt.Errorf("device is already in cluster: %s", existingCluster.Name)
	}

	// Add worker to cluster configuration
	if err := cluster.AddWorker(deviceID); err != nil {
		return err
	}

	// If cluster is running, connect the new worker
	if cluster.IsRunning() && device != nil && headDevice != nil {
		headAddress := fmt.Sprintf("%s:%d", headDevice.TailscaleIP, cluster.RayPort)
		if err := uc.rayManager.StartWorker(ctx, device, headAddress); err != nil {
			return fmt.Errorf("failed to connect worker: %w", err)
		}
	}

	return uc.repos.Clusters.Update(ctx, cluster)
}

// RemoveWorker removes a worker from the cluster
func (uc *ClusterUseCase) RemoveWorker(ctx context.Context, clusterName string, deviceID string, device *domain.Device) error {
	cluster, err := uc.repos.Clusters.GetByName(ctx, clusterName)
	if err != nil {
		return domain.ErrClusterNotFound
	}

	// Remove worker from cluster configuration
	if err := cluster.RemoveWorker(deviceID); err != nil {
		return err
	}

	// If cluster is running, stop Ray on the worker
	if cluster.IsRunning() && device != nil {
		if err := uc.rayManager.StopRay(ctx, device); err != nil {
			// Log but don't fail
			fmt.Printf("Warning: failed to stop Ray on worker: %v\n", err)
		}
	}

	return uc.repos.Clusters.Update(ctx, cluster)
}

// ChangeHead changes the head node of the cluster
func (uc *ClusterUseCase) ChangeHead(ctx context.Context, clusterName string, newHeadID string, devices map[string]*domain.Device) error {
	cluster, err := uc.repos.Clusters.GetByName(ctx, clusterName)
	if err != nil {
		return domain.ErrClusterNotFound
	}

	wasRunning := cluster.IsRunning()

	// Stop cluster if running
	if wasRunning {
		if err := uc.StopCluster(ctx, clusterName, devices, true); err != nil {
			return fmt.Errorf("failed to stop cluster: %w", err)
		}
	}

	// Change head in configuration
	if err := cluster.ChangeHead(newHeadID); err != nil {
		return err
	}

	if err := uc.repos.Clusters.Update(ctx, cluster); err != nil {
		return err
	}

	// Restart cluster if it was running
	if wasRunning {
		if err := uc.StartCluster(ctx, clusterName, devices); err != nil {
			return fmt.Errorf("failed to restart cluster with new head: %w", err)
		}
	}

	return nil
}

// GetClusterStatus gets the current status of a cluster
func (uc *ClusterUseCase) GetClusterStatus(ctx context.Context, name string, headDevice *domain.Device) (*domain.RayClusterInfo, error) {
	cluster, err := uc.repos.Clusters.GetByName(ctx, name)
	if err != nil {
		return nil, domain.ErrClusterNotFound
	}

	if !cluster.IsRunning() {
		return nil, fmt.Errorf("cluster is not running")
	}

	if headDevice == nil {
		return nil, fmt.Errorf("head device not available")
	}

	return uc.rayManager.GetClusterInfo(ctx, headDevice)
}
