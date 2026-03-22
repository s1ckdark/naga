package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GPUInfo represents a single GPU's metrics from nvidia-smi
type GPUInfo struct {
	Index              int     `json:"index"`
	Name               string  `json:"name"`
	UtilizationPercent float64 `json:"utilizationPercent"`
	MemoryUsedMB       int     `json:"memoryUsedMB"`
	MemoryTotalMB      int     `json:"memoryTotalMB"`
	TemperatureC       int     `json:"temperatureC"`
	PowerDrawW         float64 `json:"powerDrawW"`
	PowerLimitW        float64 `json:"powerLimitW"`
}

// MemoryUsagePercent returns memory usage as a percentage
func (g *GPUInfo) MemoryUsagePercent() float64 {
	if g.MemoryTotalMB == 0 {
		return 0
	}
	return float64(g.MemoryUsedMB) / float64(g.MemoryTotalMB) * 100
}

// GPUNodeMetrics holds GPU metrics for a single node
type GPUNodeMetrics struct {
	NodeName    string    `json:"nodeName"`
	DeviceID    string    `json:"deviceId"`
	GPUs        []GPUInfo `json:"gpus"`
	CollectedAt time.Time `json:"collectedAt"`
	Error       string    `json:"error,omitempty"`
}

// HasGPU returns true if the node has at least one GPU
func (m *GPUNodeMetrics) HasGPU() bool {
	return len(m.GPUs) > 0
}

// AvgUtilization returns the average GPU utilization across all GPUs
func (m *GPUNodeMetrics) AvgUtilization() float64 {
	if len(m.GPUs) == 0 {
		return 0
	}
	var total float64
	for _, g := range m.GPUs {
		total += g.UtilizationPercent
	}
	return total / float64(len(m.GPUs))
}

// TotalMemoryUsedMB returns total memory used across all GPUs in MB
func (m *GPUNodeMetrics) TotalMemoryUsedMB() int {
	var total int
	for _, g := range m.GPUs {
		total += g.MemoryUsedMB
	}
	return total
}

// TotalMemoryMB returns total memory across all GPUs in MB
func (m *GPUNodeMetrics) TotalMemoryMB() int {
	var total int
	for _, g := range m.GPUs {
		total += g.MemoryTotalMB
	}
	return total
}

// ParseNvidiaSmiOutput parses nvidia-smi CSV output into GPUInfo slices.
// Input format: "index, name, utilization.gpu, memory.used, memory.total, temperature.gpu, power.draw, power.limit"
// nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits
func ParseNvidiaSmiOutput(raw string) ([]GPUInfo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var gpus []GPUInfo
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, ", ")
		if len(parts) != 8 {
			return nil, fmt.Errorf("expected 8 fields, got %d: %q", len(parts), line)
		}

		index, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, fmt.Errorf("invalid index %q: %w", parts[0], err)
		}

		name := strings.TrimSpace(parts[1])

		utilization, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid utilization %q: %w", parts[2], err)
		}

		memUsed, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil {
			return nil, fmt.Errorf("invalid memory.used %q: %w", parts[3], err)
		}

		memTotal, err := strconv.Atoi(strings.TrimSpace(parts[4]))
		if err != nil {
			return nil, fmt.Errorf("invalid memory.total %q: %w", parts[4], err)
		}

		temp, err := strconv.Atoi(strings.TrimSpace(parts[5]))
		if err != nil {
			return nil, fmt.Errorf("invalid temperature %q: %w", parts[5], err)
		}

		powerDraw, err := strconv.ParseFloat(strings.TrimSpace(parts[6]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid power.draw %q: %w", parts[6], err)
		}

		powerLimit, err := strconv.ParseFloat(strings.TrimSpace(parts[7]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid power.limit %q: %w", parts[7], err)
		}

		gpus = append(gpus, GPUInfo{
			Index:              index,
			Name:               name,
			UtilizationPercent: utilization,
			MemoryUsedMB:       memUsed,
			MemoryTotalMB:      memTotal,
			TemperatureC:       temp,
			PowerDrawW:         powerDraw,
			PowerLimitW:        powerLimit,
		})
	}

	return gpus, nil
}
