package ai

import (
	"context"

	"github.com/dave/naga/internal/domain"
)

// HeadSelector selects the best head node from candidates.
type HeadSelector interface {
	SelectHead(ctx context.Context, candidates []domain.ElectionCandidate) (nodeID string, reason string, err error)
}

// TaskScheduler decides which worker should run a task.
type TaskScheduler interface {
	ScheduleTask(ctx context.Context, task *domain.Task, workers []WorkerSnapshot) (*ScheduleDecision, error)
}

// CapacityEstimator estimates how many more tasks a worker can absorb.
type CapacityEstimator interface {
	EstimateCapacity(ctx context.Context, worker WorkerSnapshot, pendingTasks []*domain.Task) (*CapacityEstimate, error)
}

// WorkerSnapshot captures a worker's current resource state.
type WorkerSnapshot struct {
	DeviceID        string
	Capabilities    []string
	GPUUtilization  float64
	MemoryFreeGB    float64
	CPUUsage        float64
	RunningJobs     int
	GPUCount        int
	GPUMemoryFreeMB int
}

// ScheduleDecision is the AI response for task placement.
type ScheduleDecision struct {
	DeviceID   string  `json:"device_id"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

// CapacityEstimate describes how much capacity a worker has remaining.
type CapacityEstimate struct {
	AvailableGPUPercent float64 `json:"available_gpu_percent"`
	AvailableMemoryGB   float64 `json:"available_memory_gb"`
	EstimatedSlots      int     `json:"estimated_slots"`
	Bottleneck          string  `json:"bottleneck"`
	Recommendation      string  `json:"recommendation"`
}

// Config controls which provider handles each AI role.
type Config struct {
	HeadSelection      ProviderConfig `json:"headSelection"`
	TaskScheduling     ProviderConfig `json:"taskScheduling"`
	CapacityEstimation ProviderConfig `json:"capacityEstimation"`
}

// ProviderConfig holds credentials and routing info for a single provider.
type ProviderConfig struct {
	Provider string `json:"provider"` // "claude", "openai", "zai", "local"
	APIKey   string `json:"apiKey"`
	Model    string `json:"model"`
	Endpoint string `json:"endpoint"` // for local/zai custom endpoints
}
