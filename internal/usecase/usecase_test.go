package usecase

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/s1ckdark/hydra/internal/domain"
	"github.com/s1ckdark/hydra/internal/repository"
	"github.com/s1ckdark/hydra/internal/repository/sqlite"
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

func (m *mockRayManager) GetOrchInfo(ctx context.Context, headDevice *domain.Device) (*domain.RayOrchInfo, error) {
	return &domain.RayOrchInfo{
		RayVersion: "2.9.0",
		TotalCPUs:  12,
		Nodes:      []domain.RayNodeInfo{{NodeID: "n1", IsCoordinator: true}},
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

func TestDeviceUseCase_SetCapabilities_AppliedOnGetDevice(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	uc := NewDeviceUseCase(repos, ts, &mockCollector{})
	ctx := context.Background()

	uc.SetCapabilities("d1", []string{"gpu", "compute"})

	device, err := uc.GetDevice(ctx, "d1")
	if err != nil {
		t.Fatalf("GetDevice error: %v", err)
	}
	if got := device.Capabilities; len(got) != 2 || got[0] != "gpu" || got[1] != "compute" {
		t.Errorf("Capabilities = %v, want [gpu compute]", got)
	}

	// Untouched device has no override.
	other, _ := uc.GetDevice(ctx, "d2")
	if len(other.Capabilities) != 0 {
		t.Errorf("d2 Capabilities = %v, want empty", other.Capabilities)
	}
}

func TestDeviceUseCase_SetCapabilities_SurvivesCacheRefresh(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	uc := NewDeviceUseCase(repos, ts, &mockCollector{})
	ctx := context.Background()

	// Prime cache.
	if _, err := uc.ListDevices(ctx, false); err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	uc.SetCapabilities("d1", []string{"gpu"})

	// Force refresh — Tailscale returns devices with no Capabilities.
	devices, err := uc.ListDevices(ctx, true)
	if err != nil {
		t.Fatalf("ListDevices(force): %v", err)
	}
	var d1 *domain.Device
	for _, d := range devices {
		if d.ID == "d1" {
			d1 = d
			break
		}
	}
	if d1 == nil {
		t.Fatal("d1 missing after refresh")
	}
	if len(d1.Capabilities) != 1 || d1.Capabilities[0] != "gpu" {
		t.Errorf("Capabilities = %v after refresh, want [gpu]", d1.Capabilities)
	}
}

func TestDeviceUseCase_SetCapabilities_EmptyClears(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	uc := NewDeviceUseCase(repos, ts, &mockCollector{})
	ctx := context.Background()

	uc.SetCapabilities("d1", []string{"gpu"})
	if got := uc.GetCapabilities("d1"); len(got) != 1 {
		t.Errorf("GetCapabilities = %v, want [gpu]", got)
	}

	uc.SetCapabilities("d1", nil)
	if got := uc.GetCapabilities("d1"); got != nil {
		t.Errorf("GetCapabilities after clear = %v, want nil", got)
	}

	device, _ := uc.GetDevice(ctx, "d1")
	if len(device.Capabilities) != 0 {
		t.Errorf("Capabilities after clear = %v, want empty", device.Capabilities)
	}
}

func TestDeviceUseCase_SetCapabilities_EmptyClearsCachedDevicePointer(t *testing.T) {
	// Regression test: clearing an override must also reset the cached device's
	// Capabilities, not just delete the override map entry. Otherwise the
	// in-place mutation applied during the prior override would persist on the
	// cached *Device pointer until the next Tailscale refresh.
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	uc := NewDeviceUseCase(repos, ts, &mockCollector{})
	ctx := context.Background()

	if _, err := uc.ListDevices(ctx, false); err != nil { // populate cache
		t.Fatalf("ListDevices: %v", err)
	}
	uc.SetCapabilities("d1", []string{"gpu"})
	if _, err := uc.ListDevices(ctx, false); err != nil { // applies override
		t.Fatalf("ListDevices2: %v", err)
	}

	uc.SetCapabilities("d1", nil) // clear

	devices, _ := uc.ListDevices(ctx, false) // cache hit, applyCapabilityOverrides no-ops for d1
	for _, d := range devices {
		if d.ID == "d1" {
			if len(d.Capabilities) != 0 {
				t.Errorf("cached d1.Capabilities = %v after clear; want empty", d.Capabilities)
			}
			return
		}
	}
	t.Errorf("d1 not found in cached devices")
}

func TestDeviceUseCase_GetCapabilities_DefensiveCopy(t *testing.T) {
	repos := setupTestRepos(t)
	ts := &mockTailscale{devices: testDevices()}
	uc := NewDeviceUseCase(repos, ts, &mockCollector{})

	uc.SetCapabilities("d1", []string{"gpu"})
	got := uc.GetCapabilities("d1")
	got[0] = "tampered"

	if again := uc.GetCapabilities("d1"); again[0] != "gpu" {
		t.Errorf("internal state mutated via returned slice: got %v", again)
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

// --- OrchUseCase Tests ---

func TestOrchUseCase_CreateOrch(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewOrchUseCase(repos, ray)
	ctx := context.Background()

	orch, err := uc.CreateOrch(ctx, "my-orch", "d1", []string{"d2", "d3"})
	if err != nil {
		t.Fatalf("CreateOrch error: %v", err)
	}
	if orch.Name != "my-orch" {
		t.Errorf("Name = %q", orch.Name)
	}
	if orch.ID == "" {
		t.Error("ID should be generated")
	}

	// Duplicate name
	_, err = uc.CreateOrch(ctx, "my-orch", "d4", nil)
	if err != domain.ErrOrchAlreadyExist {
		t.Errorf("duplicate name error = %v, want ErrOrchAlreadyExist", err)
	}

	// Head already in orch
	_, err = uc.CreateOrch(ctx, "c2", "d1", nil)
	if err == nil {
		t.Error("head already in orch should fail")
	}

	// Worker already in orch
	_, err = uc.CreateOrch(ctx, "c3", "d5", []string{"d2"})
	if err == nil {
		t.Error("worker already in orch should fail")
	}
}

func TestOrchUseCase_ListOrchs(t *testing.T) {
	repos := setupTestRepos(t)
	uc := NewOrchUseCase(repos, &mockRayManager{})
	ctx := context.Background()

	uc.CreateOrch(ctx, "c1", "d1", nil)
	uc.CreateOrch(ctx, "c2", "d2", nil)

	orchs, err := uc.ListOrchs(ctx)
	if err != nil {
		t.Fatalf("ListOrchs error: %v", err)
	}
	if len(orchs) != 2 {
		t.Errorf("orchs = %d, want 2", len(orchs))
	}
}

func TestOrchUseCase_StartOrch(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewOrchUseCase(repos, ray)
	ctx := context.Background()

	orch, _ := uc.CreateOrch(ctx, "my-orch", "d1", []string{"d2", "d3"})
	devices := testDeviceMap()

	err := uc.StartOrch(ctx, orch.Name, devices)
	if err != nil {
		t.Fatalf("StartOrch error: %v", err)
	}
	if !ray.startHeadCalled {
		t.Error("StartHead should be called")
	}
	if ray.startWorkerCalled != 2 {
		t.Errorf("StartWorker called %d times, want 2", ray.startWorkerCalled)
	}

	// Verify status updated
	got, _ := uc.GetOrch(ctx, "my-orch")
	if got.Status != domain.OrchStatusRunning {
		t.Errorf("Status = %q, want running", got.Status)
	}
	if got.DashboardURL == "" {
		t.Error("DashboardURL should be set")
	}

	// Starting already running orch should fail
	err = uc.StartOrch(ctx, "my-orch", devices)
	if err == nil {
		t.Error("starting running orch should fail")
	}
}

func TestOrchUseCase_StopOrch(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewOrchUseCase(repos, ray)
	ctx := context.Background()

	orch, _ := uc.CreateOrch(ctx, "my-orch", "d1", []string{"d2"})
	devices := testDeviceMap()
	uc.StartOrch(ctx, orch.Name, devices)

	// Reset counters
	ray.stopRayCalled = 0

	err := uc.StopOrch(ctx, "my-orch", devices, false)
	if err != nil {
		t.Fatalf("StopOrch error: %v", err)
	}
	// Should stop worker + head = 2
	if ray.stopRayCalled != 2 {
		t.Errorf("StopRay called %d times, want 2", ray.stopRayCalled)
	}

	got, _ := uc.GetOrch(ctx, "my-orch")
	if got.Status != domain.OrchStatusStopped {
		t.Errorf("Status = %q, want stopped", got.Status)
	}
}

func TestOrchUseCase_StopOrch_WithRunningJobs(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{hasRunningJobs: true}
	uc := NewOrchUseCase(repos, ray)
	ctx := context.Background()

	orch, _ := uc.CreateOrch(ctx, "my-orch", "d1", nil)
	devices := testDeviceMap()
	uc.StartOrch(ctx, orch.Name, devices)

	// Without force should fail
	err := uc.StopOrch(ctx, "my-orch", devices, false)
	if err != domain.ErrOrchInUse {
		t.Errorf("stop with running jobs = %v, want ErrOrchInUse", err)
	}

	// With force should succeed
	err = uc.StopOrch(ctx, "my-orch", devices, true)
	if err != nil {
		t.Fatalf("force stop error: %v", err)
	}
}

func TestOrchUseCase_AddWorker(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewOrchUseCase(repos, ray)
	ctx := context.Background()
	devices := testDeviceMap()

	orch, _ := uc.CreateOrch(ctx, "my-orch", "d1", nil)

	// Add worker to pending orch (no Ray start)
	err := uc.AddWorker(ctx, orch.Name, "d2", devices["d2"], devices["d1"])
	if err != nil {
		t.Fatalf("AddWorker error: %v", err)
	}

	got, _ := uc.GetOrch(ctx, "my-orch")
	if !got.HasWorker("d2") {
		t.Error("d2 should be a worker")
	}

	// Add duplicate
	err = uc.AddWorker(ctx, orch.Name, "d2", devices["d2"], devices["d1"])
	if err != domain.ErrNodeAlreadyInOrch {
		t.Errorf("duplicate add = %v, want ErrNodeAlreadyInOrch", err)
	}
}

func TestOrchUseCase_RemoveWorker(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewOrchUseCase(repos, ray)
	ctx := context.Background()

	orch, _ := uc.CreateOrch(ctx, "my-orch", "d1", []string{"d2", "d3"})

	err := uc.RemoveWorker(ctx, orch.Name, "d2", testDeviceMap()["d2"])
	if err != nil {
		t.Fatalf("RemoveWorker error: %v", err)
	}

	got, _ := uc.GetOrch(ctx, "my-orch")
	if got.HasWorker("d2") {
		t.Error("d2 should be removed")
	}

	// Remove head should fail
	err = uc.RemoveWorker(ctx, orch.Name, "d1", nil)
	if err != domain.ErrCannotRemoveHead {
		t.Errorf("remove head = %v, want ErrCannotRemoveHead", err)
	}
}

func TestOrchUseCase_DeleteOrch(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewOrchUseCase(repos, ray)
	ctx := context.Background()

	uc.CreateOrch(ctx, "my-orch", "d1", nil)

	err := uc.DeleteOrch(ctx, "my-orch", testDeviceMap(), false)
	if err != nil {
		t.Fatalf("DeleteOrch error: %v", err)
	}

	_, err = uc.GetOrch(ctx, "my-orch")
	if err != domain.ErrOrchNotFound {
		t.Errorf("after delete = %v, want ErrOrchNotFound", err)
	}
}

func TestOrchUseCase_GetOrchStatus(t *testing.T) {
	repos := setupTestRepos(t)
	ray := &mockRayManager{}
	uc := NewOrchUseCase(repos, ray)
	ctx := context.Background()

	orch, _ := uc.CreateOrch(ctx, "my-orch", "d1", nil)
	devices := testDeviceMap()
	uc.StartOrch(ctx, orch.Name, devices)

	info, err := uc.GetOrchStatus(ctx, "my-orch", devices["d1"])
	if err != nil {
		t.Fatalf("GetOrchStatus error: %v", err)
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
