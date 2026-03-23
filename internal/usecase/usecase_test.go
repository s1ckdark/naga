package usecase

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dave/naga/internal/domain"
	"github.com/dave/naga/internal/repository"
	"github.com/dave/naga/internal/repository/sqlite"
)

// --- Mock implementations ---

type mockTailscale struct {
	devices []*domain.Device
}

func (m *mockTailscale) ListDevices(ctx context.Context) ([]*domain.Device, error) {
	return m.devices, nil
}

func (m *mockTailscale) GetDevice(ctx context.Context, nameOrID string) (*domain.Device, error) {
	for _, d := range m.devices {
		if d.ID == nameOrID || d.Name == nameOrID {
			return d, nil
		}
	}
	return nil, fmt.Errorf("device not found: %s", nameOrID)
}

func (m *mockTailscale) GetDeviceByID(ctx context.Context, id string) (*domain.Device, error) {
	for _, d := range m.devices {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, fmt.Errorf("device not found: %s", id)
}

type mockCollector struct{}

func (m *mockCollector) CollectMetrics(ctx context.Context, device *domain.Device) (*domain.DeviceMetrics, error) {
	return &domain.DeviceMetrics{
		DeviceID:    device.ID,
		CPU:         domain.CPUMetrics{UsagePercent: 25.0, Cores: 4},
		Memory:      domain.MemoryMetrics{Total: 16 * 1024 * 1024 * 1024, UsagePercent: 50.0},
		CollectedAt: time.Now(),
	}, nil
}

func (m *mockCollector) CollectMetricsParallel(ctx context.Context, devices []*domain.Device) ([]*domain.DeviceMetrics, error) {
	var result []*domain.DeviceMetrics
	for _, d := range devices {
		metrics, _ := m.CollectMetrics(ctx, d)
		result = append(result, metrics)
	}
	return result, nil
}

type mockRayManager struct {
	startHeadCalled   bool
	startWorkerCalled int
	stopRayCalled     int
	hasRunningJobs    bool
}

func (m *mockRayManager) StartHead(ctx context.Context, device *domain.Device, port, dashboardPort int) error {
	m.startHeadCalled = true
	return nil
}

func (m *mockRayManager) StartWorker(ctx context.Context, device *domain.Device, headAddress string) error {
	m.startWorkerCalled++
	return nil
}

func (m *mockRayManager) StopRay(ctx context.Context, device *domain.Device) error {
	m.stopRayCalled++
	return nil
}

func (m *mockRayManager) GetClusterInfo(ctx context.Context, headDevice *domain.Device) (*domain.RayClusterInfo, error) {
	return &domain.RayClusterInfo{
		RayVersion: "2.9.0",
		TotalCPUs:  12,
		Nodes:      []domain.RayNodeInfo{{NodeID: "n1", IsHeadNode: true}},
	}, nil
}

func (m *mockRayManager) CheckRayInstalled(ctx context.Context, device *domain.Device) (bool, string, error) {
	return true, "2.9.0", nil
}

func (m *mockRayManager) InstallRay(ctx context.Context, device *domain.Device, version string) error {
	return nil
}

func (m *mockRayManager) HasRunningJobs(ctx context.Context, headDevice *domain.Device) (bool, error) {
	return m.hasRunningJobs, nil
}

// --- Test helpers ---

func setupTestRepos(t *testing.T) *repository.Repositories {
	t.Helper()
	db, err := sqlite.NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB error: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db.Repositories()
}

func testDevices() []*domain.Device {
	return []*domain.Device{
		{ID: "d1", Name: "server1", TailscaleIP: "100.64.0.1", Status: domain.DeviceStatusOnline, SSHEnabled: true, OS: "linux"},
		{ID: "d2", Name: "server2", TailscaleIP: "100.64.0.2", Status: domain.DeviceStatusOnline, SSHEnabled: true, OS: "linux"},
		{ID: "d3", Name: "server3", TailscaleIP: "100.64.0.3", Status: domain.DeviceStatusOnline, SSHEnabled: true, OS: "linux"},
		{ID: "d4", Name: "laptop", TailscaleIP: "100.64.0.4", Status: domain.DeviceStatusOffline, SSHEnabled: false, OS: "macos"},
	}
}

func testDeviceMap() map[string]*domain.Device {
	m := make(map[string]*domain.Device)
	for _, d := range testDevices() {
		m[d.ID] = d
	}
	return m
}

// --- DeviceUseCase Tests ---

func TestDeviceUseCase_ListDevices(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	uc := NewDeviceUseCase(repos, ts, &mockCollector{})
	ctx := context.Background()

	devices, err := uc.ListDevices(ctx, false)
	if err != nil {
		t.Fatalf("ListDevices error: %v", err)
	}
	if len(devices) != 4 {
		t.Errorf("devices = %d, want 4", len(devices))
	}
}

func TestDeviceUseCase_ListDevices_Cache(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	uc := NewDeviceUseCase(repos, ts, &mockCollector{})
	ctx := context.Background()

	// First call populates cache
	uc.ListDevices(ctx, false)

	// Modify mock data
	ts.devices = []*domain.Device{{ID: "only-one"}}

	// Should still return cached data
	devices, _ := uc.ListDevices(ctx, false)
	if len(devices) != 4 {
		t.Errorf("cached devices = %d, want 4", len(devices))
	}

	// Force refresh should get new data
	devices, _ = uc.ListDevices(ctx, true)
	if len(devices) != 1 {
		t.Errorf("refreshed devices = %d, want 1", len(devices))
	}
}

func TestDeviceUseCase_GetDevice(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	uc := NewDeviceUseCase(repos, ts, &mockCollector{})
	ctx := context.Background()

	device, err := uc.GetDevice(ctx, "server1")
	if err != nil {
		t.Fatalf("GetDevice error: %v", err)
	}
	if device.ID != "d1" {
		t.Errorf("device ID = %q, want %q", device.ID, "d1")
	}

	_, err = uc.GetDevice(ctx, "nonexistent")
	if err == nil {
		t.Error("GetDevice(nonexistent) should fail")
	}
}

func TestDeviceUseCase_FilterDevices(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	uc := NewDeviceUseCase(repos, ts, &mockCollector{})
	ctx := context.Background()

	online := domain.DeviceStatusOnline
	devices, _ := uc.FilterDevices(ctx, domain.DeviceFilter{Status: &online})
	if len(devices) != 3 {
		t.Errorf("online devices = %d, want 3", len(devices))
	}

	devices, _ = uc.FilterDevices(ctx, domain.DeviceFilter{OS: "macos"})
	if len(devices) != 1 {
		t.Errorf("macos devices = %d, want 1", len(devices))
	}
}

func TestDeviceUseCase_GetDeviceMap(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	uc := NewDeviceUseCase(repos, ts, &mockCollector{})
	ctx := context.Background()

	dm, err := uc.GetDeviceMap(ctx)
	if err != nil {
		t.Fatalf("GetDeviceMap error: %v", err)
	}
	if len(dm) != 4 {
		t.Errorf("device map size = %d, want 4", len(dm))
	}
	if dm["d1"].Name != "server1" {
		t.Errorf("dm[d1].Name = %q", dm["d1"].Name)
	}
}

// --- ClusterUseCase Tests ---

func TestClusterUseCase_CreateCluster(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewClusterUseCase(repos, ray)
	ctx := context.Background()

	cluster, err := uc.CreateCluster(ctx, "my-cluster", "d1", []string{"d2", "d3"})
	if err != nil {
		t.Fatalf("CreateCluster error: %v", err)
	}
	if cluster.Name != "my-cluster" {
		t.Errorf("Name = %q", cluster.Name)
	}
	if cluster.ID == "" {
		t.Error("ID should be generated")
	}

	// Duplicate name
	_, err = uc.CreateCluster(ctx, "my-cluster", "d4", nil)
	if err != domain.ErrClusterAlreadyExist {
		t.Errorf("duplicate name error = %v, want ErrClusterAlreadyExist", err)
	}

	// Head already in cluster
	_, err = uc.CreateCluster(ctx, "c2", "d1", nil)
	if err == nil {
		t.Error("head already in cluster should fail")
	}

	// Worker already in cluster
	_, err = uc.CreateCluster(ctx, "c3", "d5", []string{"d2"})
	if err == nil {
		t.Error("worker already in cluster should fail")
	}
}

func TestClusterUseCase_ListClusters(t *testing.T) {
	repos := setupTestRepos(t)
	uc := NewClusterUseCase(repos, &mockRayManager{})
	ctx := context.Background()

	uc.CreateCluster(ctx, "c1", "d1", nil)
	uc.CreateCluster(ctx, "c2", "d2", nil)

	clusters, err := uc.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters error: %v", err)
	}
	if len(clusters) != 2 {
		t.Errorf("clusters = %d, want 2", len(clusters))
	}
}

func TestClusterUseCase_StartCluster(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewClusterUseCase(repos, ray)
	ctx := context.Background()

	cluster, _ := uc.CreateCluster(ctx, "my-cluster", "d1", []string{"d2", "d3"})
	devices := testDeviceMap()

	err := uc.StartCluster(ctx, cluster.Name, devices)
	if err != nil {
		t.Fatalf("StartCluster error: %v", err)
	}
	if !ray.startHeadCalled {
		t.Error("StartHead should be called")
	}
	if ray.startWorkerCalled != 2 {
		t.Errorf("StartWorker called %d times, want 2", ray.startWorkerCalled)
	}

	// Verify status updated
	got, _ := uc.GetCluster(ctx, "my-cluster")
	if got.Status != domain.ClusterStatusRunning {
		t.Errorf("Status = %q, want running", got.Status)
	}
	if got.DashboardURL == "" {
		t.Error("DashboardURL should be set")
	}

	// Starting already running cluster should fail
	err = uc.StartCluster(ctx, "my-cluster", devices)
	if err == nil {
		t.Error("starting running cluster should fail")
	}
}

func TestClusterUseCase_StopCluster(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewClusterUseCase(repos, ray)
	ctx := context.Background()

	cluster, _ := uc.CreateCluster(ctx, "my-cluster", "d1", []string{"d2"})
	devices := testDeviceMap()
	uc.StartCluster(ctx, cluster.Name, devices)

	// Reset counters
	ray.stopRayCalled = 0

	err := uc.StopCluster(ctx, "my-cluster", devices, false)
	if err != nil {
		t.Fatalf("StopCluster error: %v", err)
	}
	// Should stop worker + head = 2
	if ray.stopRayCalled != 2 {
		t.Errorf("StopRay called %d times, want 2", ray.stopRayCalled)
	}

	got, _ := uc.GetCluster(ctx, "my-cluster")
	if got.Status != domain.ClusterStatusStopped {
		t.Errorf("Status = %q, want stopped", got.Status)
	}
}

func TestClusterUseCase_StopCluster_WithRunningJobs(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{hasRunningJobs: true}
	uc := NewClusterUseCase(repos, ray)
	ctx := context.Background()

	cluster, _ := uc.CreateCluster(ctx, "my-cluster", "d1", nil)
	devices := testDeviceMap()
	uc.StartCluster(ctx, cluster.Name, devices)

	// Without force should fail
	err := uc.StopCluster(ctx, "my-cluster", devices, false)
	if err != domain.ErrClusterInUse {
		t.Errorf("stop with running jobs = %v, want ErrClusterInUse", err)
	}

	// With force should succeed
	err = uc.StopCluster(ctx, "my-cluster", devices, true)
	if err != nil {
		t.Fatalf("force stop error: %v", err)
	}
}

func TestClusterUseCase_AddWorker(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewClusterUseCase(repos, ray)
	ctx := context.Background()
	devices := testDeviceMap()

	cluster, _ := uc.CreateCluster(ctx, "my-cluster", "d1", nil)

	// Add worker to pending cluster (no Ray start)
	err := uc.AddWorker(ctx, cluster.Name, "d2", devices["d2"], devices["d1"])
	if err != nil {
		t.Fatalf("AddWorker error: %v", err)
	}

	got, _ := uc.GetCluster(ctx, "my-cluster")
	if !got.HasWorker("d2") {
		t.Error("d2 should be a worker")
	}

	// Add duplicate
	err = uc.AddWorker(ctx, cluster.Name, "d2", devices["d2"], devices["d1"])
	if err != domain.ErrNodeAlreadyInCluster {
		t.Errorf("duplicate add = %v, want ErrNodeAlreadyInCluster", err)
	}
}

func TestClusterUseCase_RemoveWorker(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewClusterUseCase(repos, ray)
	ctx := context.Background()

	cluster, _ := uc.CreateCluster(ctx, "my-cluster", "d1", []string{"d2", "d3"})

	err := uc.RemoveWorker(ctx, cluster.Name, "d2", testDeviceMap()["d2"])
	if err != nil {
		t.Fatalf("RemoveWorker error: %v", err)
	}

	got, _ := uc.GetCluster(ctx, "my-cluster")
	if got.HasWorker("d2") {
		t.Error("d2 should be removed")
	}

	// Remove head should fail
	err = uc.RemoveWorker(ctx, cluster.Name, "d1", nil)
	if err != domain.ErrCannotRemoveHead {
		t.Errorf("remove head = %v, want ErrCannotRemoveHead", err)
	}
}

func TestClusterUseCase_DeleteCluster(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewClusterUseCase(repos, ray)
	ctx := context.Background()

	uc.CreateCluster(ctx, "my-cluster", "d1", nil)

	err := uc.DeleteCluster(ctx, "my-cluster", testDeviceMap(), false)
	if err != nil {
		t.Fatalf("DeleteCluster error: %v", err)
	}

	_, err = uc.GetCluster(ctx, "my-cluster")
	if err != domain.ErrClusterNotFound {
		t.Errorf("after delete = %v, want ErrClusterNotFound", err)
	}
}

func TestClusterUseCase_GetClusterStatus(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewClusterUseCase(repos, ray)
	ctx := context.Background()

	cluster, _ := uc.CreateCluster(ctx, "my-cluster", "d1", nil)
	devices := testDeviceMap()
	uc.StartCluster(ctx, cluster.Name, devices)

	info, err := uc.GetClusterStatus(ctx, "my-cluster", devices["d1"])
	if err != nil {
		t.Fatalf("GetClusterStatus error: %v", err)
	}
	if info.RayVersion != "2.9.0" {
		t.Errorf("RayVersion = %q", info.RayVersion)
	}
}

// --- MonitorUseCase Tests ---

func TestMonitorUseCase_GetDeviceMetrics(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	deviceUC := NewDeviceUseCase(repos, ts, &mockCollector{})
	collector := &mockCollector{}
	uc := NewMonitorUseCase(repos, collector, deviceUC)
	ctx := context.Background()

	metrics, err := uc.GetDeviceMetrics(ctx, "server1")
	if err != nil {
		t.Fatalf("GetDeviceMetrics error: %v", err)
	}
	if metrics.DeviceID != "d1" {
		t.Errorf("DeviceID = %q, want d1", metrics.DeviceID)
	}
	if metrics.CPU.Cores != 4 {
		t.Errorf("CPU.Cores = %d, want 4", metrics.CPU.Cores)
	}
}

func TestMonitorUseCase_GetDeviceMetrics_Offline(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	deviceUC := NewDeviceUseCase(repos, ts, &mockCollector{})
	uc := NewMonitorUseCase(repos, &mockCollector{}, deviceUC)
	ctx := context.Background()

	metrics, err := uc.GetDeviceMetrics(ctx, "laptop")
	if err != nil {
		t.Fatalf("GetDeviceMetrics error: %v", err)
	}
	if metrics.Error == "" {
		t.Error("offline device should have error message")
	}
}

func TestMonitorUseCase_GetAllMetrics(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	deviceUC := NewDeviceUseCase(repos, ts, &mockCollector{})
	uc := NewMonitorUseCase(repos, &mockCollector{}, deviceUC)
	ctx := context.Background()

	snapshot, err := uc.GetAllMetrics(ctx)
	if err != nil {
		t.Fatalf("GetAllMetrics error: %v", err)
	}
	// 3 online SSH-enabled devices
	if len(snapshot.Devices) != 3 {
		t.Errorf("snapshot devices = %d, want 3", len(snapshot.Devices))
	}
}

func TestMonitorUseCase_SaveAndGetHistory(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	deviceUC := NewDeviceUseCase(repos, ts, &mockCollector{})
	uc := NewMonitorUseCase(repos, &mockCollector{}, deviceUC)
	ctx := context.Background()

	// Save metrics
	for i := 0; i < 3; i++ {
		uc.SaveMetrics(ctx, &domain.DeviceMetrics{
			DeviceID:    "d1",
			CPU:         domain.CPUMetrics{UsagePercent: float64(i * 20)},
			CollectedAt: time.Now().Add(time.Duration(i) * time.Minute),
		})
	}

	history, err := uc.GetMetricsHistory(ctx, "d1", 10)
	if err != nil {
		t.Fatalf("GetMetricsHistory error: %v", err)
	}
	if len(history.Points) != 3 {
		t.Errorf("history points = %d, want 3", len(history.Points))
	}
}
