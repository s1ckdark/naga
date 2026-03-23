package usecase

import (
	"context"
	"testing"

	"github.com/dave/naga/internal/domain"
)

type mockFailoverRayManager struct {
	started map[string]bool
	stopped map[string]bool
}

func (m *mockFailoverRayManager) StartHead(ctx context.Context, device *domain.Device, port, dashboardPort int) error {
	m.started[device.ID] = true
	return nil
}

func (m *mockFailoverRayManager) StartWorker(ctx context.Context, device *domain.Device, headAddress string) error {
	m.started[device.ID] = true
	return nil
}

func (m *mockFailoverRayManager) StopRay(ctx context.Context, device *domain.Device) error {
	m.stopped[device.ID] = true
	return nil
}

func (m *mockFailoverRayManager) GetClusterInfo(ctx context.Context, headDevice *domain.Device) (*domain.RayClusterInfo, error) {
	return nil, nil
}

func (m *mockFailoverRayManager) CheckRayInstalled(ctx context.Context, device *domain.Device) (bool, string, error) {
	return true, "2.9.0", nil
}

func (m *mockFailoverRayManager) InstallRay(ctx context.Context, device *domain.Device, version string) error {
	return nil
}

func (m *mockFailoverRayManager) HasRunningJobs(ctx context.Context, headDevice *domain.Device) (bool, error) {
	return false, nil
}

func (m *mockFailoverRayManager) SaveCheckpoint(ctx context.Context, headDevice *domain.Device, checkpointDir string) error {
	return nil
}

func (m *mockFailoverRayManager) RestoreCheckpoint(ctx context.Context, headDevice *domain.Device, checkpointDir string) error {
	return nil
}

func TestFailoverUseCase_ExecuteFailover(t *testing.T) {
	ray := &mockFailoverRayManager{started: make(map[string]bool), stopped: make(map[string]bool)}
	uc := NewFailoverUseCase(ray)

	cluster := &domain.Cluster{
		ID:            "c1",
		Name:          "test-cluster",
		HeadNodeID:    "old-head",
		WorkerIDs:     []string{"worker-1", "worker-2"},
		RayPort:       6379,
		DashboardPort: 8265,
		Status:        domain.ClusterStatusRunning,
	}

	devices := map[string]*domain.Device{
		"old-head": {ID: "old-head", TailscaleIP: "100.64.0.1", Status: domain.DeviceStatusOffline},
		"worker-1": {ID: "worker-1", TailscaleIP: "100.64.0.2", Status: domain.DeviceStatusOnline, SSHEnabled: true},
		"worker-2": {ID: "worker-2", TailscaleIP: "100.64.0.3", Status: domain.DeviceStatusOnline, SSHEnabled: true},
	}

	result := &domain.ElectionResult{NewHeadID: "worker-1", Reason: "lowest GPU", AIDecision: false}

	err := uc.ExecuteFailover(context.Background(), cluster, result, devices, "/tmp/checkpoints")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ray.started["worker-1"] {
		t.Error("expected worker-1 started as head")
	}
	if !ray.started["worker-2"] {
		t.Error("expected worker-2 reconnected")
	}
	if cluster.HeadNodeID != "worker-1" {
		t.Errorf("expected head worker-1, got %s", cluster.HeadNodeID)
	}
	if cluster.Status != domain.ClusterStatusRunning {
		t.Error("expected running status")
	}
}
