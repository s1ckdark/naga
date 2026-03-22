package usecase

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/dave/clusterctl/internal/domain"
	"github.com/dave/clusterctl/internal/repository"
)

// GPUChecker checks GPU availability on a device
type GPUChecker interface {
	CollectGPUMetrics(ctx context.Context, device *domain.Device) *domain.GPUNodeMetrics
}

// DeviceUseCase handles device-related business logic
type DeviceUseCase struct {
	repos         *repository.Repositories
	tailscale     TailscaleClient
	sshCollector  MetricsCollector
	gpuChecker    GPUChecker
	cacheTTL      time.Duration
	cachedDevices []*domain.Device
	cacheTime     time.Time
	cacheMu       sync.RWMutex
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

// SetGPUChecker sets the GPU checker (optional dependency)
func (uc *DeviceUseCase) SetGPUChecker(checker GPUChecker) {
	uc.gpuChecker = checker
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

	// Merge GPU info from DB (Tailscale API doesn't know about GPUs)
	if uc.repos != nil && uc.repos.Devices != nil {
		uc.mergeGPUFromDB(ctx, devices)
	}

	// Update cache immediately (before GPU probe) so requests aren't blocked
	uc.cacheMu.Lock()
	uc.cachedDevices = devices
	uc.cacheTime = time.Now()
	uc.cacheMu.Unlock()

	// Save to repository for persistence
	if uc.repos != nil && uc.repos.Devices != nil {
		_ = uc.repos.Devices.SaveMany(ctx, devices)
	}

	// Check GPU on candidates in the background (non-blocking)
	if uc.gpuChecker != nil {
		go func(devs []*domain.Device) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			uc.probeGPU(bgCtx, devs)

			// Update cache and DB with GPU results
			uc.cacheMu.Lock()
			uc.cachedDevices = devs
			uc.cacheMu.Unlock()

			if uc.repos != nil && uc.repos.Devices != nil {
				_ = uc.repos.Devices.SaveMany(bgCtx, devs)
			}
		}(devices)
	}

	return devices, nil
}

// mergeGPUFromDB restores GPU info from DB into freshly fetched Tailscale devices.
func (uc *DeviceUseCase) mergeGPUFromDB(ctx context.Context, devices []*domain.Device) {
	dbDevices, err := uc.repos.Devices.GetAll(ctx)
	if err != nil {
		return
	}
	dbMap := make(map[string]*domain.Device, len(dbDevices))
	for _, d := range dbDevices {
		dbMap[d.ID] = d
	}
	for _, d := range devices {
		if saved, ok := dbMap[d.ID]; ok && saved.GPUModel != "" {
			d.HasGPU = saved.HasGPU
			d.GPUModel = saved.GPUModel
			d.GPUCount = saved.GPUCount
		}
	}
}

// probeGPU checks GPU availability on candidate devices (Linux+SSH) that haven't been probed yet.
func (uc *DeviceUseCase) probeGPU(ctx context.Context, devices []*domain.Device) {
	var candidates []*domain.Device
	for _, d := range devices {
		// Only probe if not yet checked (GPUModel empty) and is a candidate
		if d.IsGPUCandidate() && d.GPUModel == "" {
			candidates = append(candidates, d)
		}
	}
	if len(candidates) == 0 {
		return
	}

	log.Printf("Probing GPU on %d candidate devices...", len(candidates))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // max 5 concurrent SSH sessions

	for _, d := range candidates {
		wg.Add(1)
		go func(dev *domain.Device) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			metrics := uc.gpuChecker.CollectGPUMetrics(probeCtx, dev)
			if metrics.Error != "" {
				log.Printf("GPU probe failed on %s: %s", dev.GetDisplayName(), metrics.Error)
				// Don't save "none" on error — leave empty so we retry next time
				return
			}
			if metrics.HasGPU() {
				dev.HasGPU = true
				dev.GPUCount = len(metrics.GPUs)
				dev.GPUModel = metrics.GPUs[0].Name
				log.Printf("GPU found on %s: %dx %s", dev.GetDisplayName(), dev.GPUCount, dev.GPUModel)
			} else {
				dev.HasGPU = false
				dev.GPUModel = "none"
				dev.GPUCount = 0
				log.Printf("No GPU on %s", dev.GetDisplayName())
			}
		}(d)
	}
	wg.Wait()
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
