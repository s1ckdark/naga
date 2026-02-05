package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

// ClusterNodeRepository implements repository.ClusterNodeRepository for SQLite
type ClusterNodeRepository struct {
	db dbExecutor
}

// NewClusterNodeRepository creates a new ClusterNodeRepository
func NewClusterNodeRepository(db dbExecutor) *ClusterNodeRepository {
	return &ClusterNodeRepository{db: db}
}

// Save creates or updates a cluster node
func (r *ClusterNodeRepository) Save(ctx context.Context, node *domain.ClusterNode) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (
			device_id, cluster_id, role, status, ray_address,
			num_cpus, num_gpus, memory_bytes, joined_at, left_at,
			last_error, last_error_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id, cluster_id) DO UPDATE SET
			role = excluded.role,
			status = excluded.status,
			ray_address = excluded.ray_address,
			num_cpus = excluded.num_cpus,
			num_gpus = excluded.num_gpus,
			memory_bytes = excluded.memory_bytes,
			left_at = excluded.left_at,
			last_error = excluded.last_error,
			last_error_at = excluded.last_error_at
	`,
		node.DeviceID, node.ClusterID, node.Role, node.Status,
		node.RayAddress, node.NumCPUs, node.NumGPUs, node.MemoryBytes,
		node.JoinedAt, node.LeftAt, node.LastError, node.LastErrorAt,
	)

	return err
}

// GetByDeviceAndCluster retrieves a node by device and cluster IDs
func (r *ClusterNodeRepository) GetByDeviceAndCluster(ctx context.Context, deviceID, clusterID string) (*domain.ClusterNode, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT device_id, cluster_id, role, status, ray_address,
			   num_cpus, num_gpus, memory_bytes, joined_at, left_at,
			   last_error, last_error_at
		FROM cluster_nodes WHERE device_id = ? AND cluster_id = ?
	`, deviceID, clusterID)

	return r.scanNode(row)
}

// GetByCluster retrieves all nodes for a cluster
func (r *ClusterNodeRepository) GetByCluster(ctx context.Context, clusterID string) ([]*domain.ClusterNode, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT device_id, cluster_id, role, status, ray_address,
			   num_cpus, num_gpus, memory_bytes, joined_at, left_at,
			   last_error, last_error_at
		FROM cluster_nodes WHERE cluster_id = ?
		ORDER BY role, joined_at
	`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanNodes(rows)
}

// Delete removes a node from a cluster
func (r *ClusterNodeRepository) Delete(ctx context.Context, deviceID, clusterID string) error {
	_, err := r.db.ExecContext(ctx,
		"DELETE FROM cluster_nodes WHERE device_id = ? AND cluster_id = ?",
		deviceID, clusterID)
	return err
}

func (r *ClusterNodeRepository) scanNode(row *sql.Row) (*domain.ClusterNode, error) {
	var n domain.ClusterNode
	var rayAddress, lastError sql.NullString
	var leftAt, lastErrorAt sql.NullTime

	err := row.Scan(
		&n.DeviceID, &n.ClusterID, &n.Role, &n.Status, &rayAddress,
		&n.NumCPUs, &n.NumGPUs, &n.MemoryBytes, &n.JoinedAt, &leftAt,
		&lastError, &lastErrorAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if rayAddress.Valid {
		n.RayAddress = rayAddress.String
	}
	if lastError.Valid {
		n.LastError = lastError.String
	}
	if leftAt.Valid {
		n.LeftAt = &leftAt.Time
	}
	if lastErrorAt.Valid {
		n.LastErrorAt = &lastErrorAt.Time
	}

	return &n, nil
}

func (r *ClusterNodeRepository) scanNodes(rows *sql.Rows) ([]*domain.ClusterNode, error) {
	var nodes []*domain.ClusterNode

	for rows.Next() {
		var n domain.ClusterNode
		var rayAddress, lastError sql.NullString
		var leftAt, lastErrorAt sql.NullTime

		err := rows.Scan(
			&n.DeviceID, &n.ClusterID, &n.Role, &n.Status, &rayAddress,
			&n.NumCPUs, &n.NumGPUs, &n.MemoryBytes, &n.JoinedAt, &leftAt,
			&lastError, &lastErrorAt,
		)
		if err != nil {
			return nil, err
		}

		if rayAddress.Valid {
			n.RayAddress = rayAddress.String
		}
		if lastError.Valid {
			n.LastError = lastError.String
		}
		if leftAt.Valid {
			n.LeftAt = &leftAt.Time
		}
		if lastErrorAt.Valid {
			n.LastErrorAt = &lastErrorAt.Time
		}

		nodes = append(nodes, &n)
	}

	return nodes, rows.Err()
}

// Ensure time package is used
var _ = time.Now
