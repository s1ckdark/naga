package agent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dave/naga/internal/domain"
)

// Agent runs on each cluster node as a systemd service.
// It provides HTTP endpoints for heartbeat, health, and metrics,
// sends periodic heartbeats, and watches for head node failure.
type Agent struct {
	nodeID            string
	clusterID         string
	role              domain.NodeRole
	listenAddr        string
	authToken         string
	heartbeatInterval time.Duration
	failureTimeout    time.Duration
	monitor           *HeartbeatMonitor
	election          *Election
	mu                sync.RWMutex
	metrics           *domain.HeartbeatMetrics
	onFailover        func(ctx context.Context, clusterID string, candidates []domain.ElectionCandidate) (*domain.ElectionResult, error)
}

// AgentConfig holds configuration for creating a new Agent.
type AgentConfig struct {
	NodeID            string
	ClusterID         string
	Role              domain.NodeRole
	ListenAddr        string
	AuthToken         string
	HeartbeatInterval time.Duration
	FailureTimeout    time.Duration
	HeadSelector      HeadSelector
}

// NewAgent creates a new Agent with the given configuration.
func NewAgent(cfg AgentConfig) *Agent {
	monitor := NewHeartbeatMonitor(cfg.FailureTimeout, cfg.HeartbeatInterval)
	election := NewElection(cfg.HeadSelector)

	a := &Agent{
		nodeID:            cfg.NodeID,
		clusterID:         cfg.ClusterID,
		role:              cfg.Role,
		listenAddr:        cfg.ListenAddr,
		authToken:         cfg.AuthToken,
		heartbeatInterval: cfg.HeartbeatInterval,
		failureTimeout:    cfg.FailureTimeout,
		monitor:           monitor,
		election:          election,
		metrics:           &domain.HeartbeatMetrics{},
	}

	a.onFailover = func(ctx context.Context, clusterID string, candidates []domain.ElectionCandidate) (*domain.ElectionResult, error) {
		return election.Elect(ctx, clusterID, candidates)
	}

	return a
}

// UpdateMetrics updates the agent's current metrics snapshot.
func (a *Agent) UpdateMetrics(m *domain.HeartbeatMetrics) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.metrics = m
}

// Run starts the HTTP server, heartbeat sender, and (for workers) head watcher.
// It blocks until ctx is cancelled, then performs graceful shutdown.
func (a *Agent) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/heartbeat", a.handleHeartbeat)
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/metrics", a.handleMetrics)

	srv := &http.Server{
		Addr:    a.listenAddr,
		Handler: mux,
	}

	errCh := make(chan error, 1)

	// Start HTTP server
	go func() {
		log.Printf("agent %s starting HTTP server on %s (role=%s, cluster=%s)",
			a.nodeID, a.listenAddr, a.role, a.clusterID)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http server error: %w", err)
		}
	}()

	// Start heartbeat sender
	go a.sendHeartbeats(ctx)

	// Workers watch the head node for failures
	if a.role == domain.NodeRoleWorker {
		go a.watchHead(ctx)
	}

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		log.Printf("agent %s shutting down", a.nodeID)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// sendHeartbeats periodically records this node's heartbeat with the monitor.
func (a *Agent) sendHeartbeats(ctx context.Context) {
	ticker := time.NewTicker(a.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.mu.RLock()
			metrics := a.metrics
			a.mu.RUnlock()

			hb := domain.Heartbeat{
				NodeID:    a.nodeID,
				ClusterID: a.clusterID,
				Role:      a.role,
				Timestamp: time.Now(),
				Metrics:   metrics,
			}
			a.monitor.RecordHeartbeat(hb)
		}
	}
}

// watchHead monitors the head node and triggers an election if it fails.
func (a *Agent) watchHead(ctx context.Context) {
	ticker := time.NewTicker(a.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			failed := a.monitor.GetFailedNodes(a.clusterID)
			for _, nh := range failed {
				if nh.Role == domain.NodeRoleHead {
					log.Printf("agent %s detected head node %s failure",
						a.nodeID, nh.NodeID)

					// Deduplication: only the worker with the lowest ID triggers the election
					workers := a.monitor.GetHealthyWorkers(a.clusterID)
					if len(workers) == 0 {
						continue
					}

					workerIDs := make([]string, 0, len(workers))
					for _, w := range workers {
						workerIDs = append(workerIDs, w.NodeID)
					}
					sort.Strings(workerIDs)

					if workerIDs[0] != a.nodeID {
						log.Printf("agent %s deferring election to %s", a.nodeID, workerIDs[0])
						continue
					}

					log.Printf("agent %s triggering election as lowest-ID worker", a.nodeID)
					candidates := make([]domain.ElectionCandidate, 0, len(workers))
					for _, w := range workers {
						c := domain.ElectionCandidate{
							NodeID: w.NodeID,
						}
						if w.LastMetrics != nil {
							c.GPUUtilization = w.LastMetrics.GPUUtilization
							c.MemoryFreeGB = w.LastMetrics.MemoryFreeGB
							c.RunningJobs = w.LastMetrics.RunningJobs
						}
						candidates = append(candidates, c)
					}
					if len(candidates) > 0 {
						result, err := a.onFailover(ctx, a.clusterID, candidates)
						if err != nil {
							log.Printf("election failed: %v", err)
						} else {
							log.Printf("election result: new head=%s reason=%s",
								result.NewHeadID, result.Reason)
						}
					}
				}
			}
		}
	}
}

// handleHeartbeat handles POST /heartbeat requests.
func (a *Agent) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Authenticate request using Bearer token
	if !a.authenticateRequest(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Limit request body size to prevent abuse
	r.Body = http.MaxBytesReader(w, r.Body, 4096)

	var hb domain.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	a.monitor.RecordHeartbeat(hb)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// authenticateRequest checks the Bearer token using constant-time comparison.
// Returns true if no token is configured or the token matches.
func (a *Agent) authenticateRequest(r *http.Request) bool {
	if a.authToken == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	token := auth[len(prefix):]
	return subtle.ConstantTimeCompare([]byte(token), []byte(a.authToken)) == 1
}

// handleHealth handles GET /health requests.
func (a *Agent) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !a.authenticateRequest(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodeId":    a.nodeID,
		"clusterId": a.clusterID,
		"role":      a.role,
		"status":    "healthy",
	})
}

// handleMetrics handles GET /metrics requests.
func (a *Agent) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !a.authenticateRequest(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	a.mu.RLock()
	metrics := a.metrics
	a.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}
