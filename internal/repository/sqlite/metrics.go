package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

// MetricsRepository implements repository.MetricsRepository for SQLite
type MetricsRepository struct {
	db dbExecutor
}

// NewMetricsRepository creates a new MetricsRepository
func NewMetricsRepository(db dbExecutor) *MetricsRepository {
	return &MetricsRepository{db: db}
}

// Save stores metrics for a device
func (r *MetricsRepository) Save(ctx context.Context, metrics *domain.DeviceMetrics) error {
	diskData, _ := json.Marshal(metrics.Disk)

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO metrics (
			device_id, cpu_usage, cpu_cores, cpu_model, load_avg_1, load_avg_5, load_avg_15,
			mem_total, mem_used, mem_free, mem_available, mem_usage_percent,
			swap_total, swap_used, disk_data, collected_at, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		metrics.DeviceID, metrics.CPU.UsagePercent, metrics.CPU.Cores, metrics.CPU.ModelName,
		metrics.CPU.LoadAvg1, metrics.CPU.LoadAvg5, metrics.CPU.LoadAvg15,
		metrics.Memory.Total, metrics.Memory.Used, metrics.Memory.Free,
		metrics.Memory.Available, metrics.Memory.UsagePercent,
		metrics.Memory.SwapTotal, metrics.Memory.SwapUsed,
		string(diskData), metrics.CollectedAt, metrics.Error,
	)

	return err
}

// GetLatest retrieves the latest metrics for a device
func (r *MetricsRepository) GetLatest(ctx context.Context, deviceID string) (*domain.DeviceMetrics, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT device_id, cpu_usage, cpu_cores, cpu_model, load_avg_1, load_avg_5, load_avg_15,
			   mem_total, mem_used, mem_free, mem_available, mem_usage_percent,
			   swap_total, swap_used, disk_data, collected_at, error
		FROM metrics WHERE device_id = ?
		ORDER BY collected_at DESC LIMIT 1
	`, deviceID)

	return r.scanMetrics(row)
}

// GetHistory retrieves historical metrics for a device
func (r *MetricsRepository) GetHistory(ctx context.Context, deviceID string, limit int) (*domain.MetricsHistory, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT device_id, cpu_usage, cpu_cores, cpu_model, load_avg_1, load_avg_5, load_avg_15,
			   mem_total, mem_used, mem_free, mem_available, mem_usage_percent,
			   swap_total, swap_used, disk_data, collected_at, error
		FROM metrics WHERE device_id = ?
		ORDER BY collected_at DESC LIMIT ?
	`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	history := &domain.MetricsHistory{
		DeviceID: deviceID,
		Points:   []domain.DeviceMetrics{},
	}

	for rows.Next() {
		m, err := r.scanMetricsFromRows(rows)
		if err != nil {
			return nil, err
		}
		history.Points = append(history.Points, *m)
	}

	return history, rows.Err()
}

// GetSnapshot retrieves the latest metrics for all devices
func (r *MetricsRepository) GetSnapshot(ctx context.Context) (*domain.MetricsSnapshot, error) {
	// Get distinct device IDs with their latest metrics
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.device_id, m.cpu_usage, m.cpu_cores, m.cpu_model,
			   m.load_avg_1, m.load_avg_5, m.load_avg_15,
			   m.mem_total, m.mem_used, m.mem_free, m.mem_available, m.mem_usage_percent,
			   m.swap_total, m.swap_used, m.disk_data, m.collected_at, m.error
		FROM metrics m
		INNER JOIN (
			SELECT device_id, MAX(collected_at) as max_time
			FROM metrics
			GROUP BY device_id
		) latest ON m.device_id = latest.device_id AND m.collected_at = latest.max_time
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snapshot := &domain.MetricsSnapshot{
		Devices:     make(map[string]*domain.DeviceMetrics),
		CollectedAt: time.Now(),
	}

	for rows.Next() {
		m, err := r.scanMetricsFromRows(rows)
		if err != nil {
			return nil, err
		}
		snapshot.Devices[m.DeviceID] = m
	}

	return snapshot, rows.Err()
}

// Cleanup removes old metrics data
func (r *MetricsRepository) Cleanup(ctx context.Context, olderThanDays int) error {
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	_, err := r.db.ExecContext(ctx, "DELETE FROM metrics WHERE collected_at < ?", cutoff)
	return err
}

func (r *MetricsRepository) scanMetrics(row *sql.Row) (*domain.DeviceMetrics, error) {
	var m domain.DeviceMetrics
	var diskDataJSON string
	var errorMsg sql.NullString

	err := row.Scan(
		&m.DeviceID, &m.CPU.UsagePercent, &m.CPU.Cores, &m.CPU.ModelName,
		&m.CPU.LoadAvg1, &m.CPU.LoadAvg5, &m.CPU.LoadAvg15,
		&m.Memory.Total, &m.Memory.Used, &m.Memory.Free,
		&m.Memory.Available, &m.Memory.UsagePercent,
		&m.Memory.SwapTotal, &m.Memory.SwapUsed,
		&diskDataJSON, &m.CollectedAt, &errorMsg,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(diskDataJSON), &m.Disk)
	if errorMsg.Valid {
		m.Error = errorMsg.String
	}

	return &m, nil
}

func (r *MetricsRepository) scanMetricsFromRows(rows *sql.Rows) (*domain.DeviceMetrics, error) {
	var m domain.DeviceMetrics
	var diskDataJSON string
	var errorMsg sql.NullString

	err := rows.Scan(
		&m.DeviceID, &m.CPU.UsagePercent, &m.CPU.Cores, &m.CPU.ModelName,
		&m.CPU.LoadAvg1, &m.CPU.LoadAvg5, &m.CPU.LoadAvg15,
		&m.Memory.Total, &m.Memory.Used, &m.Memory.Free,
		&m.Memory.Available, &m.Memory.UsagePercent,
		&m.Memory.SwapTotal, &m.Memory.SwapUsed,
		&diskDataJSON, &m.CollectedAt, &errorMsg,
	)
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(diskDataJSON), &m.Disk)
	if errorMsg.Valid {
		m.Error = errorMsg.String
	}

	return &m, nil
}
