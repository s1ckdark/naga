package ssh

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dave/naga/internal/domain"
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

	// Get load average (macOS vm.loadavg returns "{ 1.23 2.34 3.45 }", strip braces)
	output, err := c.executor.Execute(ctx, device, "cat /proc/loadavg 2>/dev/null || sysctl -n vm.loadavg 2>/dev/null")
	if err == nil {
		cleaned := strings.NewReplacer("{", "", "}", "").Replace(output)
		parts := strings.Fields(cleaned)
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

	// Get actual CPU usage percent
	cpu.UsagePercent = c.collectCPUUsage(ctx, device)

	return cpu, nil
}

// collectCPUUsage gets real CPU usage percentage.
// Linux: delta of /proc/stat between two samples.
// macOS: parses "CPU usage" from top (second sample for accuracy).
func (c *Collector) collectCPUUsage(ctx context.Context, device *domain.Device) float64 {
	// Try Linux /proc/stat first: two samples 1s apart, calculate delta
	cmd := `cat /proc/stat 2>/dev/null | head -1`
	out1, err := c.executor.Execute(ctx, device, cmd)
	if err == nil && strings.HasPrefix(out1, "cpu ") {
		// Sleep 1s and take second sample
		cmd2 := `sleep 1 && cat /proc/stat | head -1`
		out2, err2 := c.executor.Execute(ctx, device, cmd2)
		if err2 == nil {
			return parseProcStatDelta(out1, out2)
		}
	}

	// macOS: top -l 2 takes two samples; second line is accurate
	out, err := c.executor.Execute(ctx, device, `top -l 2 -n 0 -s 1 2>/dev/null | grep "CPU usage" | tail -1`)
	if err == nil && strings.Contains(out, "CPU usage") {
		return parseMacOSTopCPU(out)
	}

	return 0
}

// parseProcStatDelta calculates CPU usage % from two /proc/stat "cpu" lines.
// Format: cpu user nice system idle iowait irq softirq steal
func parseProcStatDelta(line1, line2 string) float64 {
	parse := func(line string) (idle, total uint64) {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, 0
		}
		// fields[0] = "cpu", fields[1..] = user nice system idle ...
		var vals []uint64
		for _, f := range fields[1:] {
			v, _ := strconv.ParseUint(f, 10, 64)
			vals = append(vals, v)
			total += v
		}
		if len(vals) >= 4 {
			idle = vals[3] // idle is the 4th value
		}
		return idle, total
	}

	idle1, total1 := parse(line1)
	idle2, total2 := parse(line2)

	totalDelta := total2 - total1
	idleDelta := idle2 - idle1

	if totalDelta == 0 {
		return 0
	}

	usage := float64(totalDelta-idleDelta) / float64(totalDelta) * 100
	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}
	return usage
}

// parseMacOSTopCPU parses macOS top output like:
// "CPU usage: 12.34% user, 5.67% sys, 81.99% idle"
func parseMacOSTopCPU(line string) float64 {
	var user, sys float64
	parts := strings.Split(line, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		fields := strings.Fields(p)
		for i, f := range fields {
			if strings.HasSuffix(f, "%") && i+1 < len(fields) {
				val, _ := strconv.ParseFloat(strings.TrimSuffix(f, "%"), 64)
				label := fields[i+1]
				switch label {
				case "user":
					user = val
				case "sys":
					sys = val
				}
			}
		}
	}
	return user + sys
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
