package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

// newTestDB creates an in-memory SQLite DB for testing
func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB(:memory:) error: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// --- Device Repository Tests ---

func TestDeviceRepository_SaveAndGetByID(t *testing.T) {
	db := newTestDB(t)
	repo := NewDeviceRepository(db.db)
	ctx := context.Background()

	device := &domain.Device{
		ID:          "dev-1",
		Name:        "test-server",
		Hostname:    "server1",
		IPAddresses: []string{"100.64.0.1", "192.168.1.10"},
		TailscaleIP: "100.64.0.1",
		OS:          "linux",
		Status:      domain.DeviceStatusOnline,
		Tags:        []string{"tag:server", "tag:gpu"},
		User:        "dave",
		SSHEnabled:  true,
		RayInstalled: true,
		RayVersion:  "2.9.0",
		LastSeen:    time.Now(),
		CreatedAt:   time.Now(),
	}

	if err := repo.Save(ctx, device); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := repo.GetByID(ctx, "dev-1")
	if err != nil {
		t.Fatalf("GetByID error: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil")
	}
	if got.Name != "test-server" {
		t.Errorf("Name = %q, want %q", got.Name, "test-server")
	}
	if got.OS != "linux" {
		t.Errorf("OS = %q, want %q", got.OS, "linux")
	}
	if len(got.IPAddresses) != 2 {
		t.Errorf("IPAddresses length = %d, want 2", len(got.IPAddresses))
	}
	if len(got.Tags) != 2 {
		t.Errorf("Tags length = %d, want 2", len(got.Tags))
	}
	if got.RayVersion != "2.9.0" {
		t.Errorf("RayVersion = %q, want %q", got.RayVersion, "2.9.0")
	}
}

func TestDeviceRepository_GetByID_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewDeviceRepository(db.db)

	got, err := repo.GetByID(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetByID error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent device")
	}
}

func TestDeviceRepository_Upsert(t *testing.T) {
	db := newTestDB(t)
	repo := NewDeviceRepository(db.db)
	ctx := context.Background()

	device := &domain.Device{
		ID:     "dev-1",
		Name:   "original",
		Status: domain.DeviceStatusOnline,
	}
	repo.Save(ctx, device)

	device.Name = "updated"
	repo.Save(ctx, device)

	got, _ := repo.GetByID(ctx, "dev-1")
	if got.Name != "updated" {
		t.Errorf("Name = %q, want %q after upsert", got.Name, "updated")
	}
}

func TestDeviceRepository_GetAll(t *testing.T) {
	db := newTestDB(t)
	repo := NewDeviceRepository(db.db)
	ctx := context.Background()

	for i, name := range []string{"charlie", "alpha", "bravo"} {
		repo.Save(ctx, &domain.Device{ID: string(rune('a' + i)), Name: name})
	}

	devices, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll error: %v", err)
	}
	if len(devices) != 3 {
		t.Fatalf("GetAll length = %d, want 3", len(devices))
	}
	// Should be ordered by name
	if devices[0].Name != "alpha" {
		t.Errorf("first device = %q, want %q (ordered by name)", devices[0].Name, "alpha")
	}
}

func TestDeviceRepository_GetByFilter(t *testing.T) {
	db := newTestDB(t)
	repo := NewDeviceRepository(db.db)
	ctx := context.Background()

	repo.Save(ctx, &domain.Device{ID: "1", Name: "a", OS: "linux", Status: domain.DeviceStatusOnline, SSHEnabled: true})
	repo.Save(ctx, &domain.Device{ID: "2", Name: "b", OS: "macos", Status: domain.DeviceStatusOnline, SSHEnabled: false})
	repo.Save(ctx, &domain.Device{ID: "3", Name: "c", OS: "linux", Status: domain.DeviceStatusOffline, SSHEnabled: true})

	// Filter by OS
	devices, _ := repo.GetByFilter(ctx, domain.DeviceFilter{OS: "linux"})
	if len(devices) != 2 {
		t.Errorf("linux filter: got %d, want 2", len(devices))
	}

	// Filter by status
	online := domain.DeviceStatusOnline
	devices, _ = repo.GetByFilter(ctx, domain.DeviceFilter{Status: &online})
	if len(devices) != 2 {
		t.Errorf("online filter: got %d, want 2", len(devices))
	}

	// Filter by SSH
	sshTrue := true
	devices, _ = repo.GetByFilter(ctx, domain.DeviceFilter{SSHEnabled: &sshTrue})
	if len(devices) != 2 {
		t.Errorf("ssh filter: got %d, want 2", len(devices))
	}
}

func TestDeviceRepository_Delete(t *testing.T) {
	db := newTestDB(t)
	repo := NewDeviceRepository(db.db)
	ctx := context.Background()

	repo.Save(ctx, &domain.Device{ID: "dev-1", Name: "test"})
	repo.Delete(ctx, "dev-1")

	got, _ := repo.GetByID(ctx, "dev-1")
	if got != nil {
		t.Error("device should be deleted")
	}
}

func TestDeviceRepository_SaveMany(t *testing.T) {
	db := newTestDB(t)
	repo := NewDeviceRepository(db.db)
	ctx := context.Background()

	devices := []*domain.Device{
		{ID: "1", Name: "a"},
		{ID: "2", Name: "b"},
	}

	if err := repo.SaveMany(ctx, devices); err != nil {
		t.Fatalf("SaveMany error: %v", err)
	}

	all, _ := repo.GetAll(ctx)
	if len(all) != 2 {
		t.Errorf("GetAll length = %d, want 2", len(all))
	}
}

// --- Cluster Repository Tests ---

func TestClusterRepository_CreateAndGetByID(t *testing.T) {
	db := newTestDB(t)
	repo := NewClusterRepository(db.db)
	ctx := context.Background()

	cluster := domain.NewCluster("my-cluster", "head1", []string{"w1", "w2"})
	cluster.Description = "test cluster"

	if err := repo.Create(ctx, cluster); err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if cluster.ID == "" {
		t.Error("ID should be auto-generated")
	}

	got, err := repo.GetByID(ctx, cluster.ID)
	if err != nil {
		t.Fatalf("GetByID error: %v", err)
	}
	if got.Name != "my-cluster" {
		t.Errorf("Name = %q, want %q", got.Name, "my-cluster")
	}
	if got.HeadNodeID != "head1" {
		t.Errorf("HeadNodeID = %q, want %q", got.HeadNodeID, "head1")
	}
	if len(got.WorkerIDs) != 2 {
		t.Errorf("WorkerIDs length = %d, want 2", len(got.WorkerIDs))
	}
	if got.Description != "test cluster" {
		t.Errorf("Description = %q, want %q", got.Description, "test cluster")
	}
}

func TestClusterRepository_GetByName(t *testing.T) {
	db := newTestDB(t)
	repo := NewClusterRepository(db.db)
	ctx := context.Background()

	cluster := domain.NewCluster("unique-name", "h1", nil)
	repo.Create(ctx, cluster)

	got, err := repo.GetByName(ctx, "unique-name")
	if err != nil {
		t.Fatalf("GetByName error: %v", err)
	}
	if got.Name != "unique-name" {
		t.Errorf("Name = %q, want %q", got.Name, "unique-name")
	}

	_, err = repo.GetByName(ctx, "nonexistent")
	if err != domain.ErrClusterNotFound {
		t.Errorf("GetByName(nonexistent) = %v, want ErrClusterNotFound", err)
	}
}

func TestClusterRepository_Update(t *testing.T) {
	db := newTestDB(t)
	repo := NewClusterRepository(db.db)
	ctx := context.Background()

	cluster := domain.NewCluster("c1", "h1", nil)
	repo.Create(ctx, cluster)

	cluster.Status = domain.ClusterStatusRunning
	cluster.DashboardURL = "http://localhost:8265"
	if err := repo.Update(ctx, cluster); err != nil {
		t.Fatalf("Update error: %v", err)
	}

	got, _ := repo.GetByID(ctx, cluster.ID)
	if got.Status != domain.ClusterStatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, domain.ClusterStatusRunning)
	}
	if got.DashboardURL != "http://localhost:8265" {
		t.Errorf("DashboardURL = %q", got.DashboardURL)
	}

	// Update non-existent
	fake := &domain.Cluster{ID: "fake"}
	if err := repo.Update(ctx, fake); err != domain.ErrClusterNotFound {
		t.Errorf("Update(fake) = %v, want ErrClusterNotFound", err)
	}
}

func TestClusterRepository_GetByStatus(t *testing.T) {
	db := newTestDB(t)
	repo := NewClusterRepository(db.db)
	ctx := context.Background()

	c1 := domain.NewCluster("c1", "h1", nil)
	c2 := domain.NewCluster("c2", "h2", nil)
	c2.Status = domain.ClusterStatusRunning
	repo.Create(ctx, c1)
	repo.Create(ctx, c2)

	pending, _ := repo.GetByStatus(ctx, domain.ClusterStatusPending)
	if len(pending) != 1 {
		t.Errorf("pending clusters = %d, want 1", len(pending))
	}

	running, _ := repo.GetByStatus(ctx, domain.ClusterStatusRunning)
	if len(running) != 1 {
		t.Errorf("running clusters = %d, want 1", len(running))
	}
}

func TestClusterRepository_Delete(t *testing.T) {
	db := newTestDB(t)
	repo := NewClusterRepository(db.db)
	ctx := context.Background()

	cluster := domain.NewCluster("c1", "h1", nil)
	repo.Create(ctx, cluster)

	if err := repo.Delete(ctx, cluster.ID); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	_, err := repo.GetByID(ctx, cluster.ID)
	if err != domain.ErrClusterNotFound {
		t.Errorf("after delete, GetByID = %v, want ErrClusterNotFound", err)
	}

	if err := repo.Delete(ctx, "fake"); err != domain.ErrClusterNotFound {
		t.Errorf("Delete(fake) = %v, want ErrClusterNotFound", err)
	}
}

func TestClusterRepository_GetClusterByDeviceID(t *testing.T) {
	db := newTestDB(t)
	repo := NewClusterRepository(db.db)
	ctx := context.Background()

	cluster := domain.NewCluster("c1", "head1", []string{"w1", "w2"})
	repo.Create(ctx, cluster)

	// Find by head node
	got, err := repo.GetClusterByDeviceID(ctx, "head1")
	if err != nil {
		t.Fatalf("GetClusterByDeviceID(head1) error: %v", err)
	}
	if got.Name != "c1" {
		t.Errorf("found cluster = %q, want %q", got.Name, "c1")
	}

	// Find by worker
	got, err = repo.GetClusterByDeviceID(ctx, "w1")
	if err != nil {
		t.Fatalf("GetClusterByDeviceID(w1) error: %v", err)
	}
	if got.Name != "c1" {
		t.Errorf("found cluster = %q, want %q", got.Name, "c1")
	}

	// Not found
	_, err = repo.GetClusterByDeviceID(ctx, "unknown")
	if err != domain.ErrClusterNotFound {
		t.Errorf("GetClusterByDeviceID(unknown) = %v, want ErrClusterNotFound", err)
	}
}

// --- ClusterNode Repository Tests ---

func TestClusterNodeRepository_SaveAndGet(t *testing.T) {
	db := newTestDB(t)
	// Need a cluster first (foreign key)
	clusterRepo := NewClusterRepository(db.db)
	ctx := context.Background()
	cluster := domain.NewCluster("c1", "h1", nil)
	clusterRepo.Create(ctx, cluster)

	nodeRepo := NewClusterNodeRepository(db.db)

	node := &domain.ClusterNode{
		DeviceID:    "d1",
		ClusterID:   cluster.ID,
		Role:        domain.NodeRoleHead,
		Status:      domain.NodeStatusRunning,
		RayAddress:  "100.64.0.1:6379",
		NumCPUs:     8,
		NumGPUs:     2,
		MemoryBytes: 16 * 1024 * 1024 * 1024,
		JoinedAt:    time.Now(),
	}

	if err := nodeRepo.Save(ctx, node); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := nodeRepo.GetByDeviceAndCluster(ctx, "d1", cluster.ID)
	if err != nil {
		t.Fatalf("GetByDeviceAndCluster error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil node")
	}
	if got.Role != domain.NodeRoleHead {
		t.Errorf("Role = %q, want %q", got.Role, domain.NodeRoleHead)
	}
	if got.NumCPUs != 8 {
		t.Errorf("NumCPUs = %d, want 8", got.NumCPUs)
	}
	if got.RayAddress != "100.64.0.1:6379" {
		t.Errorf("RayAddress = %q", got.RayAddress)
	}
}

func TestClusterNodeRepository_GetByCluster(t *testing.T) {
	db := newTestDB(t)
	clusterRepo := NewClusterRepository(db.db)
	ctx := context.Background()
	cluster := domain.NewCluster("c1", "h1", nil)
	clusterRepo.Create(ctx, cluster)

	nodeRepo := NewClusterNodeRepository(db.db)
	nodeRepo.Save(ctx, &domain.ClusterNode{DeviceID: "d1", ClusterID: cluster.ID, Role: domain.NodeRoleHead, Status: domain.NodeStatusRunning, JoinedAt: time.Now()})
	nodeRepo.Save(ctx, &domain.ClusterNode{DeviceID: "d2", ClusterID: cluster.ID, Role: domain.NodeRoleWorker, Status: domain.NodeStatusRunning, JoinedAt: time.Now()})

	nodes, err := nodeRepo.GetByCluster(ctx, cluster.ID)
	if err != nil {
		t.Fatalf("GetByCluster error: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(nodes))
	}
}

func TestClusterNodeRepository_Delete(t *testing.T) {
	db := newTestDB(t)
	clusterRepo := NewClusterRepository(db.db)
	ctx := context.Background()
	cluster := domain.NewCluster("c1", "h1", nil)
	clusterRepo.Create(ctx, cluster)

	nodeRepo := NewClusterNodeRepository(db.db)
	nodeRepo.Save(ctx, &domain.ClusterNode{DeviceID: "d1", ClusterID: cluster.ID, Role: domain.NodeRoleHead, Status: domain.NodeStatusRunning, JoinedAt: time.Now()})

	nodeRepo.Delete(ctx, "d1", cluster.ID)

	got, _ := nodeRepo.GetByDeviceAndCluster(ctx, "d1", cluster.ID)
	if got != nil {
		t.Error("node should be deleted")
	}
}

// --- Metrics Repository Tests ---

func TestMetricsRepository_SaveAndGetLatest(t *testing.T) {
	db := newTestDB(t)
	repo := NewMetricsRepository(db.db)
	ctx := context.Background()

	metrics := &domain.DeviceMetrics{
		DeviceID: "dev-1",
		CPU: domain.CPUMetrics{
			UsagePercent: 45.5,
			Cores:        8,
			ModelName:    "Apple M1",
			LoadAvg1:     2.5,
			LoadAvg5:     1.8,
			LoadAvg15:    1.2,
		},
		Memory: domain.MemoryMetrics{
			Total:        16 * 1024 * 1024 * 1024,
			Used:         8 * 1024 * 1024 * 1024,
			Free:         4 * 1024 * 1024 * 1024,
			Available:    8 * 1024 * 1024 * 1024,
			UsagePercent: 50.0,
			SwapTotal:    4 * 1024 * 1024 * 1024,
			SwapUsed:     1 * 1024 * 1024 * 1024,
		},
		Disk: domain.DiskMetrics{
			Partitions: []domain.PartitionMetrics{
				{MountPoint: "/", Device: "/dev/sda1", Total: 500 * 1024 * 1024 * 1024, Used: 250 * 1024 * 1024 * 1024, UsagePercent: 50.0},
			},
		},
		CollectedAt: time.Now(),
	}

	if err := repo.Save(ctx, metrics); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := repo.GetLatest(ctx, "dev-1")
	if err != nil {
		t.Fatalf("GetLatest error: %v", err)
	}
	if got == nil {
		t.Fatal("GetLatest returned nil")
	}
	if got.CPU.Cores != 8 {
		t.Errorf("CPU.Cores = %d, want 8", got.CPU.Cores)
	}
	if got.Memory.UsagePercent != 50.0 {
		t.Errorf("Memory.UsagePercent = %f, want 50.0", got.Memory.UsagePercent)
	}
	if len(got.Disk.Partitions) != 1 {
		t.Errorf("Disk.Partitions length = %d, want 1", len(got.Disk.Partitions))
	}
}

func TestMetricsRepository_GetHistory(t *testing.T) {
	db := newTestDB(t)
	repo := NewMetricsRepository(db.db)
	ctx := context.Background()

	// Save 5 metrics points
	for i := 0; i < 5; i++ {
		repo.Save(ctx, &domain.DeviceMetrics{
			DeviceID:    "dev-1",
			CPU:         domain.CPUMetrics{UsagePercent: float64(i * 10)},
			CollectedAt: time.Now().Add(time.Duration(i) * time.Minute),
		})
	}

	history, err := repo.GetHistory(ctx, "dev-1", 3)
	if err != nil {
		t.Fatalf("GetHistory error: %v", err)
	}
	if len(history.Points) != 3 {
		t.Errorf("history points = %d, want 3", len(history.Points))
	}
	if history.DeviceID != "dev-1" {
		t.Errorf("DeviceID = %q, want %q", history.DeviceID, "dev-1")
	}
}

func TestMetricsRepository_GetSnapshot(t *testing.T) {
	db := newTestDB(t)
	repo := NewMetricsRepository(db.db)
	ctx := context.Background()

	repo.Save(ctx, &domain.DeviceMetrics{DeviceID: "dev-1", CPU: domain.CPUMetrics{UsagePercent: 30}, CollectedAt: time.Now()})
	repo.Save(ctx, &domain.DeviceMetrics{DeviceID: "dev-2", CPU: domain.CPUMetrics{UsagePercent: 60}, CollectedAt: time.Now()})
	// Older entry for dev-1 should not appear
	repo.Save(ctx, &domain.DeviceMetrics{DeviceID: "dev-1", CPU: domain.CPUMetrics{UsagePercent: 90}, CollectedAt: time.Now().Add(time.Second)})

	snapshot, err := repo.GetSnapshot(ctx)
	if err != nil {
		t.Fatalf("GetSnapshot error: %v", err)
	}
	if len(snapshot.Devices) != 2 {
		t.Errorf("snapshot devices = %d, want 2", len(snapshot.Devices))
	}
	// dev-1 should have the latest (90%)
	if m, ok := snapshot.Devices["dev-1"]; ok {
		if m.CPU.UsagePercent != 90 {
			t.Errorf("dev-1 CPU = %f, want 90", m.CPU.UsagePercent)
		}
	} else {
		t.Error("dev-1 not found in snapshot")
	}
}

func TestMetricsRepository_Cleanup(t *testing.T) {
	db := newTestDB(t)
	repo := NewMetricsRepository(db.db)
	ctx := context.Background()

	// Save old and new metrics
	repo.Save(ctx, &domain.DeviceMetrics{DeviceID: "dev-1", CollectedAt: time.Now().AddDate(0, 0, -10)})
	repo.Save(ctx, &domain.DeviceMetrics{DeviceID: "dev-1", CollectedAt: time.Now()})

	if err := repo.Cleanup(ctx, 7); err != nil {
		t.Fatalf("Cleanup error: %v", err)
	}

	history, _ := repo.GetHistory(ctx, "dev-1", 100)
	if len(history.Points) != 1 {
		t.Errorf("after cleanup, points = %d, want 1", len(history.Points))
	}
}

// --- Transaction Tests ---

func TestTransaction(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Commit scenario
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin error: %v", err)
	}

	tx.Devices().Save(ctx, &domain.Device{ID: "tx-1", Name: "tx-device"})
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit error: %v", err)
	}

	got, _ := NewDeviceRepository(db.db).GetByID(ctx, "tx-1")
	if got == nil {
		t.Error("committed device should exist")
	}

	// Rollback scenario
	tx2, _ := db.Begin(ctx)
	tx2.Devices().Save(ctx, &domain.Device{ID: "tx-2", Name: "rolled-back"})
	tx2.Rollback()

	got2, _ := NewDeviceRepository(db.db).GetByID(ctx, "tx-2")
	if got2 != nil {
		t.Error("rolled-back device should not exist")
	}
}
