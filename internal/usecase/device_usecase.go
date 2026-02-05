package usecase

import (
	"context"
	"sync"
	"time"

	"github.com/dave/clusterctl/internal/domain"
	"github.com/dave/clusterctl/internal/repository"
)

// DeviceUseCase handles device-related business logic
type DeviceUseCase struct {
	repos          *repository.Repositories
	tailscale      TailscaleClient
	sshCollector   MetricsCollector
	cacheTTL       time.Duration
	cachedDevices  []*domain.Device
	cacheTime      time.Time
	cacheMu        sync.RWMutex
}

// TailscaleClient interface for Tailscale API operations
type TailscaleClient interface {
	ListDevices(ctx context.Context) ([]*domain.Device, error)
	GetDevice(ctx context.Context, nameOrID string) (*domain.Device, error)
	GetDeviceByID(ctx context.Context, id string) (*domain.Device, error)
}

// MetricsCollector interface for collecting metrics from devices
type MetricsCollector interface {
	CollectMetrics(ctx context.Context, device *domain.Device) (*domain.DeviceMetrics, error)
	CollectMetricsParallel(ctx context.Context, devices []*domain.Device) ([]*domain.DeviceMetrics, error)
}

// NewDeviceUseCase creates a new DeviceUseCase
func NewDeviceUseCase(repos *repository.Repositories, tailscale TailscaleClient, sshCollector MetricsCollector) *DeviceUseCase {
	return &DeviceUseCase{
		repos:        repos,
		tailscale:    tailscale,
		sshCollector: sshCollector,
		cacheTTL:     1 * time.Minute,
	}
}

// ListDevices returns all devices, using cache if available
func (uc *DeviceUseCase) ListDevices(ctx context.Context, forceRefresh bool) ([]*domain.Device, error) {
	if !forceRefresh {
		uc.cacheMu.RLock()
		if time.Since(uc.cacheTime) < uc.cacheTTL && len(uc.cachedDevices) > 0 {
			devices := uc.cachedDevices
			uc.cacheMu.RUnlock()
			return devices, nil
		}
		uc.cacheMu.RUnlock()
	}

	// Fetch from Tailscale API
	devices, err := uc.tailscale.ListDevices(ctx)
	if err != nil {
		return nil, err
	}

	// Update cache
	uc.cacheMu.Lock()
	uc.cachedDevices = devices
	uc.cacheTime = time.Now()
	uc.cacheMu.Unlock()

	// Save to repository for persistence
	if uc.repos != nil && uc.repos.Devices != nil {
		go uc.repos.Devices.SaveMany(context.Background(), devices)
	}

	return devices, nil
}

// GetDevice returns a specific device
func (uc *DeviceUseCase) GetDevice(ctx context.Context, nameOrID string) (*domain.Device, error) {
	return uc.tailscale.GetDevice(ctx, nameOrID)
}

// GetDeviceWithMetrics returns a device with its current metrics
func (uc *DeviceUseCase) GetDeviceWithMetrics(ctx context.Context, nameOrID string) (*domain.Device, *domain.DeviceMetrics, error) {
	device, err := uc.tailscale.GetDevice(ctx, nameOrID)
	if err != nil {
		return nil, nil, err
	}

	if !device.CanSSH() {
		return device, nil, nil
	}

	metrics, err := uc.sshCollector.CollectMetrics(ctx, device)
	if err != nil {
		// Return device without metrics on error
		return device, nil, nil
	}

	return device, metrics, nil
}

// GetAllDevicesWithMetrics returns all devices with their metrics
func (uc *DeviceUseCase) GetAllDevicesWithMetrics(ctx context.Context) ([]*domain.Device, []*domain.DeviceMetrics, error) {
	devices, err := uc.ListDevices(ctx, false)
	if err != nil {
		return nil, nil, err
	}

	// Filter to online devices with SSH
	var sshDevices []*domain.Device
	for _, d := range devices {
		if d.CanSSH() {
			sshDevices = append(sshDevices, d)
		}
	}

	if len(sshDevices) == 0 {
		return devices, nil, nil
	}

	metrics, err := uc.sshCollector.CollectMetricsParallel(ctx, sshDevices)
	if err != nil {
		return devices, nil, nil
	}

	return devices, metrics, nil
}

// FilterDevices returns devices matching the filter
func (uc *DeviceUseCase) FilterDevices(ctx context.Context, filter domain.DeviceFilter) ([]*domain.Device, error) {
	devices, err := uc.ListDevices(ctx, false)
	if err != nil {
		return nil, err
	}

	var result []*domain.Device
	for _, d := range devices {
		if matchesFilter(d, filter) {
			result = append(result, d)
		}
	}

	return result, nil
}

// GetDeviceMap returns a map of device ID to device
func (uc *DeviceUseCase) GetDeviceMap(ctx context.Context) (map[string]*domain.Device, error) {
	devices, err := uc.ListDevices(ctx, false)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*domain.Device, len(devices))
	for _, d := range devices {
		result[d.ID] = d
	}

	return result, nil
}

// CheckRayStatus checks Ray installation status on a device
func (uc *DeviceUseCase) CheckRayStatus(ctx context.Context, device *domain.Device) (*domain.Device, error) {
	// This would be implemented with SSH to check Ray installation
	// For now, return the device as-is
	return device, nil
}

func matchesFilter(d *domain.Device, filter domain.DeviceFilter) bool {
	if filter.Status != nil && d.Status != *filter.Status {
		return false
	}

	if filter.OS != "" && d.OS != filter.OS {
		return false
	}

	if filter.RayInstalled != nil && d.RayInstalled != *filter.RayInstalled {
		return false
	}

	if filter.SSHEnabled != nil && d.SSHEnabled != *filter.SSHEnabled {
		return false
	}

	if filter.HasTag != "" {
		hasTag := false
		for _, t := range d.Tags {
			if t == filter.HasTag {
				hasTag = true
				break
			}
		}
		if !hasTag {
			return false
		}
	}

	return true
}
