package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

// DeviceRepository implements repository.DeviceRepository for SQLite
type DeviceRepository struct {
	db dbExecutor
}

// NewDeviceRepository creates a new DeviceRepository
func NewDeviceRepository(db dbExecutor) *DeviceRepository {
	return &DeviceRepository{db: db}
}

// Save creates or updates a device
func (r *DeviceRepository) Save(ctx context.Context, device *domain.Device) error {
	ipAddresses, _ := json.Marshal(device.IPAddresses)
	tags, _ := json.Marshal(device.Tags)

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO devices (
			id, name, hostname, ip_addresses, tailscale_ip, os, status,
			is_external, tags, user, last_seen, created_at, ssh_enabled,
			ray_installed, ray_version, python_version, has_gpu, gpu_model, gpu_count, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			hostname = excluded.hostname,
			ip_addresses = excluded.ip_addresses,
			tailscale_ip = excluded.tailscale_ip,
			os = excluded.os,
			status = excluded.status,
			is_external = excluded.is_external,
			tags = excluded.tags,
			user = excluded.user,
			last_seen = excluded.last_seen,
			ssh_enabled = excluded.ssh_enabled,
			ray_installed = excluded.ray_installed,
			ray_version = excluded.ray_version,
			python_version = excluded.python_version,
			has_gpu = excluded.has_gpu,
			gpu_model = excluded.gpu_model,
			gpu_count = excluded.gpu_count,
			updated_at = excluded.updated_at
	`,
		device.ID, device.Name, device.Hostname, string(ipAddresses),
		device.TailscaleIP, device.OS, device.Status, device.IsExternal,
		string(tags), device.User, device.LastSeen, device.CreatedAt,
		device.SSHEnabled, device.RayInstalled, device.RayVersion,
		device.PythonVersion, device.HasGPU, device.GPUModel, device.GPUCount,
		time.Now(),
	)

	return err
}

// GetByID retrieves a device by its ID
func (r *DeviceRepository) GetByID(ctx context.Context, id string) (*domain.Device, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, hostname, ip_addresses, tailscale_ip, os, status,
			   is_external, tags, user, last_seen, created_at, ssh_enabled,
			   ray_installed, ray_version, python_version,
			   has_gpu, gpu_model, gpu_count
		FROM devices WHERE id = ?
	`, id)

	return r.scanDevice(row)
}

// GetAll retrieves all devices
func (r *DeviceRepository) GetAll(ctx context.Context) ([]*domain.Device, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, hostname, ip_addresses, tailscale_ip, os, status,
			   is_external, tags, user, last_seen, created_at, ssh_enabled,
			   ray_installed, ray_version, python_version,
			   has_gpu, gpu_model, gpu_count
		FROM devices ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanDevices(rows)
}

// GetByFilter retrieves devices matching the filter
func (r *DeviceRepository) GetByFilter(ctx context.Context, filter domain.DeviceFilter) ([]*domain.Device, error) {
	query := `
		SELECT id, name, hostname, ip_addresses, tailscale_ip, os, status,
			   is_external, tags, user, last_seen, created_at, ssh_enabled,
			   ray_installed, ray_version, python_version,
			   has_gpu, gpu_model, gpu_count
		FROM devices WHERE 1=1
	`
	args := []interface{}{}

	if filter.Status != nil {
		query += " AND status = ?"
		args = append(args, *filter.Status)
	}

	if filter.OS != "" {
		query += " AND os = ?"
		args = append(args, filter.OS)
	}

	if filter.RayInstalled != nil {
		query += " AND ray_installed = ?"
		args = append(args, *filter.RayInstalled)
	}

	if filter.SSHEnabled != nil {
		query += " AND ssh_enabled = ?"
		args = append(args, *filter.SSHEnabled)
	}

	query += " ORDER BY name"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanDevices(rows)
}

// Delete removes a device by ID
func (r *DeviceRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM devices WHERE id = ?", id)
	return err
}

// SaveMany saves multiple devices
func (r *DeviceRepository) SaveMany(ctx context.Context, devices []*domain.Device) error {
	for _, device := range devices {
		if err := r.Save(ctx, device); err != nil {
			return err
		}
	}
	return nil
}

func (r *DeviceRepository) scanDevice(row *sql.Row) (*domain.Device, error) {
	var d domain.Device
	var ipAddressesJSON, tagsJSON string
	var rayVersion, pythonVersion sql.NullString
	var lastSeen, createdAt sql.NullTime

	var gpuModel sql.NullString

	err := row.Scan(
		&d.ID, &d.Name, &d.Hostname, &ipAddressesJSON, &d.TailscaleIP,
		&d.OS, &d.Status, &d.IsExternal, &tagsJSON, &d.User,
		&lastSeen, &createdAt, &d.SSHEnabled, &d.RayInstalled,
		&rayVersion, &pythonVersion,
		&d.HasGPU, &gpuModel, &d.GPUCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(ipAddressesJSON), &d.IPAddresses)
	json.Unmarshal([]byte(tagsJSON), &d.Tags)

	if rayVersion.Valid {
		d.RayVersion = rayVersion.String
	}
	if pythonVersion.Valid {
		d.PythonVersion = pythonVersion.String
	}
	if lastSeen.Valid {
		d.LastSeen = lastSeen.Time
	}
	if createdAt.Valid {
		d.CreatedAt = createdAt.Time
	}
	if gpuModel.Valid {
		d.GPUModel = gpuModel.String
	}

	return &d, nil
}

func (r *DeviceRepository) scanDevices(rows *sql.Rows) ([]*domain.Device, error) {
	var devices []*domain.Device

	for rows.Next() {
		var d domain.Device
		var ipAddressesJSON, tagsJSON string
		var rayVersion, pythonVersion sql.NullString
		var lastSeen, createdAt sql.NullTime

		var gpuModel sql.NullString

		err := rows.Scan(
			&d.ID, &d.Name, &d.Hostname, &ipAddressesJSON, &d.TailscaleIP,
			&d.OS, &d.Status, &d.IsExternal, &tagsJSON, &d.User,
			&lastSeen, &createdAt, &d.SSHEnabled, &d.RayInstalled,
			&rayVersion, &pythonVersion,
			&d.HasGPU, &gpuModel, &d.GPUCount,
		)
		if err != nil {
			return nil, err
		}

		json.Unmarshal([]byte(ipAddressesJSON), &d.IPAddresses)
		json.Unmarshal([]byte(tagsJSON), &d.Tags)

		if rayVersion.Valid {
			d.RayVersion = rayVersion.String
		}
		if pythonVersion.Valid {
			d.PythonVersion = pythonVersion.String
		}
		if lastSeen.Valid {
			d.LastSeen = lastSeen.Time
		}
		if createdAt.Valid {
			d.CreatedAt = createdAt.Time
		}
		if gpuModel.Valid {
			d.GPUModel = gpuModel.String
		}

		devices = append(devices, &d)
	}

	return devices, rows.Err()
}
