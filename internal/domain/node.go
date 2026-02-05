package domain

import "time"

// NodeRole represents the role of a node in a Ray cluster
type NodeRole string

const (
	NodeRoleHead   NodeRole = "head"
	NodeRoleWorker NodeRole = "worker"
)

// NodeStatus represents the current status of a node in a cluster
type NodeStatus string

const (
	NodeStatusPending     NodeStatus = "pending"
	NodeStatusStarting    NodeStatus = "starting"
	NodeStatusRunning     NodeStatus = "running"
	NodeStatusStopping    NodeStatus = "stopping"
	NodeStatusStopped     NodeStatus = "stopped"
	NodeStatusError       NodeStatus = "error"
	NodeStatusUnreachable NodeStatus = "unreachable"
)

// ClusterNode represents a node's membership and status in a cluster
type ClusterNode struct {
	DeviceID    string     `json:"deviceId"`
	ClusterID   string     `json:"clusterId"`
	Role        NodeRole   `json:"role"`
	Status      NodeStatus `json:"status"`
	RayAddress  string     `json:"rayAddress"`  // e.g., "100.64.0.1:6379"
	NumCPUs     int        `json:"numCpus"`
	NumGPUs     int        `json:"numGpus"`
	MemoryBytes int64      `json:"memoryBytes"`

	// Join information
	JoinedAt   time.Time  `json:"joinedAt"`
	LeftAt     *time.Time `json:"leftAt,omitempty"`

	// Error tracking
	LastError   string     `json:"lastError,omitempty"`
	LastErrorAt *time.Time `json:"lastErrorAt,omitempty"`
}

// IsHead returns true if this node is the head node
func (n *ClusterNode) IsHead() bool {
	return n.Role == NodeRoleHead
}

// IsWorker returns true if this node is a worker node
func (n *ClusterNode) IsWorker() bool {
	return n.Role == NodeRoleWorker
}

// IsRunning returns true if this node is running
func (n *ClusterNode) IsRunning() bool {
	return n.Status == NodeStatusRunning
}

// IsHealthy returns true if the node is in a healthy state
func (n *ClusterNode) IsHealthy() bool {
	return n.Status == NodeStatusRunning
}

// SetError sets an error state on the node
func (n *ClusterNode) SetError(err string) {
	now := time.Now()
	n.Status = NodeStatusError
	n.LastError = err
	n.LastErrorAt = &now
}

// RayNodeInfo represents information about a Ray node from the Ray API
type RayNodeInfo struct {
	NodeID          string  `json:"nodeId"`
	NodeIP          string  `json:"nodeIp"`
	IsHeadNode      bool    `json:"isHeadNode"`
	State           string  `json:"state"`
	NodeName        string  `json:"nodeName"`
	ResourcesTotal  map[string]float64 `json:"resourcesTotal"`
	ResourcesAvail  map[string]float64 `json:"resourcesAvailable"`
}

// RayClusterInfo represents overall Ray cluster information
type RayClusterInfo struct {
	GCSAddress    string         `json:"gcsAddress"`
	DashboardURL  string         `json:"dashboardUrl"`
	PythonVersion string         `json:"pythonVersion"`
	RayVersion    string         `json:"rayVersion"`
	Nodes         []RayNodeInfo  `json:"nodes"`

	// Aggregate stats
	TotalCPUs     float64 `json:"totalCpus"`
	AvailCPUs     float64 `json:"availCpus"`
	TotalMemory   int64   `json:"totalMemory"`
	AvailMemory   int64   `json:"availMemory"`
	TotalGPUs     float64 `json:"totalGpus"`
	AvailGPUs     float64 `json:"availGpus"`

	// Job information
	ActiveJobs    int `json:"activeJobs"`
	PendingJobs   int `json:"pendingJobs"`
	CompletedJobs int `json:"completedJobs"`
}
