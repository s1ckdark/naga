-- Initial schema for clusterctl

-- Devices table: caches Tailscale device information
CREATE TABLE IF NOT EXISTS devices (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    hostname TEXT,
    tailscale_ip TEXT,
    os TEXT,
    status TEXT DEFAULT 'unknown',
    ssh_enabled INTEGER DEFAULT 0,
    ray_installed INTEGER DEFAULT 0,
    ray_version TEXT,
    tags TEXT,  -- JSON array
    last_seen TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_devices_name ON devices(name);
CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status);

-- Clusters table: Ray cluster configurations
CREATE TABLE IF NOT EXISTS clusters (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    description TEXT,
    status TEXT DEFAULT 'created',
    head_node_id TEXT NOT NULL,
    worker_ids TEXT,  -- JSON array
    dashboard_url TEXT,
    ray_port INTEGER DEFAULT 6379,
    dashboard_port INTEGER DEFAULT 8265,
    object_store_memory INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    stopped_at TIMESTAMP,
    last_error TEXT,
    last_error_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_clusters_name ON clusters(name);
CREATE INDEX IF NOT EXISTS idx_clusters_status ON clusters(status);
CREATE INDEX IF NOT EXISTS idx_clusters_head_node ON clusters(head_node_id);

-- Cluster nodes table: detailed node information for clusters
CREATE TABLE IF NOT EXISTS cluster_nodes (
    id TEXT PRIMARY KEY,
    cluster_id TEXT NOT NULL,
    device_id TEXT NOT NULL,
    role TEXT NOT NULL,  -- 'head' or 'worker'
    status TEXT DEFAULT 'pending',
    ray_pid INTEGER,
    joined_at TIMESTAMP,
    left_at TIMESTAMP,
    last_error TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cluster_id) REFERENCES clusters(id) ON DELETE CASCADE,
    UNIQUE(cluster_id, device_id)
);

CREATE INDEX IF NOT EXISTS idx_cluster_nodes_cluster ON cluster_nodes(cluster_id);
CREATE INDEX IF NOT EXISTS idx_cluster_nodes_device ON cluster_nodes(device_id);

-- Metrics table: device resource metrics history
CREATE TABLE IF NOT EXISTS metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL,
    cpu_usage_percent REAL,
    cpu_cores INTEGER,
    memory_total_bytes INTEGER,
    memory_used_bytes INTEGER,
    memory_usage_percent REAL,
    disk_total_bytes INTEGER,
    disk_used_bytes INTEGER,
    disk_usage_percent REAL,
    gpu_name TEXT,
    gpu_usage_percent REAL,
    gpu_memory_total_bytes INTEGER,
    gpu_memory_used_bytes INTEGER,
    network_rx_bytes INTEGER,
    network_tx_bytes INTEGER,
    collected_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_metrics_device ON metrics(device_id);
CREATE INDEX IF NOT EXISTS idx_metrics_collected_at ON metrics(collected_at);

-- Cleanup old metrics (keep 30 days)
-- This can be run periodically via a cron job or application task
-- DELETE FROM metrics WHERE collected_at < datetime('now', '-30 days');
