package domain

import (
	"testing"
	"time"
)

func TestClusterNode_Roles(t *testing.T) {
	head := &ClusterNode{Role: NodeRoleHead}
	worker := &ClusterNode{Role: NodeRoleWorker}

	if !head.IsHead() {
		t.Error("head node IsHead() = false")
	}
	if head.IsWorker() {
		t.Error("head node IsWorker() = true")
	}
	if worker.IsHead() {
		t.Error("worker node IsHead() = true")
	}
	if !worker.IsWorker() {
		t.Error("worker node IsWorker() = false")
	}
}

func TestClusterNode_IsRunning(t *testing.T) {
	tests := []struct {
		status NodeStatus
		want   bool
	}{
		{NodeStatusRunning, true},
		{NodeStatusPending, false},
		{NodeStatusStopped, false},
		{NodeStatusError, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			n := &ClusterNode{Status: tt.status}
			if got := n.IsRunning(); got != tt.want {
				t.Errorf("IsRunning() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClusterNode_IsHealthy(t *testing.T) {
	n := &ClusterNode{Status: NodeStatusRunning}
	if !n.IsHealthy() {
		t.Error("running node should be healthy")
	}

	n.Status = NodeStatusError
	if n.IsHealthy() {
		t.Error("error node should not be healthy")
	}
}

func TestClusterNode_SetError(t *testing.T) {
	n := &ClusterNode{
		DeviceID:  "d1",
		ClusterID: "c1",
		Status:    NodeStatusRunning,
	}

	n.SetError("timeout")

	if n.Status != NodeStatusError {
		t.Errorf("Status = %q, want %q", n.Status, NodeStatusError)
	}
	if n.LastError != "timeout" {
		t.Errorf("LastError = %q, want %q", n.LastError, "timeout")
	}
	if n.LastErrorAt == nil {
		t.Error("LastErrorAt should not be nil")
	}
	if time.Since(*n.LastErrorAt) > time.Second {
		t.Error("LastErrorAt should be recent")
	}
}
