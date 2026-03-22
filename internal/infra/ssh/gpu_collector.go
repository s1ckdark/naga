package ssh

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

const nvidiaSmiCommand = "nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits"

// GPUCollector collects GPU metrics from devices via SSH
type GPUCollector struct {
	executor      *Executor
	maxConcurrent int
}

// NewGPUCollector creates a new GPU metrics collector
func NewGPUCollector(executor *Executor) *GPUCollector {
	return &GPUCollector{
		executor:      executor,
		maxConcurrent: 10,
	}
}

// CollectGPUMetrics collects GPU metrics from a single device.
// Returns metrics with Error field set if nvidia-smi fails (graceful degradation).
func (c *GPUCollector) CollectGPUMetrics(ctx context.Context, device *domain.Device) *domain.GPUNodeMetrics {
	metrics := &domain.GPUNodeMetrics{
		NodeName:    device.GetDisplayName(),
		DeviceID:    device.ID,
		CollectedAt: time.Now(),
	}

	output, err := c.executor.Execute(ctx, device, nvidiaSmiCommand)
	if err != nil {
		metrics.Error = err.Error()
		return metrics
	}

	gpus, err := domain.ParseNvidiaSmiOutput(strings.TrimSpace(output))
	if err != nil {
		metrics.Error = err.Error()
		return metrics
	}

	metrics.GPUs = gpus
	return metrics
}

// CollectClusterGPUMetrics collects GPU metrics from multiple devices in parallel.
// Uses semaphore pattern like existing CollectMetricsParallel in collector.go.
func (c *GPUCollector) CollectClusterGPUMetrics(ctx context.Context, devices []*domain.Device) []*domain.GPUNodeMetrics {
	results := make([]*domain.GPUNodeMetrics, len(devices))
	var wg sync.WaitGroup

	sem := make(chan struct{}, c.maxConcurrent)

	for i, device := range devices {
		wg.Add(1)
		go func(idx int, d *domain.Device) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			results[idx] = c.CollectGPUMetrics(ctx, d)
		}(i, device)
	}

	wg.Wait()
	return results
}
