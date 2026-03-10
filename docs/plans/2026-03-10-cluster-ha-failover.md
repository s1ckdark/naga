# Cluster HA Failover Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add automatic head node failover with AI-driven selection, dual-layer failure detection, checkpoint-based recovery, and systemd-managed node agents.

**Architecture:** Each node runs a lightweight agent (systemd service) that handles heartbeat, metrics collection, and participates in head election. The clusterctl server monitors all nodes (primary detection). If the server is also down, workers self-detect and elect a new head using rule-based fallback. When available, Claude API analyzes node metrics to pick the optimal new head.

**Tech Stack:** Go (agent binary), systemd (process management), Claude API (AI selection), existing SSH executor, Ray checkpoint API

---

### Task 1: Heartbeat domain types

**Files:**
- Create: `internal/domain/heartbeat.go`
- Test: `internal/domain/heartbeat_test.go`

**Step 1: Write the failing test**

```go
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
		NodeID:       "node-1",
		Role:         NodeRoleHead,
		Status:       NodeStatusRunning,
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

	// Higher GPU util = lower score
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run "TestHeartbeat|TestNodeHealth|TestElectionCandidate" -v`
Expected: FAIL — types not defined

**Step 3: Write implementation**

```go
package domain

import "time"

// Heartbeat represents a heartbeat message between nodes
type Heartbeat struct {
	NodeID    string    `json:"nodeId"`
	ClusterID string    `json:"clusterId"`
	Role      NodeRole  `json:"role"`
	Timestamp time.Time `json:"timestamp"`
	Metrics   *HeartbeatMetrics `json:"metrics,omitempty"`
}

// HeartbeatMetrics is a lightweight metrics snapshot sent with heartbeats
type HeartbeatMetrics struct {
	GPUUtilization float64 `json:"gpuUtilization"` // average across GPUs
	MemoryFreeGB   float64 `json:"memoryFreeGB"`
	RunningJobs    int     `json:"runningJobs"`
}

// IsExpired returns true if the heartbeat is older than the given timeout
func (hb *Heartbeat) IsExpired(timeout time.Duration) bool {
	return time.Since(hb.Timestamp) > timeout
}

// NodeHealth tracks a node's health state based on heartbeats
type NodeHealth struct {
	NodeID        string     `json:"nodeId"`
	ClusterID     string     `json:"clusterId"`
	Role          NodeRole   `json:"role"`
	Status        NodeStatus `json:"status"`
	LastHeartbeat time.Time  `json:"lastHeartbeat"`
	LastMetrics   *HeartbeatMetrics `json:"lastMetrics,omitempty"`
	FailureCount  int        `json:"failureCount"`
}

// IsHealthy returns true if the node's last heartbeat is within timeout
func (nh *NodeHealth) IsHealthy(timeout time.Duration) bool {
	return time.Since(nh.LastHeartbeat) <= timeout
}

// ElectionCandidate represents a node that can become head
type ElectionCandidate struct {
	NodeID         string        `json:"nodeId"`
	GPUUtilization float64       `json:"gpuUtilization"` // percent
	MemoryFreeGB   float64       `json:"memoryFreeGB"`
	RunningJobs    int           `json:"runningJobs"`
	Latency        time.Duration `json:"latency"`
}

// RuleBasedScore calculates a score for head selection (higher = better candidate)
// Weights: GPU utilization (40%), memory (30%), latency (20%), running jobs (10%)
func (c *ElectionCandidate) RuleBasedScore() float64 {
	gpuScore := (100 - c.GPUUtilization) * 0.4
	memScore := c.MemoryFreeGB * 2.0 * 0.3 // normalize: 50GB free = 100 points
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
	NewHeadID  string    `json:"newHeadId"`
	Reason     string    `json:"reason"`
	AIDecision bool      `json:"aiDecision"` // true if AI made the decision
	DecidedAt  time.Time `json:"decidedAt"`
	Candidates []ElectionCandidate `json:"candidates"`
}
```

**Step 4: Run tests**

Run: `go test ./internal/domain/ -run "TestHeartbeat|TestNodeHealth|TestElectionCandidate" -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/domain/heartbeat.go internal/domain/heartbeat_test.go
git commit -m "feat: add heartbeat, node health, and election domain types"
```

---

### Task 2: Agent config and heartbeat protocol

**Files:**
- Modify: `config/config.go` — add AgentConfig
- Create: `internal/agent/heartbeat.go`
- Test: `internal/agent/heartbeat_test.go`

**Step 1: Add AgentConfig to config.go**

Add to `Config` struct after `Log LogConfig`:
```go
Agent AgentConfig `mapstructure:"agent"`
```

Add new config type:
```go
// AgentConfig holds node agent settings
type AgentConfig struct {
	HeartbeatInterval int    `mapstructure:"heartbeat_interval"` // seconds, default 3
	HealthCheckInterval int  `mapstructure:"healthcheck_interval"` // seconds, default 5
	FailureTimeout    int    `mapstructure:"failure_timeout"`     // seconds, default 15
	CheckpointDir     string `mapstructure:"checkpoint_dir"`
	AnthropicAPIKey   string `mapstructure:"anthropic_api_key"`
	AgentPort         int    `mapstructure:"agent_port"`          // default 9090
}
```

Add defaults in `DefaultConfig()`:
```go
Agent: AgentConfig{
	HeartbeatInterval:   3,
	HealthCheckInterval: 5,
	FailureTimeout:      15,
	CheckpointDir:       "/tmp/ray-checkpoints",
	AgentPort:           9090,
},
```

**Step 2: Write heartbeat test**

```go
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

func TestHeartbeatMonitor_DetectFailure(t *testing.T) {
	mon := NewHeartbeatMonitor(3*time.Second, 500*time.Millisecond) // short timeout for test

	// Register a head node
	mon.RecordHeartbeat(domain.Heartbeat{
		NodeID:    "head-1",
		ClusterID: "cluster-1",
		Role:      domain.NodeRoleHead,
		Timestamp: time.Now(),
	})

	// Should be healthy
	if !mon.IsNodeHealthy("head-1") {
		t.Error("expected head-1 to be healthy")
	}

	// Simulate time passing beyond timeout
	mon.nodeHealth["head-1"].LastHeartbeat = time.Now().Add(-4 * time.Second)

	// Should be unhealthy
	if mon.IsNodeHealthy("head-1") {
		t.Error("expected head-1 to be unhealthy after timeout")
	}

	// Check failed nodes
	failed := mon.GetFailedNodes("cluster-1")
	if len(failed) != 1 || failed[0].NodeID != "head-1" {
		t.Errorf("expected 1 failed node (head-1), got %v", failed)
	}
}

func TestHeartbeatMonitor_GetHealthyWorkers(t *testing.T) {
	mon := NewHeartbeatMonitor(15*time.Second, time.Second)

	now := time.Now()
	mon.RecordHeartbeat(domain.Heartbeat{NodeID: "head-1", ClusterID: "c1", Role: domain.NodeRoleHead, Timestamp: now})
	mon.RecordHeartbeat(domain.Heartbeat{NodeID: "worker-1", ClusterID: "c1", Role: domain.NodeRoleWorker, Timestamp: now})
	mon.RecordHeartbeat(domain.Heartbeat{NodeID: "worker-2", ClusterID: "c1", Role: domain.NodeRoleWorker, Timestamp: now})

	workers := mon.GetHealthyWorkers("c1")
	if len(workers) != 2 {
		t.Errorf("expected 2 healthy workers, got %d", len(workers))
	}
}
```

**Step 3: Write implementation**

```go
package agent

import (
	"sync"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

// HeartbeatMonitor tracks heartbeats from cluster nodes
type HeartbeatMonitor struct {
	timeout    time.Duration
	checkInterval time.Duration

	mu         sync.RWMutex
	nodeHealth map[string]*domain.NodeHealth // keyed by nodeID
}

// NewHeartbeatMonitor creates a new heartbeat monitor
func NewHeartbeatMonitor(timeout, checkInterval time.Duration) *HeartbeatMonitor {
	return &HeartbeatMonitor{
		timeout:       timeout,
		checkInterval: checkInterval,
		nodeHealth:    make(map[string]*domain.NodeHealth),
	}
}

// RecordHeartbeat records a heartbeat from a node
func (m *HeartbeatMonitor) RecordHeartbeat(hb domain.Heartbeat) {
	m.mu.Lock()
	defer m.mu.Unlock()

	nh, exists := m.nodeHealth[hb.NodeID]
	if !exists {
		nh = &domain.NodeHealth{
			NodeID:    hb.NodeID,
			ClusterID: hb.ClusterID,
			Role:      hb.Role,
			Status:    domain.NodeStatusRunning,
		}
		m.nodeHealth[hb.NodeID] = nh
	}

	nh.LastHeartbeat = hb.Timestamp
	nh.LastMetrics = hb.Metrics
	nh.FailureCount = 0
	nh.Status = domain.NodeStatusRunning
}

// IsNodeHealthy checks if a node is healthy
func (m *HeartbeatMonitor) IsNodeHealthy(nodeID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nh, exists := m.nodeHealth[nodeID]
	if !exists {
		return false
	}
	return nh.IsHealthy(m.timeout)
}

// GetFailedNodes returns nodes that have exceeded the heartbeat timeout
func (m *HeartbeatMonitor) GetFailedNodes(clusterID string) []*domain.NodeHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var failed []*domain.NodeHealth
	for _, nh := range m.nodeHealth {
		if nh.ClusterID == clusterID && !nh.IsHealthy(m.timeout) {
			failed = append(failed, nh)
		}
	}
	return failed
}

// GetHealthyWorkers returns healthy worker nodes for a cluster
func (m *HeartbeatMonitor) GetHealthyWorkers(clusterID string) []*domain.NodeHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var workers []*domain.NodeHealth
	for _, nh := range m.nodeHealth {
		if nh.ClusterID == clusterID &&
			nh.Role == domain.NodeRoleWorker &&
			nh.IsHealthy(m.timeout) {
			workers = append(workers, nh)
		}
	}
	return workers
}

// RemoveNode removes a node from tracking
func (m *HeartbeatMonitor) RemoveNode(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.nodeHealth, nodeID)
}
```

**Step 4: Run tests**

Run: `go test ./internal/agent/ -run "TestHeartbeat" -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add config/config.go internal/agent/heartbeat.go internal/agent/heartbeat_test.go
git commit -m "feat: add heartbeat monitor with failure detection"
```

---

### Task 3: Election logic (rule-based + AI)

**Files:**
- Create: `internal/agent/election.go`
- Create: `internal/agent/election_test.go`
- Create: `internal/infra/ai/selector.go`

**Step 1: Write election test**

```go
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

type mockAISelector struct {
	result   string
	reason   string
	shouldErr bool
}

func (m *mockAISelector) SelectHead(ctx context.Context, candidates []domain.ElectionCandidate) (string, string, error) {
	if m.shouldErr {
		return "", "", context.DeadlineExceeded
	}
	return m.result, m.reason, nil
}

func TestElection_RuleBasedFallback(t *testing.T) {
	candidates := []domain.ElectionCandidate{
		{NodeID: "worker-1", GPUUtilization: 80, MemoryFreeGB: 4, Latency: 10 * time.Millisecond},
		{NodeID: "worker-2", GPUUtilization: 20, MemoryFreeGB: 16, Latency: 5 * time.Millisecond},
		{NodeID: "worker-3", GPUUtilization: 50, MemoryFreeGB: 8, Latency: 8 * time.Millisecond},
	}

	// AI unavailable — should fallback to rule-based
	e := NewElection(&mockAISelector{shouldErr: true})
	result, err := e.Elect(context.Background(), "cluster-1", candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.NewHeadID != "worker-2" {
		t.Errorf("expected worker-2 (lowest util + most memory), got %s", result.NewHeadID)
	}
	if result.AIDecision {
		t.Error("expected rule-based decision, got AI decision")
	}
}

func TestElection_AIDecision(t *testing.T) {
	candidates := []domain.ElectionCandidate{
		{NodeID: "worker-1", GPUUtilization: 80, MemoryFreeGB: 4},
		{NodeID: "worker-2", GPUUtilization: 20, MemoryFreeGB: 16},
	}

	ai := &mockAISelector{result: "worker-1", reason: "worker-1 has better network position"}
	e := NewElection(ai)
	result, err := e.Elect(context.Background(), "cluster-1", candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.NewHeadID != "worker-1" {
		t.Errorf("expected AI choice worker-1, got %s", result.NewHeadID)
	}
	if !result.AIDecision {
		t.Error("expected AI decision flag to be true")
	}
}

func TestElection_NoCandidates(t *testing.T) {
	e := NewElection(&mockAISelector{shouldErr: true})
	_, err := e.Elect(context.Background(), "cluster-1", nil)
	if err == nil {
		t.Error("expected error for empty candidates")
	}
}
```

**Step 2: Run to verify fail**

Run: `go test ./internal/agent/ -run TestElection -v`
Expected: FAIL

**Step 3: Write election implementation**

```go
package agent

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

// AISelector is the interface for AI-based head selection
type AISelector interface {
	SelectHead(ctx context.Context, candidates []domain.ElectionCandidate) (nodeID string, reason string, err error)
}

// Election handles head node election
type Election struct {
	ai AISelector
}

// NewElection creates a new election handler
func NewElection(ai AISelector) *Election {
	return &Election{ai: ai}
}

// Elect selects a new head from candidates
// Tries AI first, falls back to rule-based scoring
func (e *Election) Elect(ctx context.Context, clusterID string, candidates []domain.ElectionCandidate) (*domain.ElectionResult, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no candidates available for election in cluster %s", clusterID)
	}

	result := &domain.ElectionResult{
		DecidedAt:  time.Now(),
		Candidates: candidates,
	}

	// Try AI selection first
	if e.ai != nil {
		aiCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		nodeID, reason, err := e.ai.SelectHead(aiCtx, candidates)
		if err == nil && nodeID != "" {
			// Validate AI chose a valid candidate
			for _, c := range candidates {
				if c.NodeID == nodeID {
					result.NewHeadID = nodeID
					result.Reason = reason
					result.AIDecision = true
					return result, nil
				}
			}
			// AI returned invalid node, fall through to rule-based
		}
		// AI failed or returned invalid, fall through
	}

	// Rule-based fallback: sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].RuleBasedScore() > candidates[j].RuleBasedScore()
	})

	result.NewHeadID = candidates[0].NodeID
	result.Reason = fmt.Sprintf("rule-based: highest score %.1f (GPU: %.0f%%, mem: %.1fGB free)",
		candidates[0].RuleBasedScore(), candidates[0].GPUUtilization, candidates[0].MemoryFreeGB)
	result.AIDecision = false

	return result, nil
}
```

**Step 4: Write AI selector (Claude API)**

`internal/infra/ai/selector.go`:
```go
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"bytes"

	"github.com/dave/clusterctl/internal/domain"
)

// ClaudeSelector uses Claude API to select the best head node
type ClaudeSelector struct {
	apiKey string
	model  string
}

// NewClaudeSelector creates a new Claude-based head selector
func NewClaudeSelector(apiKey string) *ClaudeSelector {
	return &ClaudeSelector{
		apiKey: apiKey,
		model:  "claude-sonnet-4-6",
	}
}

// SelectHead asks Claude to select the best head node candidate
func (s *ClaudeSelector) SelectHead(ctx context.Context, candidates []domain.ElectionCandidate) (string, string, error) {
	if s.apiKey == "" {
		return "", "", fmt.Errorf("anthropic API key not configured")
	}

	prompt := buildSelectionPrompt(candidates)

	reqBody := map[string]interface{}{
		"model":      s.model,
		"max_tokens": 256,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("claude API returned status %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}

	if len(result.Content) == 0 {
		return "", "", fmt.Errorf("empty response from claude")
	}

	// Parse structured response
	var selection struct {
		NodeID string `json:"node_id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &selection); err != nil {
		return "", "", fmt.Errorf("failed to parse AI response: %w", err)
	}

	return selection.NodeID, selection.Reason, nil
}

func buildSelectionPrompt(candidates []domain.ElectionCandidate) string {
	candidateJSON, _ := json.MarshalIndent(candidates, "", "  ")
	return fmt.Sprintf(`You are a cluster management AI. The head node has failed and you must select the best replacement from the available worker nodes.

Candidates:
%s

Select the best candidate considering:
1. Lower GPU utilization = better (head needs resources for coordination)
2. More free memory = better
3. Lower latency = better (faster coordination)
4. Fewer running jobs = better (less disruption)

Respond with ONLY valid JSON:
{"node_id": "<selected_node_id>", "reason": "<brief explanation>"}`, string(candidateJSON))
}
```

**Step 5: Run tests**

Run: `go test ./internal/agent/ -run TestElection -v`
Expected: all PASS

**Step 6: Verify AI selector compiles**

Run: `go build ./internal/infra/ai/`
Expected: success

**Step 7: Commit**

```bash
git add internal/agent/election.go internal/agent/election_test.go internal/infra/ai/selector.go
git commit -m "feat: add head election with AI selection and rule-based fallback"
```

---

### Task 4: Failover usecase

**Files:**
- Create: `internal/usecase/failover_usecase.go`
- Test: `internal/usecase/failover_usecase_test.go`

**Step 1: Write test**

```go
package usecase

import (
	"context"
	"testing"

	"github.com/dave/clusterctl/internal/domain"
)

type mockRayManagerForFailover struct {
	started    map[string]bool
	stopped    map[string]bool
	checkpoint string
}

func newMockRayManager() *mockRayManagerForFailover {
	return &mockRayManagerForFailover{
		started: make(map[string]bool),
		stopped: make(map[string]bool),
	}
}

func (m *mockRayManagerForFailover) StartHead(ctx context.Context, device *domain.Device, port, dashPort int) error {
	m.started[device.ID] = true
	return nil
}
func (m *mockRayManagerForFailover) StartWorker(ctx context.Context, device *domain.Device, headAddr string) error {
	m.started[device.ID] = true
	return nil
}
func (m *mockRayManagerForFailover) StopRay(ctx context.Context, device *domain.Device) error {
	m.stopped[device.ID] = true
	return nil
}
func (m *mockRayManagerForFailover) GetClusterInfo(ctx context.Context, head *domain.Device) (*domain.RayClusterInfo, error) {
	return &domain.RayClusterInfo{}, nil
}
func (m *mockRayManagerForFailover) CheckRayInstalled(ctx context.Context, device *domain.Device) (bool, string, error) {
	return true, "2.9.0", nil
}
func (m *mockRayManagerForFailover) InstallRay(ctx context.Context, device *domain.Device, version string) error {
	return nil
}
func (m *mockRayManagerForFailover) HasRunningJobs(ctx context.Context, head *domain.Device) (bool, error) {
	return false, nil
}
func (m *mockRayManagerForFailover) SaveCheckpoint(ctx context.Context, head *domain.Device, dir string) error {
	m.checkpoint = dir
	return nil
}
func (m *mockRayManagerForFailover) RestoreCheckpoint(ctx context.Context, head *domain.Device, dir string) error {
	return nil
}

func TestFailoverUseCase_ExecuteFailover(t *testing.T) {
	ray := newMockRayManager()
	uc := NewFailoverUseCase(ray)

	cluster := &domain.Cluster{
		ID:         "c1",
		Name:       "test-cluster",
		HeadNodeID: "old-head",
		WorkerIDs:  []string{"worker-1", "worker-2"},
		RayPort:    6379,
		DashboardPort: 8265,
		Status:     domain.ClusterStatusRunning,
	}

	devices := map[string]*domain.Device{
		"old-head":  {ID: "old-head", Name: "old-head", TailscaleIP: "100.64.0.1", Status: domain.DeviceStatusOffline},
		"worker-1":  {ID: "worker-1", Name: "worker-1", TailscaleIP: "100.64.0.2", Status: domain.DeviceStatusOnline, SSHEnabled: true},
		"worker-2":  {ID: "worker-2", Name: "worker-2", TailscaleIP: "100.64.0.3", Status: domain.DeviceStatusOnline, SSHEnabled: true},
	}

	result := &domain.ElectionResult{
		NewHeadID:  "worker-1",
		Reason:     "lowest GPU usage",
		AIDecision: false,
	}

	err := uc.ExecuteFailover(context.Background(), cluster, result, devices, "/tmp/checkpoints")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// New head should be started as head
	if !ray.started["worker-1"] {
		t.Error("expected worker-1 to be started as head")
	}
	// worker-2 should be reconnected
	if !ray.started["worker-2"] {
		t.Error("expected worker-2 to be reconnected")
	}
	// Cluster should be updated
	if cluster.HeadNodeID != "worker-1" {
		t.Errorf("expected head to be worker-1, got %s", cluster.HeadNodeID)
	}
}
```

**Step 2: Run to verify fail**

Run: `go test ./internal/usecase/ -run TestFailoverUseCase -v`
Expected: FAIL

**Step 3: Write implementation**

```go
package usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

// FailoverRayManager extends RayManager with checkpoint operations
type FailoverRayManager interface {
	RayManager
	SaveCheckpoint(ctx context.Context, headDevice *domain.Device, checkpointDir string) error
	RestoreCheckpoint(ctx context.Context, headDevice *domain.Device, checkpointDir string) error
}

// FailoverUseCase handles head node failover
type FailoverUseCase struct {
	rayManager FailoverRayManager
}

// NewFailoverUseCase creates a new FailoverUseCase
func NewFailoverUseCase(rayManager FailoverRayManager) *FailoverUseCase {
	return &FailoverUseCase{rayManager: rayManager}
}

// ExecuteFailover performs a head failover:
// 1. Try to save checkpoint from old head (best-effort)
// 2. Stop Ray on old head (best-effort)
// 3. Update cluster configuration
// 4. Start Ray head on new node
// 5. Reconnect workers
// 6. Restore checkpoint
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

	// Step 1: Try to save checkpoint from old head (best-effort)
	if oldHeadDevice != nil && oldHeadDevice.IsOnline() && checkpointDir != "" {
		saveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := uc.rayManager.SaveCheckpoint(saveCtx, oldHeadDevice, checkpointDir); err != nil {
			log.Printf("Warning: failed to save checkpoint from old head: %v", err)
		}
		cancel()
	}

	// Step 2: Stop Ray on old head (best-effort)
	if oldHeadDevice != nil && oldHeadDevice.IsOnline() {
		stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := uc.rayManager.StopRay(stopCtx, oldHeadDevice); err != nil {
			log.Printf("Warning: failed to stop Ray on old head: %v", err)
		}
		cancel()
	}

	// Step 3: Update cluster configuration
	cluster.ChangeHead(newHeadID)
	cluster.Status = domain.ClusterStatusStarting
	now := time.Now()
	cluster.StartedAt = &now

	// Step 4: Start Ray head on new node
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

	// Step 6: Restore checkpoint (best-effort)
	if checkpointDir != "" {
		restoreCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := uc.rayManager.RestoreCheckpoint(restoreCtx, newHeadDevice, checkpointDir); err != nil {
			log.Printf("Warning: failed to restore checkpoint: %v", err)
		}
		cancel()
	}

	// Mark cluster as running
	cluster.Status = domain.ClusterStatusRunning
	cluster.DashboardURL = fmt.Sprintf("http://%s:%d", newHeadDevice.TailscaleIP, cluster.DashboardPort)
	cluster.UpdatedAt = time.Now()

	return nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/usecase/ -run TestFailoverUseCase -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/usecase/failover_usecase.go internal/usecase/failover_usecase_test.go
git commit -m "feat: add failover usecase with checkpoint save/restore"
```

---

### Task 5: Node agent main loop

**Files:**
- Create: `internal/agent/agent.go`
- Create: `cmd/cluster-agent/main.go`

**Step 1: Write the agent**

`internal/agent/agent.go`:
```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

// Agent is the node agent that runs on each cluster node
type Agent struct {
	nodeID    string
	clusterID string
	role      domain.NodeRole
	listenAddr string

	heartbeatInterval time.Duration
	failureTimeout    time.Duration

	monitor  *HeartbeatMonitor
	election *Election

	mu       sync.RWMutex
	metrics  *domain.HeartbeatMetrics

	onFailover func(ctx context.Context, clusterID string, candidates []domain.ElectionCandidate) (*domain.ElectionResult, error)
}

// AgentConfig holds agent initialization parameters
type AgentConfig struct {
	NodeID            string
	ClusterID         string
	Role              domain.NodeRole
	ListenAddr        string
	HeartbeatInterval time.Duration
	FailureTimeout    time.Duration
	AISelector        AISelector
}

// NewAgent creates a new node agent
func NewAgent(cfg AgentConfig) *Agent {
	monitor := NewHeartbeatMonitor(cfg.FailureTimeout, cfg.HeartbeatInterval)
	election := NewElection(cfg.AISelector)

	a := &Agent{
		nodeID:            cfg.NodeID,
		clusterID:         cfg.ClusterID,
		role:              cfg.Role,
		listenAddr:        cfg.ListenAddr,
		heartbeatInterval: cfg.HeartbeatInterval,
		failureTimeout:    cfg.FailureTimeout,
		monitor:           monitor,
		election:          election,
	}

	a.onFailover = func(ctx context.Context, clusterID string, candidates []domain.ElectionCandidate) (*domain.ElectionResult, error) {
		return election.Elect(ctx, clusterID, candidates)
	}

	return a
}

// Run starts the agent main loop
func (a *Agent) Run(ctx context.Context) error {
	// Start HTTP server for heartbeat endpoints
	mux := http.NewServeMux()
	mux.HandleFunc("/heartbeat", a.handleHeartbeat)
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/metrics", a.handleMetrics)

	server := &http.Server{
		Addr:    a.listenAddr,
		Handler: mux,
	}

	// Start server in background
	go func() {
		log.Printf("Agent %s listening on %s (role: %s)", a.nodeID, a.listenAddr, a.role)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Agent server error: %v", err)
		}
	}()

	// Start heartbeat sender
	go a.sendHeartbeats(ctx)

	// Start failure detector (workers watch head)
	if a.role == domain.NodeRoleWorker {
		go a.watchHead(ctx)
	}

	// Wait for shutdown
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

// sendHeartbeats periodically sends heartbeat to peers
func (a *Agent) sendHeartbeats(ctx context.Context) {
	ticker := time.NewTicker(a.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.mu.RLock()
			hb := domain.Heartbeat{
				NodeID:    a.nodeID,
				ClusterID: a.clusterID,
				Role:      a.role,
				Timestamp: time.Now(),
				Metrics:   a.metrics,
			}
			a.mu.RUnlock()

			// Record own heartbeat
			a.monitor.RecordHeartbeat(hb)
		}
	}
}

// watchHead monitors the head node and triggers election if it fails
func (a *Agent) watchHead(ctx context.Context) {
	ticker := time.NewTicker(a.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			failedNodes := a.monitor.GetFailedNodes(a.clusterID)
			for _, node := range failedNodes {
				if node.Role == domain.NodeRoleHead {
					log.Printf("Head node %s failed! Initiating election...", node.NodeID)
					a.triggerElection(ctx)
					return // election will change roles
				}
			}
		}
	}
}

// triggerElection initiates a head election
func (a *Agent) triggerElection(ctx context.Context) {
	workers := a.monitor.GetHealthyWorkers(a.clusterID)
	if len(workers) == 0 {
		log.Printf("No healthy workers available for election")
		return
	}

	var candidates []domain.ElectionCandidate
	for _, w := range workers {
		c := domain.ElectionCandidate{
			NodeID: w.NodeID,
		}
		if w.LastMetrics != nil {
			c.GPUUtilization = w.LastMetrics.GPUUtilization
			c.MemoryFreeGB = w.LastMetrics.MemoryFreeGB
		}
		candidates = append(candidates, c)
	}

	result, err := a.onFailover(ctx, a.clusterID, candidates)
	if err != nil {
		log.Printf("Election failed: %v", err)
		return
	}

	log.Printf("Election result: new head = %s (reason: %s, AI: %v)",
		result.NewHeadID, result.Reason, result.AIDecision)

	// If this node is the new head, change role
	if result.NewHeadID == a.nodeID {
		a.mu.Lock()
		a.role = domain.NodeRoleHead
		a.mu.Unlock()
		log.Printf("This node (%s) is now the head!", a.nodeID)
	}
}

// UpdateMetrics updates the agent's current metrics
func (a *Agent) UpdateMetrics(m *domain.HeartbeatMetrics) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.metrics = m
}

// HTTP handlers

func (a *Agent) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var hb domain.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, "invalid heartbeat", http.StatusBadRequest)
		return
	}

	a.monitor.RecordHeartbeat(hb)
	w.WriteHeader(http.StatusOK)
}

func (a *Agent) handleHealth(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodeId":    a.nodeID,
		"clusterId": a.clusterID,
		"role":      a.role,
		"healthy":   true,
	})
}

func (a *Agent) handleMetrics(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	json.NewEncoder(w).Encode(a.metrics)
}
```

**Step 2: Write agent binary entrypoint**

`cmd/cluster-agent/main.go`:
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dave/clusterctl/internal/agent"
	"github.com/dave/clusterctl/internal/domain"
	"github.com/dave/clusterctl/internal/infra/ai"
)

func main() {
	nodeID := flag.String("node-id", "", "This node's ID")
	clusterID := flag.String("cluster-id", "", "Cluster ID")
	role := flag.String("role", "worker", "Node role (head or worker)")
	port := flag.Int("port", 9090, "Agent listen port")
	heartbeat := flag.Int("heartbeat", 3, "Heartbeat interval in seconds")
	timeout := flag.Int("timeout", 15, "Failure timeout in seconds")
	apiKey := flag.String("anthropic-key", os.Getenv("ANTHROPIC_API_KEY"), "Anthropic API key for AI selection")
	flag.Parse()

	if *nodeID == "" || *clusterID == "" {
		fmt.Fprintf(os.Stderr, "Usage: cluster-agent --node-id=ID --cluster-id=ID [--role=worker] [--port=9090]\n")
		os.Exit(1)
	}

	var nodeRole domain.NodeRole
	switch *role {
	case "head":
		nodeRole = domain.NodeRoleHead
	default:
		nodeRole = domain.NodeRoleWorker
	}

	var aiSelector agent.AISelector
	if *apiKey != "" {
		aiSelector = ai.NewClaudeSelector(*apiKey)
	}

	a := agent.NewAgent(agent.AgentConfig{
		NodeID:            *nodeID,
		ClusterID:         *clusterID,
		Role:              nodeRole,
		ListenAddr:        fmt.Sprintf(":%d", *port),
		HeartbeatInterval: time.Duration(*heartbeat) * time.Second,
		FailureTimeout:    time.Duration(*timeout) * time.Second,
		AISelector:        aiSelector,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down agent...")
		cancel()
	}()

	log.Printf("Starting cluster agent (node: %s, cluster: %s, role: %s)", *nodeID, *clusterID, *role)
	if err := a.Run(ctx); err != nil {
		log.Fatalf("Agent error: %v", err)
	}
}
```

**Step 3: Verify build**

Run: `go build ./cmd/cluster-agent/ && go build ./internal/agent/`
Expected: success

**Step 4: Commit**

```bash
git add internal/agent/agent.go cmd/cluster-agent/main.go
git commit -m "feat: add node agent with heartbeat, failure detection, and HTTP endpoints"
```

---

### Task 6: Systemd unit generation

**Files:**
- Create: `internal/agent/systemd.go`
- Test: `internal/agent/systemd_test.go`

**Step 1: Write test**

```go
package agent

import (
	"strings"
	"testing"
)

func TestGenerateSystemdUnit(t *testing.T) {
	cfg := SystemdConfig{
		NodeID:    "worker-1",
		ClusterID: "my-cluster",
		Role:      "worker",
		Port:      9090,
		BinaryPath: "/usr/local/bin/cluster-agent",
	}

	unit := GenerateSystemdUnit(cfg)

	if !strings.Contains(unit, "[Unit]") {
		t.Error("expected [Unit] section")
	}
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/cluster-agent") {
		t.Error("expected ExecStart with binary path")
	}
	if !strings.Contains(unit, "--node-id=worker-1") {
		t.Error("expected --node-id flag")
	}
	if !strings.Contains(unit, "--cluster-id=my-cluster") {
		t.Error("expected --cluster-id flag")
	}
	if !strings.Contains(unit, "Restart=always") {
		t.Error("expected Restart=always")
	}
}

func TestInstallCommand(t *testing.T) {
	cfg := SystemdConfig{
		NodeID:    "worker-1",
		ClusterID: "my-cluster",
		Role:      "worker",
		Port:      9090,
		BinaryPath: "/usr/local/bin/cluster-agent",
	}

	cmds := InstallCommands(cfg)
	if len(cmds) == 0 {
		t.Error("expected install commands")
	}

	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "systemctl daemon-reload") {
		t.Error("expected daemon-reload command")
	}
	if !strings.Contains(joined, "systemctl enable") {
		t.Error("expected enable command")
	}
}
```

**Step 2: Write implementation**

```go
package agent

import "fmt"

// SystemdConfig holds configuration for systemd unit generation
type SystemdConfig struct {
	NodeID     string
	ClusterID  string
	Role       string
	Port       int
	BinaryPath string
	APIKey     string // optional, for AI selection
}

// GenerateSystemdUnit generates a systemd unit file for the cluster agent
func GenerateSystemdUnit(cfg SystemdConfig) string {
	execStart := fmt.Sprintf("%s --node-id=%s --cluster-id=%s --role=%s --port=%d",
		cfg.BinaryPath, cfg.NodeID, cfg.ClusterID, cfg.Role, cfg.Port)

	if cfg.APIKey != "" {
		execStart += fmt.Sprintf(" --anthropic-key=%s", cfg.APIKey)
	}

	return fmt.Sprintf(`[Unit]
Description=Cluster Agent for %s (node: %s)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=cluster-agent

[Install]
WantedBy=multi-user.target
`, cfg.ClusterID, cfg.NodeID, execStart)
}

// ServiceName returns the systemd service name
func ServiceName(clusterID, nodeID string) string {
	return fmt.Sprintf("cluster-agent-%s-%s", clusterID, nodeID)
}

// UnitFilePath returns the path for the systemd unit file
func UnitFilePath(clusterID, nodeID string) string {
	return fmt.Sprintf("/etc/systemd/system/%s.service", ServiceName(clusterID, nodeID))
}

// InstallCommands returns the shell commands to install and start the agent
func InstallCommands(cfg SystemdConfig) []string {
	svcName := ServiceName(cfg.ClusterID, cfg.NodeID)
	unitPath := UnitFilePath(cfg.ClusterID, cfg.NodeID)

	return []string{
		fmt.Sprintf("sudo tee %s > /dev/null << 'EOF'\n%sEOF", unitPath, GenerateSystemdUnit(cfg)),
		"sudo systemctl daemon-reload",
		fmt.Sprintf("sudo systemctl enable %s", svcName),
		fmt.Sprintf("sudo systemctl start %s", svcName),
	}
}

// UninstallCommands returns the shell commands to stop and remove the agent
func UninstallCommands(clusterID, nodeID string) []string {
	svcName := ServiceName(clusterID, nodeID)
	unitPath := UnitFilePath(clusterID, nodeID)

	return []string{
		fmt.Sprintf("sudo systemctl stop %s", svcName),
		fmt.Sprintf("sudo systemctl disable %s", svcName),
		fmt.Sprintf("sudo rm %s", unitPath),
		"sudo systemctl daemon-reload",
	}
}
```

**Step 3: Run tests**

Run: `go test ./internal/agent/ -run TestGenerateSystemd -v && go test ./internal/agent/ -run TestInstallCommand -v`
Expected: all PASS

**Step 4: Commit**

```bash
git add internal/agent/systemd.go internal/agent/systemd_test.go
git commit -m "feat: add systemd unit generation for cluster agent"
```

---

### Task 7: CLI commands for agent management

**Files:**
- Modify: `internal/cli/cluster.go` — add `agent install`, `agent uninstall`, `agent status` subcommands

**Step 1: Add agent subcommand group**

Add to `newClusterCmd()`:
```go
cmd.AddCommand(newClusterAgentCmd())
```

```go
func newClusterAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage cluster node agents",
	}

	cmd.AddCommand(newAgentInstallCmd())
	cmd.AddCommand(newAgentUninstallCmd())
	cmd.AddCommand(newAgentStatusCmd())

	return cmd
}
```

`newAgentInstallCmd`: SSH into each cluster node, copy binary, write systemd unit, enable + start.
`newAgentUninstallCmd`: SSH into each node, stop + disable + remove service.
`newAgentStatusCmd`: SSH into each node, check `systemctl status cluster-agent-*`.

These use the existing `ssh.Executor` to run commands remotely.

**Step 2: Verify build**

Run: `go build ./cmd/clusterctl/`
Expected: success

**Step 3: Commit**

```bash
git add internal/cli/cluster.go
git commit -m "feat: add cluster agent install/uninstall/status CLI commands"
```

---

### Task 8: Integration — server-side failure monitoring

**Files:**
- Modify: `cmd/server/main.go` — add heartbeat monitoring goroutine
- Modify: `internal/web/handler/handler.go` — add failover API endpoints

**Step 1: Add failover endpoints**

Add to handler:
- `POST /api/clusters/:id/failover` — manually trigger failover
- `GET /api/clusters/:id/health` — get health status of all nodes

**Step 2: Add background monitoring to server**

In `cmd/server/main.go`, start a goroutine that:
1. Periodically checks node health via agent HTTP endpoints
2. If head fails, calls Claude API for selection
3. Executes failover using FailoverUseCase

**Step 3: Verify**

Run: `go build ./cmd/server/ && go build ./cmd/clusterctl/ && go test ./... -v`
Expected: all pass

**Step 4: Commit**

```bash
git add cmd/server/main.go internal/web/handler/handler.go
git commit -m "feat: add server-side health monitoring and failover endpoints"
```

---

### Task 9: End-to-end verification

**Step 1: Run all tests**

Run: `go test ./... -v`
Expected: all pass

**Step 2: Build all binaries**

Run: `go build ./cmd/clusterctl/ && go build ./cmd/cluster-agent/ && go build ./cmd/server/`
Expected: all succeed

**Step 3: Smoke test agent help**

Run: `./cluster-agent --help`
Expected: shows flags for node-id, cluster-id, role, port, etc.

**Step 4: Smoke test CLI**

Run: `./clusterctl cluster agent --help`
Expected: shows install, uninstall, status subcommands

**Step 5: Final commit**

```bash
git add -A
git commit -m "test: verify HA failover builds and passes all tests"
```
