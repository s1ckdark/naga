package domain

import (
	"errors"
	"time"
)

// ClusterMode represents the execution mode of a cluster
type ClusterMode string

const (
	ClusterModeBasic ClusterMode = "basic" // SSH-based direct execution
	ClusterModeRay   ClusterMode = "ray"   // Ray cluster orchestration
)

// ClusterStatus represents the current status of a cluster
type ClusterStatus string

const (
	ClusterStatusPending  ClusterStatus = "pending"
	ClusterStatusStarting ClusterStatus = "starting"
	ClusterStatusRunning  ClusterStatus = "running"
	ClusterStatusStopping ClusterStatus = "stopping"
	ClusterStatusStopped  ClusterStatus = "stopped"
	ClusterStatusError    ClusterStatus = "error"
)

// HeadTransfer records a head node change event
type HeadTransfer struct {
	FromDeviceID string    `json:"fromDeviceId"`
	ToDeviceID   string    `json:"toDeviceId"`
	Reason       string    `json:"reason"` // "manual", "failover", "election"
	Timestamp    time.Time `json:"timestamp"`
}

// Cluster errors
var (
	ErrClusterNotFound     = errors.New("cluster not found")
	ErrClusterAlreadyExist = errors.New("cluster already exists")
	ErrClusterInUse        = errors.New("cluster is currently in use")
	ErrHeadNodeRequired    = errors.New("head node is required")
	ErrNodeAlreadyInCluster = errors.New("node is already in a cluster")
	ErrNodeNotInCluster    = errors.New("node is not in this cluster")
	ErrCannotRemoveHead    = errors.New("cannot remove head node, change head first")
)

// Cluster represents a cluster configuration
type Cluster struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Mode        ClusterMode   `json:"mode"`
	Status      ClusterStatus `json:"status"`
	HeadNodeID  string        `json:"headNodeId"`
	WorkerIDs   []string      `json:"workerIds"`
	DashboardURL string       `json:"dashboardUrl"`

	// Ray configuration (only used when Mode == "ray")
	RayPort         int    `json:"rayPort,omitempty"`
	DashboardPort   int    `json:"dashboardPort,omitempty"`
	ObjectStoreMemory int64 `json:"objectStoreMemory,omitempty"` // bytes

	// Metadata
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	StoppedAt *time.Time `json:"stoppedAt,omitempty"`

	// Error tracking
	LastError     string    `json:"lastError,omitempty"`
	LastErrorAt   *time.Time `json:"lastErrorAt,omitempty"`

	// Head transfer history
	HeadHistory []HeadTransfer `json:"headHistory,omitempty"`
}

// NewCluster creates a new cluster with default settings
func NewCluster(name, headNodeID string, workerIDs []string) *Cluster {
	return NewClusterWithMode(name, headNodeID, workerIDs, ClusterModeBasic)
}

// NewClusterWithMode creates a new cluster with the specified mode
func NewClusterWithMode(name, headNodeID string, workerIDs []string, mode ClusterMode) *Cluster {
	now := time.Now()
	c := &Cluster{
		Name:       name,
		Mode:       mode,
		Status:     ClusterStatusPending,
		HeadNodeID: headNodeID,
		WorkerIDs:  workerIDs,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if mode == ClusterModeRay {
		c.RayPort = 6379
		c.DashboardPort = 8265
	}
	return c
}

// IsRayMode returns true if the cluster uses Ray orchestration
func (c *Cluster) IsRayMode() bool {
	return c.Mode == ClusterModeRay
}

// TotalNodes returns the total number of nodes (head + workers)
func (c *Cluster) TotalNodes() int {
	return 1 + len(c.WorkerIDs)
}

// AllNodeIDs returns all node IDs including head and workers
func (c *Cluster) AllNodeIDs() []string {
	ids := make([]string, 0, c.TotalNodes())
	ids = append(ids, c.HeadNodeID)
	ids = append(ids, c.WorkerIDs...)
	return ids
}

// WorkerRefs returns WorkerIDs parsed as WorkerRef values
func (c *Cluster) WorkerRefs() []WorkerRef {
	refs := make([]WorkerRef, len(c.WorkerIDs))
	for i, id := range c.WorkerIDs {
		refs[i] = ParseWorkerRef(id)
	}
	return refs
}

// DeviceWorkerIDs returns only the device IDs from workers (excludes sub-clusters)
func (c *Cluster) DeviceWorkerIDs() []string {
	var ids []string
	for _, id := range c.WorkerIDs {
		ref := ParseWorkerRef(id)
		if ref.IsDevice() {
			ids = append(ids, ref.ID())
		}
	}
	return ids
}

// ClusterWorkerIDs returns only the sub-cluster IDs from workers
func (c *Cluster) ClusterWorkerIDs() []string {
	var ids []string
	for _, id := range c.WorkerIDs {
		ref := ParseWorkerRef(id)
		if ref.IsCluster() {
			ids = append(ids, ref.ID())
		}
	}
	return ids
}

// HasWorker checks if a device is a worker in this cluster
func (c *Cluster) HasWorker(deviceID string) bool {
	for _, id := range c.WorkerIDs {
		ref := ParseWorkerRef(id)
		if ref.ID() == deviceID {
			return true
		}
	}
	return false
}

// IsRunning returns true if the cluster is in running state
func (c *Cluster) IsRunning() bool {
	return c.Status == ClusterStatusRunning
}

// CanModify returns true if the cluster can be modified
func (c *Cluster) CanModify() bool {
	return c.Status == ClusterStatusPending ||
		   c.Status == ClusterStatusStopped ||
		   c.Status == ClusterStatusRunning
}

// AddWorker adds a worker node to the cluster
func (c *Cluster) AddWorker(deviceID string) error {
	if deviceID == c.HeadNodeID {
		return errors.New("cannot add head node as worker")
	}
	if c.HasWorker(deviceID) {
		return ErrNodeAlreadyInCluster
	}
	c.WorkerIDs = append(c.WorkerIDs, deviceID)
	c.UpdatedAt = time.Now()
	return nil
}

// RemoveWorker removes a worker node from the cluster
func (c *Cluster) RemoveWorker(deviceID string) error {
	if deviceID == c.HeadNodeID {
		return ErrCannotRemoveHead
	}

	for i, id := range c.WorkerIDs {
		if id == deviceID {
			c.WorkerIDs = append(c.WorkerIDs[:i], c.WorkerIDs[i+1:]...)
			c.UpdatedAt = time.Now()
			return nil
		}
	}
	return ErrNodeNotInCluster
}

// ChangeHead changes the head node of the cluster.
// The old head becomes a worker, and the new head is removed from workers if present.
// reason should be one of "manual", "failover", or "election".
func (c *Cluster) ChangeHead(newHeadID string, reason string) error {
	if newHeadID == c.HeadNodeID {
		return nil // No change needed
	}

	oldHeadID := c.HeadNodeID

	// Remove new head from workers if present
	for i, id := range c.WorkerIDs {
		if id == newHeadID {
			c.WorkerIDs = append(c.WorkerIDs[:i], c.WorkerIDs[i+1:]...)
			break
		}
	}

	// Add old head to workers
	c.WorkerIDs = append(c.WorkerIDs, oldHeadID)

	// Set new head
	c.HeadNodeID = newHeadID
	now := time.Now()
	c.UpdatedAt = now

	// Record transfer history
	c.HeadHistory = append(c.HeadHistory, HeadTransfer{
		FromDeviceID: oldHeadID,
		ToDeviceID:   newHeadID,
		Reason:       reason,
		Timestamp:    now,
	})

	return nil
}

// SetError sets an error state on the cluster
func (c *Cluster) SetError(err string) {
	now := time.Now()
	c.Status = ClusterStatusError
	c.LastError = err
	c.LastErrorAt = &now
	c.UpdatedAt = now
}
