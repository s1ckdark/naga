package ssh

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dave/clusterctl/internal/domain"
)

// Collector collects metrics from devices via SSH
type Collector struct {
	executor      *Executor
	maxConcurrent int
}

// NewCollector creates a new metrics collector
func NewCollector(executor *Executor) *Collector {
	return &Collector{
		executor:      executor,
		maxConcurrent: 10,
	}
}

// CollectMetrics collects metrics from a single device
func (c *Collector) CollectMetrics(ctx context.Context, device *domain.Device) (*domain.DeviceMetrics, error) {
	metrics := &domain.DeviceMetrics{
		DeviceID:    device.ID,
		CollectedAt: time.Now(),
	}

	// Collect all metrics in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []string

	// CPU metrics
	wg.Add(1)
	go func() {
		defer wg.Done()
		cpu, err := c.collectCPU(ctx, device)
		mu.Lock()
		if err != nil {
			errs = append(errs, fmt.Sprintf("cpu: %v", err))
		} else {
			metrics.CPU = *cpu
		}
		mu.Unlock()
	}()

	// Memory metrics
	wg.Add(1)
	go func() {
		defer wg.Done()
		mem, err := c.collectMemory(ctx, device)
		mu.Lock()
		if err != nil {
			errs = append(errs, fmt.Sprintf("memory: %v", err))
		} else {
			metrics.Memory = *mem
		}
		mu.Unlock()
	}()

	// Disk metrics
	wg.Add(1)
	go func() {
		defer wg.Done()
		disk, err := c.collectDisk(ctx, device)
		mu.Lock()
		if err != nil {
			errs = append(errs, fmt.Sprintf("disk: %v", err))
		} else {
			metrics.Disk = *disk
		}
		mu.Unlock()
	}()

	wg.Wait()

	if len(errs) > 0 {
		metrics.Error = strings.Join(errs, "; ")
	}

	return metrics, nil
}

// CollectMetricsParallel collects metrics from multiple devices in parallel
func (c *Collector) CollectMetricsParallel(ctx context.Context, devices []*domain.Device) ([]*domain.DeviceMetrics, error) {
	results := make([]*domain.DeviceMetrics, len(devices))
	var wg sync.WaitGroup

	sem := make(chan struct{}, c.maxConcurrent)

	for i, device := range devices {
		wg.Add(1)
		go func(idx int, d *domain.Device) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			metrics, _ := c.CollectMetrics(ctx, d)
			results[idx] = metrics
		}(i, device)
	}

	wg.Wait()
	return results, nil
}

// collectCPU collects CPU metrics
func (c *Collector) collectCPU(ctx context.Context, device *domain.Device) (*domain.CPUMetrics, error) {
	cpu := &domain.CPUMetrics{}

	// Get load average
	output, err := c.executor.Execute(ctx, device, "cat /proc/loadavg 2>/dev/null || sysctl -n vm.loadavg 2>/dev/null")
	if err == nil {
		parts := strings.Fields(output)
		if len(parts) >= 3 {
			cpu.LoadAvg1, _ = strconv.ParseFloat(parts[0], 64)
			cpu.LoadAvg5, _ = strconv.ParseFloat(parts[1], 64)
			cpu.LoadAvg15, _ = strconv.ParseFloat(parts[2], 64)
		}
	}

	// Get CPU count
	output, err = c.executor.Execute(ctx, device, "nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null")
	if err == nil {
		cpu.Cores, _ = strconv.Atoi(strings.TrimSpace(output))
	}

	// Get CPU model
	output, err = c.executor.Execute(ctx, device, "cat /proc/cpuinfo 2>/dev/null | grep 'model name' | head -1 | cut -d: -f2 || sysctl -n machdep.cpu.brand_string 2>/dev/null")
	if err == nil {
		cpu.ModelName = strings.TrimSpace(output)
	}

	// Calculate usage percent (rough estimate from load average)
	if cpu.Cores > 0 {
		cpu.UsagePercent = (cpu.LoadAvg1 / float64(cpu.Cores)) * 100
		if cpu.UsagePercent > 100 {
			cpu.UsagePercent = 100
		}
	}

	return cpu, nil
}

// collectMemory collects memory metrics
func (c *Collector) collectMemory(ctx context.Context, device *domain.Device) (*domain.MemoryMetrics, error) {
	mem := &domain.MemoryMetrics{}

	// Try Linux first, then macOS
	output, err := c.executor.Execute(ctx, device, "cat /proc/meminfo 2>/dev/null")
	if err == nil && strings.Contains(output, "MemTotal") {
		// Parse Linux /proc/meminfo
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}

			value, _ := strconv.ParseUint(parts[1], 10, 64)
			value *= 1024 // Convert from KB to bytes

			switch {
			case strings.HasPrefix(line, "MemTotal:"):
				mem.Total = value
			case strings.HasPrefix(line, "MemFree:"):
				mem.Free = value
			case strings.HasPrefix(line, "MemAvailable:"):
				mem.Available = value
			case strings.HasPrefix(line, "SwapTotal:"):
				mem.SwapTotal = value
			case strings.HasPrefix(line, "SwapFree:"):
				mem.SwapFree = value
			}
		}

		mem.Used = mem.Total - mem.Available
		mem.SwapUsed = mem.SwapTotal - mem.SwapFree
	} else {
		// Try macOS
		output, err = c.executor.Execute(ctx, device, "vm_stat && sysctl hw.memsize")
		if err == nil {
			// Parse macOS vm_stat (simplified)
			lines := strings.Split(output, "\n")
			var pageSize uint64 = 4096

			for _, line := range lines {
				if strings.Contains(line, "hw.memsize:") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						mem.Total, _ = strconv.ParseUint(parts[1], 10, 64)
					}
				}
				if strings.Contains(line, "Pages free:") {
					parts := strings.Fields(line)
					if len(parts) >= 3 {
						pages, _ := strconv.ParseUint(strings.TrimSuffix(parts[2], "."), 10, 64)
						mem.Free = pages * pageSize
					}
				}
			}

			mem.Used = mem.Total - mem.Free
			mem.Available = mem.Free
		}
	}

	if mem.Total > 0 {
		mem.UsagePercent = float64(mem.Used) / float64(mem.Total) * 100
	}

	return mem, nil
}

// collectDisk collects disk metrics
func (c *Collector) collectDisk(ctx context.Context, device *domain.Device) (*domain.DiskMetrics, error) {
	disk := &domain.DiskMetrics{
		Partitions: []domain.PartitionMetrics{},
	}

	output, err := c.executor.Execute(ctx, device, "df -B1 2>/dev/null || df -k")
	if err != nil {
		return disk, err
	}

	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if i == 0 { // Skip header
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 6 {
			continue
		}

		// Skip non-physical filesystems
		device := parts[0]
		if strings.HasPrefix(device, "tmpfs") ||
			strings.HasPrefix(device, "devtmpfs") ||
			strings.HasPrefix(device, "overlay") {
			continue
		}

		mountPoint := parts[5]
		if len(parts) > 6 {
			mountPoint = parts[len(parts)-1]
		}

		// Skip system mounts
		if strings.HasPrefix(mountPoint, "/dev") ||
			strings.HasPrefix(mountPoint, "/sys") ||
			strings.HasPrefix(mountPoint, "/proc") ||
			strings.HasPrefix(mountPoint, "/run") {
			continue
		}

		total, _ := strconv.ParseUint(parts[1], 10, 64)
		used, _ := strconv.ParseUint(parts[2], 10, 64)
		free, _ := strconv.ParseUint(parts[3], 10, 64)

		// If df -k was used (no -B1 support), convert from KB
		if total < 1000000000 { // Less than 1GB in bytes, probably in KB
			total *= 1024
			used *= 1024
			free *= 1024
		}

		var usagePercent float64
		if total > 0 {
			usagePercent = float64(used) / float64(total) * 100
		}

		disk.Partitions = append(disk.Partitions, domain.PartitionMetrics{
			Device:       device,
			MountPoint:   mountPoint,
			Total:        total,
			Used:         used,
			Free:         free,
			UsagePercent: usagePercent,
		})
	}

	return disk, nil
}

// CheckRayInstalled checks if Ray is installed on a device
func (c *Collector) CheckRayInstalled(ctx context.Context, device *domain.Device) (bool, string, error) {
	output, err := c.executor.Execute(ctx, device, "python3 -c 'import ray; print(ray.__version__)' 2>/dev/null || python -c 'import ray; print(ray.__version__)' 2>/dev/null")
	if err != nil {
		return false, "", nil
	}

	version := strings.TrimSpace(output)
	return version != "", version, nil
}

// CheckPythonVersion checks Python version on a device
func (c *Collector) CheckPythonVersion(ctx context.Context, device *domain.Device) (string, error) {
	output, err := c.executor.Execute(ctx, device, "python3 --version 2>/dev/null || python --version 2>/dev/null")
	if err != nil {
		return "", err
	}

	// Parse "Python 3.x.x" format
	parts := strings.Fields(output)
	if len(parts) >= 2 {
		return parts[1], nil
	}

	return strings.TrimSpace(output), nil
}
