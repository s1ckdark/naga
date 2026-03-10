package domain

import "time"

// Heartbeat represents a heartbeat message between nodes
type Heartbeat struct {
	NodeID    string            `json:"nodeId"`
	ClusterID string            `json:"clusterId"`
	Role      NodeRole          `json:"role"`
	Timestamp time.Time         `json:"timestamp"`
	Metrics   *HeartbeatMetrics `json:"metrics,omitempty"`
}

// HeartbeatMetrics is a lightweight metrics snapshot sent with heartbeats
type HeartbeatMetrics struct {
	GPUUtilization float64 `json:"gpuUtilization"`
	MemoryFreeGB   float64 `json:"memoryFreeGB"`
	RunningJobs    int     `json:"runningJobs"`
}

// IsExpired returns true if the heartbeat is older than the given timeout
func (hb *Heartbeat) IsExpired(timeout time.Duration) bool {
	return time.Since(hb.Timestamp) > timeout
}

// NodeHealth tracks a node's health state based on heartbeats
type NodeHealth struct {
	NodeID        string            `json:"nodeId"`
	ClusterID     string            `json:"clusterId"`
	Role          NodeRole          `json:"role"`
	Status        NodeStatus        `json:"status"`
	LastHeartbeat time.Time         `json:"lastHeartbeat"`
	LastMetrics   *HeartbeatMetrics `json:"lastMetrics,omitempty"`
	FailureCount  int               `json:"failureCount"`
}

// IsHealthy returns true if the node's last heartbeat is within timeout
func (nh *NodeHealth) IsHealthy(timeout time.Duration) bool {
	return time.Since(nh.LastHeartbeat) <= timeout
}

// ElectionCandidate represents a node that can become head
type ElectionCandidate struct {
	NodeID         string        `json:"nodeId"`
	GPUUtilization float64       `json:"gpuUtilization"`
	MemoryFreeGB   float64       `json:"memoryFreeGB"`
	RunningJobs    int           `json:"runningJobs"`
	Latency        time.Duration `json:"latency"`
}

// RuleBasedScore calculates a score for head selection (higher = better candidate)
func (c *ElectionCandidate) RuleBasedScore() float64 {
	gpuScore := (100 - c.GPUUtilization) * 0.4
	memScore := c.MemoryFreeGB * 2.0 * 0.3
	latencyMs := float64(c.Latency.Milliseconds())
	latencyScore := (100 - latencyMs) * 0.2
	if latencyScore < 0 {
		latencyScore = 0
	}
	jobScore := float64(100-c.RunningJobs*10) * 0.1
	if jobScore < 0 {
		jobScore = 0
	}
	return gpuScore + memScore + latencyScore + jobScore
}

// ElectionResult represents the outcome of a head election
type ElectionResult struct {
	NewHeadID  string              `json:"newHeadId"`
	Reason     string              `json:"reason"`
	AIDecision bool                `json:"aiDecision"`
	DecidedAt  time.Time           `json:"decidedAt"`
	Candidates []ElectionCandidate `json:"candidates"`
}
