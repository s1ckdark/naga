package usecase

import (
	"context"
	"time"

	"github.com/dave/clusterctl/internal/domain"
	"github.com/dave/clusterctl/internal/repository"
)

// MonitorUseCase handles monitoring-related business logic
type MonitorUseCase struct {
	repos        *repository.Repositories
	collector    MetricsCollector
	deviceUC     *DeviceUseCase
}

// NewMonitorUseCase creates a new MonitorUseCase
func NewMonitorUseCase(repos *repository.Repositories, collector MetricsCollector, deviceUC *DeviceUseCase) *MonitorUseCase {
	return &MonitorUseCase{
		repos:     repos,
		collector: collector,
		deviceUC:  deviceUC,
	}
}

// GetDeviceMetrics gets the current metrics for a device
func (uc *MonitorUseCase) GetDeviceMetrics(ctx context.Context, deviceNameOrID string) (*domain.DeviceMetrics, error) {
	device, err := uc.deviceUC.GetDevice(ctx, deviceNameOrID)
	if err != nil {
		return nil, err
	}

	if !device.CanSSH() {
		return &domain.DeviceMetrics{
			DeviceID:    device.ID,
			CollectedAt: time.Now(),
			Error:       "device is offline or SSH is disabled",
		}, nil
	}

	return uc.collector.CollectMetrics(ctx, device)
}

// GetAllMetrics gets metrics for all online devices
func (uc *MonitorUseCase) GetAllMetrics(ctx context.Context) (*domain.MetricsSnapshot, error) {
	devices, err := uc.deviceUC.ListDevices(ctx, false)
	if err != nil {
		return nil, err
	}

	// Filter to online devices with SSH
	var sshDevices []*domain.Device
	for _, d := range devices {
		if d.CanSSH() {
			sshDevices = append(sshDevices, d)
		}
	}

	snapshot := &domain.MetricsSnapshot{
		Devices:     make(map[string]*domain.DeviceMetrics),
		CollectedAt: time.Now(),
	}

	if len(sshDevices) == 0 {
		return snapshot, nil
	}

	metrics, err := uc.collector.CollectMetricsParallel(ctx, sshDevices)
	if err != nil {
		return nil, err
	}

	for _, m := range metrics {
		snapshot.Devices[m.DeviceID] = m
	}

	return snapshot, nil
}

// GetClusterMetrics gets metrics for all nodes in a cluster
func (uc *MonitorUseCase) GetClusterMetrics(ctx context.Context, cluster *domain.Cluster) (*domain.MetricsSnapshot, error) {
	deviceMap, err := uc.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return nil, err
	}

	// Get devices for this cluster
	var clusterDevices []*domain.Device
	for _, nodeID := range cluster.AllNodeIDs() {
		if device, ok := deviceMap[nodeID]; ok && device.CanSSH() {
			clusterDevices = append(clusterDevices, device)
		}
	}

	snapshot := &domain.MetricsSnapshot{
		Devices:     make(map[string]*domain.DeviceMetrics),
		CollectedAt: time.Now(),
	}

	if len(clusterDevices) == 0 {
		return snapshot, nil
	}

	metrics, err := uc.collector.CollectMetricsParallel(ctx, clusterDevices)
	if err != nil {
		return nil, err
	}

	for _, m := range metrics {
		snapshot.Devices[m.DeviceID] = m
	}

	return snapshot, nil
}

// SaveMetrics saves metrics to the repository
func (uc *MonitorUseCase) SaveMetrics(ctx context.Context, metrics *domain.DeviceMetrics) error {
	if uc.repos == nil || uc.repos.Metrics == nil {
		return nil
	}
	return uc.repos.Metrics.Save(ctx, metrics)
}

// GetMetricsHistory gets historical metrics for a device
func (uc *MonitorUseCase) GetMetricsHistory(ctx context.Context, deviceID string, limit int) (*domain.MetricsHistory, error) {
	if uc.repos == nil || uc.repos.Metrics == nil {
		return &domain.MetricsHistory{DeviceID: deviceID}, nil
	}
	return uc.repos.Metrics.GetHistory(ctx, deviceID, limit)
}

// StartBackgroundCollection starts background metrics collection
func (uc *MonitorUseCase) StartBackgroundCollection(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snapshot, err := uc.GetAllMetrics(ctx)
			if err != nil {
				continue
			}

			// Save all metrics
			for _, m := range snapshot.Devices {
				uc.SaveMetrics(ctx, m)
			}
		}
	}
}
