# Multi-Provider AI Task Scheduler Design

## Overview

Naga의 태스크 할당을 capability-only 매칭에서 AI 기반 리소스-aware 스케줄링으로 확장. 여러 AI 프로바이더(Claude, OpenAI, Z.AI, Local AI)를 용도별로 라우팅하여 최적의 태스크 배치를 결정한다.

## Key Decisions

- **용도별 라우팅**: head 선출, 태스크 스케줄링, 용량 추정 각각에 다른 AI 프로바이더 배정
- **API key 인증 우선**: OAuth는 TOS 이슈 가능성으로 후순위
- **OpenAI 호환 엔드포인트**: Local AI(Ollama, vLLM, LM Studio 등) 통합 커버
- **접근법 A (단일 인터페이스 확장)**: 기존 AISelector 패턴을 역할별 인터페이스로 분리

## Architecture

### Role-Based Interfaces

```go
// HeadSelector - head 노드 선출 (기존 AISelector 확장)
type HeadSelector interface {
    SelectHead(ctx context.Context, candidates []domain.ElectionCandidate) (nodeID string, reason string, err error)
}

// TaskScheduler - 태스크를 어느 워커에 할당할지 결정
type TaskScheduler interface {
    ScheduleTask(ctx context.Context, task *domain.Task, workers []WorkerSnapshot) (*ScheduleDecision, error)
}

// CapacityEstimator - 워커의 가용 자원 추정
type CapacityEstimator interface {
    EstimateCapacity(ctx context.Context, worker WorkerSnapshot, pendingTasks []*domain.Task) (*CapacityEstimate, error)
}
```

### Data Types

```go
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

type ScheduleDecision struct {
    DeviceID   string  `json:"device_id"`
    Reason     string  `json:"reason"`
    Confidence float64 `json:"confidence"`
}

type CapacityEstimate struct {
    AvailableGPUPercent float64 `json:"available_gpu_percent"`
    AvailableMemoryGB   float64 `json:"available_memory_gb"`
    EstimatedSlots      int     `json:"estimated_slots"`
    Bottleneck          string  `json:"bottleneck"`
    Recommendation      string  `json:"recommendation"`
}

type ResourceRequirements struct {
    GPUMemoryMB    int     `json:"gpuMemoryMB,omitempty"`
    CPUCores       int     `json:"cpuCores,omitempty"`
    MemoryMB       int     `json:"memoryMB,omitempty"`
    GPUUtilization float64 `json:"gpuUtilization,omitempty"`
}
```

### Provider Structure

```
internal/infra/ai/
├── provider.go              # Interfaces + common types
├── registry.go              # Role-based provider routing
├── scheduler.go             # RuleBasedScheduler + prompt builders
├── claude/
│   └── claude.go            # Claude Messages API
├── openai/
│   ├── openai.go            # OpenAI Chat API
│   └── local.go             # Local AI (OpenAI-compatible)
└── zai/
    └── zai.go               # Z.AI API
```

### Registry

```go
type Registry struct {
    headSelector      HeadSelector
    taskScheduler     TaskScheduler
    capacityEstimator CapacityEstimator
    fallbackScheduler *RuleBasedScheduler
}

type Config struct {
    HeadSelection      ProviderConfig `json:"headSelection"`
    TaskScheduling     ProviderConfig `json:"taskScheduling"`
    CapacityEstimation ProviderConfig `json:"capacityEstimation"`
}

type ProviderConfig struct {
    Provider string `json:"provider"`   // "claude", "openai", "zai", "local"
    APIKey   string `json:"apiKey"`
    Model    string `json:"model"`
    Endpoint string `json:"endpoint"`   // Local AI custom endpoint
}
```

### Provider Role Matrix

| Provider | HeadSelector | TaskScheduler | CapacityEstimator |
|----------|:---:|:---:|:---:|
| Claude   | O | O | - |
| OpenAI   | - | O | O |
| Z.AI     | - | O | - |
| Local    | - | O | O |

### Scheduling Flow

```
Task Created
  → 1. Capability filtering (existing, fast)
  → 2. Collect WorkerSnapshots from heartbeat metrics
  → 3. AI TaskScheduler call
       → Fallback: RuleBasedScheduler
  → 4. Assign + WebSocket dispatch
```

### Capacity Estimation (Background)

Every 30s via supervisor loop:
- Refresh CapacityEstimate per worker
- Workers with EstimatedSlots=0 excluded from scheduling candidates
- Bottleneck info displayed on dashboard

### Error Handling

All AI calls: 5s timeout → fallback to RuleBasedScheduler.
RuleBasedScheduler scoring: GPU 40%, Memory 30%, CPU 20%, Queue 10%.

## Files Changed

### Modified (5 files)
- `internal/agent/election.go` - AISelector → HeadSelector rename
- `internal/agent/agent.go` - AISelector field → Registry
- `internal/domain/task.go` - Add ResourceRequirements field
- `internal/domain/taskqueue.go` - Add AI scheduling path to FindMatchingTask
- `cmd/cluster-agent/main.go` - resolveAISelector → Registry init

### New (7 files)
- `internal/infra/ai/provider.go`
- `internal/infra/ai/registry.go`
- `internal/infra/ai/scheduler.go`
- `internal/infra/ai/claude/claude.go`
- `internal/infra/ai/openai/openai.go`
- `internal/infra/ai/openai/local.go`
- `internal/infra/ai/zai/zai.go`

### Deleted (1 file)
- `internal/infra/ai/selector.go` → replaced by claude/claude.go

## Backward Compatibility
- `--anthropic-key` CLI flag retained, coexists with config file
- `election_test.go` mock renamed: mockAISelector → mockHeadSelector
