package domain

import (
	"testing"
)

func TestNewCluster(t *testing.T) {
	workers := []string{"w1", "w2"}
	c := NewCluster("test-cluster", "head1", workers)

	if c.Name != "test-cluster" {
		t.Errorf("Name = %q, want %q", c.Name, "test-cluster")
	}
	if c.HeadNodeID != "head1" {
		t.Errorf("HeadNodeID = %q, want %q", c.HeadNodeID, "head1")
	}
	if c.Status != ClusterStatusPending {
		t.Errorf("Status = %q, want %q", c.Status, ClusterStatusPending)
	}
	if c.RayPort != 6379 {
		t.Errorf("RayPort = %d, want 6379", c.RayPort)
	}
	if c.DashboardPort != 8265 {
		t.Errorf("DashboardPort = %d, want 8265", c.DashboardPort)
	}
	if len(c.WorkerIDs) != 2 {
		t.Fatalf("WorkerIDs length = %d, want 2", len(c.WorkerIDs))
	}
	if c.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestCluster_TotalNodes(t *testing.T) {
	tests := []struct {
		name    string
		workers []string
		want    int
	}{
		{"head only", nil, 1},
		{"head + 1 worker", []string{"w1"}, 2},
		{"head + 3 workers", []string{"w1", "w2", "w3"}, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCluster("c", "h", tt.workers)
			if got := c.TotalNodes(); got != tt.want {
				t.Errorf("TotalNodes() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCluster_AllNodeIDs(t *testing.T) {
	c := NewCluster("c", "head1", []string{"w1", "w2"})
	ids := c.AllNodeIDs()

	if len(ids) != 3 {
		t.Fatalf("AllNodeIDs() length = %d, want 3", len(ids))
	}
	if ids[0] != "head1" {
		t.Errorf("first ID = %q, want %q", ids[0], "head1")
	}
	if ids[1] != "w1" || ids[2] != "w2" {
		t.Errorf("worker IDs = %v, want [w1 w2]", ids[1:])
	}
}

func TestCluster_HasWorker(t *testing.T) {
	c := NewCluster("c", "head1", []string{"w1", "w2"})

	if !c.HasWorker("w1") {
		t.Error("HasWorker(w1) = false, want true")
	}
	if c.HasWorker("w3") {
		t.Error("HasWorker(w3) = true, want false")
	}
	if c.HasWorker("head1") {
		t.Error("HasWorker(head1) = true, want false (head is not a worker)")
	}
}

func TestCluster_IsRunning(t *testing.T) {
	c := NewCluster("c", "h", nil)

	if c.IsRunning() {
		t.Error("pending cluster should not be running")
	}

	c.Status = ClusterStatusRunning
	if !c.IsRunning() {
		t.Error("running cluster should be running")
	}
}

func TestCluster_CanModify(t *testing.T) {
	tests := []struct {
		status ClusterStatus
		want   bool
	}{
		{ClusterStatusPending, true},
		{ClusterStatusStopped, true},
		{ClusterStatusRunning, true},
		{ClusterStatusStarting, false},
		{ClusterStatusStopping, false},
		{ClusterStatusError, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			c := NewCluster("c", "h", nil)
			c.Status = tt.status
			if got := c.CanModify(); got != tt.want {
				t.Errorf("CanModify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCluster_AddWorker(t *testing.T) {
	c := NewCluster("c", "head1", []string{"w1"})

	// Add new worker
	if err := c.AddWorker("w2"); err != nil {
		t.Fatalf("AddWorker(w2) error: %v", err)
	}
	if len(c.WorkerIDs) != 2 {
		t.Fatalf("WorkerIDs length = %d, want 2", len(c.WorkerIDs))
	}

	// Duplicate worker
	if err := c.AddWorker("w1"); err != ErrNodeAlreadyInCluster {
		t.Errorf("AddWorker(w1) = %v, want ErrNodeAlreadyInCluster", err)
	}

	// Add head as worker
	if err := c.AddWorker("head1"); err == nil {
		t.Error("AddWorker(head1) should fail")
	}
}

func TestCluster_RemoveWorker(t *testing.T) {
	c := NewCluster("c", "head1", []string{"w1", "w2", "w3"})

	// Remove existing worker
	if err := c.RemoveWorker("w2"); err != nil {
		t.Fatalf("RemoveWorker(w2) error: %v", err)
	}
	if len(c.WorkerIDs) != 2 {
		t.Fatalf("WorkerIDs length = %d, want 2", len(c.WorkerIDs))
	}
	if c.HasWorker("w2") {
		t.Error("w2 should be removed")
	}

	// Remove non-existent worker
	if err := c.RemoveWorker("w99"); err != ErrNodeNotInCluster {
		t.Errorf("RemoveWorker(w99) = %v, want ErrNodeNotInCluster", err)
	}

	// Cannot remove head
	if err := c.RemoveWorker("head1"); err != ErrCannotRemoveHead {
		t.Errorf("RemoveWorker(head1) = %v, want ErrCannotRemoveHead", err)
	}
}

func TestCluster_ChangeHead(t *testing.T) {
	c := NewCluster("c", "head1", []string{"w1", "w2"})

	// Change to existing worker
	if err := c.ChangeHead("w1"); err != nil {
		t.Fatalf("ChangeHead(w1) error: %v", err)
	}
	if c.HeadNodeID != "w1" {
		t.Errorf("HeadNodeID = %q, want %q", c.HeadNodeID, "w1")
	}
	// Old head should become worker
	if !c.HasWorker("head1") {
		t.Error("old head should become worker")
	}
	// New head should not be in workers
	if c.HasWorker("w1") {
		t.Error("new head should not be in workers")
	}
	// Total node count should remain the same
	if c.TotalNodes() != 3 {
		t.Errorf("TotalNodes() = %d, want 3", c.TotalNodes())
	}

	// Change to same head (no-op)
	if err := c.ChangeHead("w1"); err != nil {
		t.Fatalf("ChangeHead(same) error: %v", err)
	}

	// Change to external node (not in workers)
	if err := c.ChangeHead("external1"); err != nil {
		t.Fatalf("ChangeHead(external1) error: %v", err)
	}
	if c.HeadNodeID != "external1" {
		t.Errorf("HeadNodeID = %q, want %q", c.HeadNodeID, "external1")
	}
	// Total nodes should increase by 1 (old head added as worker, external was not removed from workers)
	if c.TotalNodes() != 4 {
		t.Errorf("TotalNodes() = %d, want 4", c.TotalNodes())
	}
}

func TestCluster_SetError(t *testing.T) {
	c := NewCluster("c", "h", nil)
	c.Status = ClusterStatusRunning

	c.SetError("connection failed")

	if c.Status != ClusterStatusError {
		t.Errorf("Status = %q, want %q", c.Status, ClusterStatusError)
	}
	if c.LastError != "connection failed" {
		t.Errorf("LastError = %q, want %q", c.LastError, "connection failed")
	}
	if c.LastErrorAt == nil {
		t.Error("LastErrorAt should not be nil")
	}
}
