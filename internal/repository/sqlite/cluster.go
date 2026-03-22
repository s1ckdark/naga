package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/dave/clusterctl/internal/domain"
)

// ClusterRepository implements repository.ClusterRepository for SQLite
type ClusterRepository struct {
	db dbExecutor
}

// NewClusterRepository creates a new ClusterRepository
func NewClusterRepository(db dbExecutor) *ClusterRepository {
	return &ClusterRepository{db: db}
}

// Create creates a new cluster
func (r *ClusterRepository) Create(ctx context.Context, cluster *domain.Cluster) error {
	if cluster.ID == "" {
		cluster.ID = uuid.New().String()
	}

	workerIDs, _ := json.Marshal(cluster.WorkerIDs)

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO clusters (
			id, name, description, mode, status, head_node_id, worker_ids,
			dashboard_url, ray_port, dashboard_port, object_store_memory,
			created_at, updated_at, started_at, stopped_at, last_error, last_error_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		cluster.ID, cluster.Name, cluster.Description, cluster.Mode, cluster.Status,
		cluster.HeadNodeID, string(workerIDs), cluster.DashboardURL,
		cluster.RayPort, cluster.DashboardPort, cluster.ObjectStoreMemory,
		cluster.CreatedAt, cluster.UpdatedAt, cluster.StartedAt, cluster.StoppedAt,
		cluster.LastError, cluster.LastErrorAt,
	)

	return err
}

// Update updates an existing cluster
func (r *ClusterRepository) Update(ctx context.Context, cluster *domain.Cluster) error {
	workerIDs, _ := json.Marshal(cluster.WorkerIDs)
	cluster.UpdatedAt = time.Now()

	result, err := r.db.ExecContext(ctx, `
		UPDATE clusters SET
			name = ?, description = ?, mode = ?, status = ?, head_node_id = ?,
			worker_ids = ?, dashboard_url = ?, ray_port = ?, dashboard_port = ?,
			object_store_memory = ?, updated_at = ?, started_at = ?, stopped_at = ?,
			last_error = ?, last_error_at = ?
		WHERE id = ?
	`,
		cluster.Name, cluster.Description, cluster.Mode, cluster.Status, cluster.HeadNodeID,
		string(workerIDs), cluster.DashboardURL, cluster.RayPort, cluster.DashboardPort,
		cluster.ObjectStoreMemory, cluster.UpdatedAt, cluster.StartedAt, cluster.StoppedAt,
		cluster.LastError, cluster.LastErrorAt, cluster.ID,
	)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return domain.ErrClusterNotFound
	}

	return nil
}

// GetByID retrieves a cluster by its ID
func (r *ClusterRepository) GetByID(ctx context.Context, id string) (*domain.Cluster, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, description, mode, status, head_node_id, worker_ids,
			   dashboard_url, ray_port, dashboard_port, object_store_memory,
			   created_at, updated_at, started_at, stopped_at, last_error, last_error_at
		FROM clusters WHERE id = ?
	`, id)

	return r.scanCluster(row)
}

// GetByName retrieves a cluster by its name
func (r *ClusterRepository) GetByName(ctx context.Context, name string) (*domain.Cluster, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, description, mode, status, head_node_id, worker_ids,
			   dashboard_url, ray_port, dashboard_port, object_store_memory,
			   created_at, updated_at, started_at, stopped_at, last_error, last_error_at
		FROM clusters WHERE name = ?
	`, name)

	return r.scanCluster(row)
}

// GetAll retrieves all clusters
func (r *ClusterRepository) GetAll(ctx context.Context) ([]*domain.Cluster, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, description, mode, status, head_node_id, worker_ids,
			   dashboard_url, ray_port, dashboard_port, object_store_memory,
			   created_at, updated_at, started_at, stopped_at, last_error, last_error_at
		FROM clusters ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanClusters(rows)
}

// GetByStatus retrieves clusters by status
func (r *ClusterRepository) GetByStatus(ctx context.Context, status domain.ClusterStatus) ([]*domain.Cluster, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, description, mode, status, head_node_id, worker_ids,
			   dashboard_url, ray_port, dashboard_port, object_store_memory,
			   created_at, updated_at, started_at, stopped_at, last_error, last_error_at
		FROM clusters WHERE status = ? ORDER BY created_at DESC
	`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanClusters(rows)
}

// Delete removes a cluster by ID
func (r *ClusterRepository) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM clusters WHERE id = ?", id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return domain.ErrClusterNotFound
	}

	return nil
}

// GetClusterByDeviceID finds the cluster that contains a device
func (r *ClusterRepository) GetClusterByDeviceID(ctx context.Context, deviceID string) (*domain.Cluster, error) {
	// Check head node first
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, description, mode, status, head_node_id, worker_ids,
			   dashboard_url, ray_port, dashboard_port, object_store_memory,
			   created_at, updated_at, started_at, stopped_at, last_error, last_error_at
		FROM clusters WHERE head_node_id = ?
	`, deviceID)

	cluster, err := r.scanCluster(row)
	if err == nil {
		return cluster, nil
	}

	// Check worker IDs (JSON array search)
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, description, mode, status, head_node_id, worker_ids,
			   dashboard_url, ray_port, dashboard_port, object_store_memory,
			   created_at, updated_at, started_at, stopped_at, last_error, last_error_at
		FROM clusters
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		c, err := r.scanClusterFromRows(rows)
		if err != nil {
			continue
		}

		for _, wid := range c.WorkerIDs {
			if wid == deviceID {
				return c, nil
			}
		}
	}

	return nil, domain.ErrClusterNotFound
}

func (r *ClusterRepository) scanCluster(row *sql.Row) (*domain.Cluster, error) {
	var c domain.Cluster
	var workerIDsJSON string
	var description, mode, dashboardURL, lastError sql.NullString
	var startedAt, stoppedAt, lastErrorAt sql.NullTime

	err := row.Scan(
		&c.ID, &c.Name, &description, &mode, &c.Status, &c.HeadNodeID, &workerIDsJSON,
		&dashboardURL, &c.RayPort, &c.DashboardPort, &c.ObjectStoreMemory,
		&c.CreatedAt, &c.UpdatedAt, &startedAt, &stoppedAt, &lastError, &lastErrorAt,
	)
	if err == sql.ErrNoRows {
		return nil, domain.ErrClusterNotFound
	}
	if err != nil {
		return nil, err
	}

	if description.Valid {
		c.Description = description.String
	}
	if mode.Valid && mode.String != "" {
		c.Mode = domain.ClusterMode(mode.String)
	} else {
		c.Mode = domain.ClusterModeBasic
	}
	if dashboardURL.Valid {
		c.DashboardURL = dashboardURL.String
	}
	if lastError.Valid {
		c.LastError = lastError.String
	}
	if startedAt.Valid {
		c.StartedAt = &startedAt.Time
	}
	if stoppedAt.Valid {
		c.StoppedAt = &stoppedAt.Time
	}
	if lastErrorAt.Valid {
		c.LastErrorAt = &lastErrorAt.Time
	}

	json.Unmarshal([]byte(workerIDsJSON), &c.WorkerIDs)
	if c.WorkerIDs == nil {
		c.WorkerIDs = []string{}
	}

	return &c, nil
}

func (r *ClusterRepository) scanClusterFromRows(rows *sql.Rows) (*domain.Cluster, error) {
	var c domain.Cluster
	var workerIDsJSON string
	var description, mode, dashboardURL, lastError sql.NullString
	var startedAt, stoppedAt, lastErrorAt sql.NullTime

	err := rows.Scan(
		&c.ID, &c.Name, &description, &mode, &c.Status, &c.HeadNodeID, &workerIDsJSON,
		&dashboardURL, &c.RayPort, &c.DashboardPort, &c.ObjectStoreMemory,
		&c.CreatedAt, &c.UpdatedAt, &startedAt, &stoppedAt, &lastError, &lastErrorAt,
	)
	if err != nil {
		return nil, err
	}

	if description.Valid {
		c.Description = description.String
	}
	if mode.Valid && mode.String != "" {
		c.Mode = domain.ClusterMode(mode.String)
	} else {
		c.Mode = domain.ClusterModeBasic
	}
	if dashboardURL.Valid {
		c.DashboardURL = dashboardURL.String
	}
	if lastError.Valid {
		c.LastError = lastError.String
	}
	if startedAt.Valid {
		c.StartedAt = &startedAt.Time
	}
	if stoppedAt.Valid {
		c.StoppedAt = &stoppedAt.Time
	}
	if lastErrorAt.Valid {
		c.LastErrorAt = &lastErrorAt.Time
	}

	json.Unmarshal([]byte(workerIDsJSON), &c.WorkerIDs)
	if c.WorkerIDs == nil {
		c.WorkerIDs = []string{}
	}

	return &c, nil
}

func (r *ClusterRepository) scanClusters(rows *sql.Rows) ([]*domain.Cluster, error) {
	var clusters []*domain.Cluster

	for rows.Next() {
		c, err := r.scanClusterFromRows(rows)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, c)
	}

	return clusters, rows.Err()
}

