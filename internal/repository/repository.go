package repository

import (
	"context"

	"github.com/dave/naga/internal/domain"
)

// DeviceRepository defines operations for device persistence
type DeviceRepository interface {
	// Save creates or updates a device
	Save(ctx context.Context, device *domain.Device) error

	// GetByID retrieves a device by its ID
	GetByID(ctx context.Context, id string) (*domain.Device, error)

	// GetAll retrieves all devices
	GetAll(ctx context.Context) ([]*domain.Device, error)

	// GetByFilter retrieves devices matching the filter
	GetByFilter(ctx context.Context, filter domain.DeviceFilter) ([]*domain.Device, error)

	// Delete removes a device by ID
	Delete(ctx context.Context, id string) error

	// SaveMany saves multiple devices
	SaveMany(ctx context.Context, devices []*domain.Device) error
}

// ClusterRepository defines operations for cluster persistence
type ClusterRepository interface {
	// Create creates a new cluster
	Create(ctx context.Context, cluster *domain.Cluster) error

	// Update updates an existing cluster
	Update(ctx context.Context, cluster *domain.Cluster) error

	// GetByID retrieves a cluster by its ID
	GetByID(ctx context.Context, id string) (*domain.Cluster, error)

	// GetByName retrieves a cluster by its name
	GetByName(ctx context.Context, name string) (*domain.Cluster, error)

	// GetAll retrieves all clusters
	GetAll(ctx context.Context) ([]*domain.Cluster, error)

	// GetByStatus retrieves clusters by status
	GetByStatus(ctx context.Context, status domain.ClusterStatus) ([]*domain.Cluster, error)

	// Delete removes a cluster by ID
	Delete(ctx context.Context, id string) error

	// GetClusterByDeviceID finds the cluster that contains a device
	GetClusterByDeviceID(ctx context.Context, deviceID string) (*domain.Cluster, error)
}

// ClusterNodeRepository defines operations for cluster node persistence
type ClusterNodeRepository interface {
	// Save creates or updates a cluster node
	Save(ctx context.Context, node *domain.ClusterNode) error

	// GetByDeviceAndCluster retrieves a node by device and cluster IDs
	GetByDeviceAndCluster(ctx context.Context, deviceID, clusterID string) (*domain.ClusterNode, error)

	// GetByCluster retrieves all nodes for a cluster
	GetByCluster(ctx context.Context, clusterID string) ([]*domain.ClusterNode, error)

	// Delete removes a node from a cluster
	Delete(ctx context.Context, deviceID, clusterID string) error
}

// MetricsRepository defines operations for metrics persistence
type MetricsRepository interface {
	// Save stores metrics for a device
	Save(ctx context.Context, metrics *domain.DeviceMetrics) error

	// GetLatest retrieves the latest metrics for a device
	GetLatest(ctx context.Context, deviceID string) (*domain.DeviceMetrics, error)

	// GetHistory retrieves historical metrics for a device
	GetHistory(ctx context.Context, deviceID string, limit int) (*domain.MetricsHistory, error)

	// GetSnapshot retrieves the latest metrics for all devices
	GetSnapshot(ctx context.Context) (*domain.MetricsSnapshot, error)

	// Cleanup removes old metrics data
	Cleanup(ctx context.Context, olderThanDays int) error
}

// UnitOfWork provides transactional support
type UnitOfWork interface {
	// Begin starts a new transaction
	Begin(ctx context.Context) (Transaction, error)
}

// Transaction represents a database transaction
type Transaction interface {
	// Commit commits the transaction
	Commit() error

	// Rollback rolls back the transaction
	Rollback() error

	// Devices returns the device repository for this transaction
	Devices() DeviceRepository

	// Clusters returns the cluster repository for this transaction
	Clusters() ClusterRepository

	// ClusterNodes returns the cluster node repository for this transaction
	ClusterNodes() ClusterNodeRepository

	// Metrics returns the metrics repository for this transaction
	Metrics() MetricsRepository
}

// Repositories provides access to all repositories
type Repositories struct {
	Devices      DeviceRepository
	Clusters     ClusterRepository
	ClusterNodes ClusterNodeRepository
	Metrics      MetricsRepository
	UnitOfWork   UnitOfWork
}
