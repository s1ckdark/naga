package domain

import (
	"testing"
	"time"
)

func TestHeartbeat_IsExpired(t *testing.T) {
	hb := &Heartbeat{
		NodeID:    "node-1",
		Timestamp: time.Now().Add(-20 * time.Second),
	}
	if !hb.IsExpired(15 * time.Second) {
		t.Error("expected heartbeat to be expired after 20s with 15s timeout")
	}

	hb.Timestamp = time.Now().Add(-5 * time.Second)
	if hb.IsExpired(15 * time.Second) {
		t.Error("expected heartbeat to not be expired after 5s with 15s timeout")
	}
}

func TestNodeHealth_IsHealthy(t *testing.T) {
	nh := &NodeHealth{
		NodeID:        "node-1",
		Role:          NodeRoleHead,
		Status:        NodeStatusRunning,
		LastHeartbeat: time.Now(),
	}
	if !nh.IsHealthy(15 * time.Second) {
		t.Error("expected node to be healthy")
	}

	nh.LastHeartbeat = time.Now().Add(-20 * time.Second)
	if nh.IsHealthy(15 * time.Second) {
		t.Error("expected node to be unhealthy after 20s")
	}
}

func TestElectionCandidate_Score(t *testing.T) {
	c := &ElectionCandidate{
		NodeID:         "worker-1",
		GPUUtilization: 30.0,
		MemoryFreeGB:   16.0,
		Latency:        5 * time.Millisecond,
	}

	score := c.RuleBasedScore()
	if score <= 0 {
		t.Errorf("expected positive score, got %v", score)
	}

	c2 := &ElectionCandidate{
		NodeID:         "worker-2",
		GPUUtilization: 90.0,
		MemoryFreeGB:   4.0,
		Latency:        50 * time.Millisecond,
	}

	if c2.RuleBasedScore() >= c.RuleBasedScore() {
		t.Error("expected worker-1 (lower util) to score higher than worker-2")
	}
}
