package usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/dave/naga/internal/domain"
)

// FailoverRayManager extends RayManager with checkpoint operations
type FailoverRayManager interface {
	RayManager
	SaveCheckpoint(ctx context.Context, headDevice *domain.Device, checkpointDir string) error
	RestoreCheckpoint(ctx context.Context, headDevice *domain.Device, checkpointDir string) error
}

// FailoverUseCase handles cluster head failover
type FailoverUseCase struct {
	rayManager FailoverRayManager
}

// NewFailoverUseCase creates a new FailoverUseCase
func NewFailoverUseCase(rayManager FailoverRayManager) *FailoverUseCase {
	return &FailoverUseCase{rayManager: rayManager}
}

// ExecuteFailover performs head failover:
// 1. Save checkpoint from old head (best-effort)
// 2. Stop Ray on old head (best-effort)
// 3. Update cluster config (ChangeHead)
// 4. Start Ray head on new node
// 5. Reconnect workers
// 6. Restore checkpoint (best-effort)
func (uc *FailoverUseCase) ExecuteFailover(
	ctx context.Context,
	cluster *domain.Cluster,
	election *domain.ElectionResult,
	devices map[string]*domain.Device,
	checkpointDir string,
) error {
	newHeadID := election.NewHeadID
	newHeadDevice := devices[newHeadID]
	if newHeadDevice == nil {
		return fmt.Errorf("new head device %s not found", newHeadID)
	}

	oldHeadDevice := devices[cluster.HeadNodeID]

	// Step 1: Save checkpoint (best-effort, uses Background to avoid caller cancellation)
	if oldHeadDevice != nil && oldHeadDevice.IsOnline() && checkpointDir != "" {
		saveCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := uc.rayManager.SaveCheckpoint(saveCtx, oldHeadDevice, checkpointDir); err != nil {
			log.Printf("Warning: failed to save checkpoint: %v", err)
		}
	}

	// Step 2: Stop old head (best-effort, uses Background to avoid caller cancellation)
	if oldHeadDevice != nil && oldHeadDevice.IsOnline() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := uc.rayManager.StopRay(stopCtx, oldHeadDevice); err != nil {
			log.Printf("Warning: failed to stop old head: %v", err)
		}
	}

	// Step 3: Update cluster config
	if err := cluster.ChangeHead(newHeadID); err != nil {
		return fmt.Errorf("failed to change head to %s: %w", newHeadID, err)
	}
	cluster.Status = domain.ClusterStatusStarting
	now := time.Now()
	cluster.StartedAt = &now

	// Step 4: Start new head
	if err := uc.rayManager.StartHead(ctx, newHeadDevice, cluster.RayPort, cluster.DashboardPort); err != nil {
		cluster.SetError(fmt.Sprintf("failover: failed to start new head: %v", err))
		return fmt.Errorf("failed to start new head on %s: %w", newHeadID, err)
	}

	// Step 5: Reconnect workers
	headAddress := fmt.Sprintf("%s:%d", newHeadDevice.TailscaleIP, cluster.RayPort)
	for _, workerID := range cluster.WorkerIDs {
		workerDevice := devices[workerID]
		if workerDevice == nil || !workerDevice.CanSSH() {
			continue
		}
		if err := uc.rayManager.StartWorker(ctx, workerDevice, headAddress); err != nil {
			log.Printf("Warning: failed to reconnect worker %s: %v", workerID, err)
		}
	}

	// Step 6: Restore checkpoint (best-effort, uses Background to avoid caller cancellation)
	if checkpointDir != "" {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := uc.rayManager.RestoreCheckpoint(restoreCtx, newHeadDevice, checkpointDir); err != nil {
			log.Printf("Warning: failed to restore checkpoint: %v", err)
		}
	}

	cluster.Status = domain.ClusterStatusRunning
	cluster.DashboardURL = fmt.Sprintf("http://%s:%d", newHeadDevice.TailscaleIP, cluster.DashboardPort)
	cluster.UpdatedAt = time.Now()

	return nil
}
