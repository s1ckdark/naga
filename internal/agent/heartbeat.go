package agent

import (
	"log"
	"sync"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

type HeartbeatMonitor struct {
	timeout       time.Duration
	checkInterval time.Duration
	mu            sync.RWMutex
	nodeHealth    map[string]*domain.NodeHealth
	stopCleanup   chan struct{}
}

func NewHeartbeatMonitor(timeout, checkInterval time.Duration) *HeartbeatMonitor {
	m := &HeartbeatMonitor{
		timeout:       timeout,
		checkInterval: checkInterval,
		nodeHealth:    make(map[string]*domain.NodeHealth),
		stopCleanup:   make(chan struct{}),
	}
	go m.cleanupLoop()
	return m
}

// cleanupLoop periodically removes stale entries that haven't sent a heartbeat
// within 3x the timeout period.
func (m *HeartbeatMonitor) cleanupLoop() {
	ticker := time.NewTicker(m.timeout * 3)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.evictStale()
		case <-m.stopCleanup:
			return
		}
	}
}

func (m *HeartbeatMonitor) evictStale() {
	staleThreshold := m.timeout * 3
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, nh := range m.nodeHealth {
		if time.Since(nh.LastHeartbeat) > staleThreshold {
			log.Printf("evicting stale node %s (last heartbeat: %v ago)", id, time.Since(nh.LastHeartbeat))
			delete(m.nodeHealth, id)
		}
	}
}

// Stop stops the cleanup goroutine.
func (m *HeartbeatMonitor) Stop() {
	close(m.stopCleanup)
}

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

func (m *HeartbeatMonitor) IsNodeHealthy(nodeID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	nh, exists := m.nodeHealth[nodeID]
	if !exists {
		return false
	}
	return nh.IsHealthy(m.timeout)
}

func (m *HeartbeatMonitor) GetFailedNodes(clusterID string) []domain.NodeHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var failed []domain.NodeHealth
	for _, nh := range m.nodeHealth {
		if nh.ClusterID == clusterID && !nh.IsHealthy(m.timeout) {
			failed = append(failed, *nh)
		}
	}
	return failed
}

func (m *HeartbeatMonitor) GetHealthyWorkers(clusterID string) []domain.NodeHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var workers []domain.NodeHealth
	for _, nh := range m.nodeHealth {
		if nh.ClusterID == clusterID && nh.Role == domain.NodeRoleWorker && nh.IsHealthy(m.timeout) {
			workers = append(workers, *nh)
		}
	}
	return workers
}

func (m *HeartbeatMonitor) RemoveNode(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.nodeHealth, nodeID)
}
