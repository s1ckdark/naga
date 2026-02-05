package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"github.com/dave/clusterctl/internal/repository"
)

// DB holds the SQLite database connection
type DB struct {
	db *sql.DB
}

// NewDB creates a new SQLite database connection
func NewDB(dsn string) (*DB, error) {
	// Expand ~ in path
	if strings.HasPrefix(dsn, "~") {
		home, _ := os.UserHomeDir()
		dsn = filepath.Join(home, dsn[1:])
	}

	// Ensure directory exists
	dir := filepath.Dir(dsn)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dsn+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite only supports one writer
	db.SetMaxIdleConns(1)

	sqliteDB := &DB{db: db}

	// Run migrations
	if err := sqliteDB.migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return sqliteDB, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	return d.db.Close()
}

// Repositories returns all repository implementations
func (d *DB) Repositories() *repository.Repositories {
	return &repository.Repositories{
		Devices:      NewDeviceRepository(d.db),
		Clusters:     NewClusterRepository(d.db),
		ClusterNodes: NewClusterNodeRepository(d.db),
		Metrics:      NewMetricsRepository(d.db),
		UnitOfWork:   d,
	}
}

// Begin starts a new transaction
func (d *DB) Begin(ctx context.Context) (repository.Transaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Transaction{tx: tx}, nil
}

// Transaction implements repository.Transaction
type Transaction struct {
	tx *sql.Tx
}

// Commit commits the transaction
func (t *Transaction) Commit() error {
	return t.tx.Commit()
}

// Rollback rolls back the transaction
func (t *Transaction) Rollback() error {
	return t.tx.Rollback()
}

// Devices returns the device repository for this transaction
func (t *Transaction) Devices() repository.DeviceRepository {
	return &DeviceRepository{db: t.tx}
}

// Clusters returns the cluster repository for this transaction
func (t *Transaction) Clusters() repository.ClusterRepository {
	return &ClusterRepository{db: t.tx}
}

// ClusterNodes returns the cluster node repository for this transaction
func (t *Transaction) ClusterNodes() repository.ClusterNodeRepository {
	return &ClusterNodeRepository{db: t.tx}
}

// Metrics returns the metrics repository for this transaction
func (t *Transaction) Metrics() repository.MetricsRepository {
	return &MetricsRepository{db: t.tx}
}

// migrate runs database migrations
func (d *DB) migrate() error {
	migrations := []string{
		// Devices table
		`CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			hostname TEXT,
			ip_addresses TEXT,
			tailscale_ip TEXT,
			os TEXT,
			status TEXT,
			is_external INTEGER DEFAULT 0,
			tags TEXT,
			user TEXT,
			last_seen DATETIME,
			created_at DATETIME,
			ssh_enabled INTEGER DEFAULT 0,
			ray_installed INTEGER DEFAULT 0,
			ray_version TEXT,
			python_version TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Clusters table
		`CREATE TABLE IF NOT EXISTS clusters (
			id TEXT PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			description TEXT,
			status TEXT NOT NULL,
			head_node_id TEXT NOT NULL,
			worker_ids TEXT,
			dashboard_url TEXT,
			ray_port INTEGER DEFAULT 6379,
			dashboard_port INTEGER DEFAULT 8265,
			object_store_memory INTEGER,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			started_at DATETIME,
			stopped_at DATETIME,
			last_error TEXT,
			last_error_at DATETIME
		)`,

		// Cluster nodes table
		`CREATE TABLE IF NOT EXISTS cluster_nodes (
			device_id TEXT NOT NULL,
			cluster_id TEXT NOT NULL,
			role TEXT NOT NULL,
			status TEXT NOT NULL,
			ray_address TEXT,
			num_cpus INTEGER,
			num_gpus INTEGER,
			memory_bytes INTEGER,
			joined_at DATETIME NOT NULL,
			left_at DATETIME,
			last_error TEXT,
			last_error_at DATETIME,
			PRIMARY KEY (device_id, cluster_id),
			FOREIGN KEY (cluster_id) REFERENCES clusters(id) ON DELETE CASCADE
		)`,

		// Metrics table
		`CREATE TABLE IF NOT EXISTS metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL,
			cpu_usage REAL,
			cpu_cores INTEGER,
			cpu_model TEXT,
			load_avg_1 REAL,
			load_avg_5 REAL,
			load_avg_15 REAL,
			mem_total INTEGER,
			mem_used INTEGER,
			mem_free INTEGER,
			mem_available INTEGER,
			mem_usage_percent REAL,
			swap_total INTEGER,
			swap_used INTEGER,
			disk_data TEXT,
			collected_at DATETIME NOT NULL,
			error TEXT
		)`,

		// Indexes
		`CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status)`,
		`CREATE INDEX IF NOT EXISTS idx_clusters_status ON clusters(status)`,
		`CREATE INDEX IF NOT EXISTS idx_clusters_name ON clusters(name)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_device_time ON metrics(device_id, collected_at DESC)`,
	}

	for _, migration := range migrations {
		if _, err := d.db.Exec(migration); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, migration)
		}
	}

	return nil
}

// dbExecutor interface for both *sql.DB and *sql.Tx
type dbExecutor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}
