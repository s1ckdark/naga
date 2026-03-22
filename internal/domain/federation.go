package domain

import (
	"fmt"
	"strings"
	"time"
)

// WorkerRef represents a reference to either a device or a sub-cluster.
// Format: "device:<id>" or "cluster:<id>"
type WorkerRef string

const (
	WorkerRefPrefixDevice  = "device:"
	WorkerRefPrefixCluster = "cluster:"
)

// NewDeviceRef creates a WorkerRef for a device
func NewDeviceRef(deviceID string) WorkerRef {
	return WorkerRef(WorkerRefPrefixDevice + deviceID)
}

// NewClusterRef creates a WorkerRef for a sub-cluster
func NewClusterRef(clusterID string) WorkerRef {
	return WorkerRef(WorkerRefPrefixCluster + clusterID)
}

// IsDevice returns true if this ref points to a device
func (r WorkerRef) IsDevice() bool {
	return strings.HasPrefix(string(r), WorkerRefPrefixDevice)
}

// IsCluster returns true if this ref points to a sub-cluster
func (r WorkerRef) IsCluster() bool {
	return strings.HasPrefix(string(r), WorkerRefPrefixCluster)
}

// ID returns the underlying device or cluster ID
func (r WorkerRef) ID() string {
	s := string(r)
	if i := strings.Index(s, ":"); i >= 0 {
		return s[i+1:]
	}
	return s // legacy: plain ID treated as device
}

// Type returns "device" or "cluster"
func (r WorkerRef) Type() string {
	if r.IsCluster() {
		return "cluster"
	}
	return "device"
}

// String returns the full ref string
func (r WorkerRef) String() string {
	return string(r)
}

// ParseWorkerRef parses a string into a WorkerRef.
// Plain IDs without prefix are treated as device refs for backward compatibility.
func ParseWorkerRef(s string) WorkerRef {
	if strings.HasPrefix(s, WorkerRefPrefixDevice) || strings.HasPrefix(s, WorkerRefPrefixCluster) {
		return WorkerRef(s)
	}
	// Legacy: plain ID → device
	return NewDeviceRef(s)
}

// ClusterGroup represents a federation of clusters
type ClusterGroup struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ClusterIDs  []string `json:"clusterIds"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// NewClusterGroup creates a new cluster group
func NewClusterGroup(name string, clusterIDs []string) *ClusterGroup {
	now := time.Now()
	return &ClusterGroup{
		Name:       name,
		ClusterIDs: clusterIDs,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// Validate checks the cluster group configuration
func (g *ClusterGroup) Validate() error {
	if g.Name == "" {
		return fmt.Errorf("cluster group name is required")
	}
	if len(g.ClusterIDs) == 0 {
		return fmt.Errorf("cluster group must contain at least one cluster")
	}
	return nil
}
