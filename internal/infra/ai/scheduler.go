package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/s1ckdark/hydra/internal/domain"
)

// BuildSelectionPrompt constructs the prompt sent to an AI provider for
// head node election. The AI must respond with ONLY valid JSON.
func BuildSelectionPrompt(candidates []domain.ElectionCandidate) string {
	candidateJSON, _ := json.MarshalIndent(candidates, "", "  ")
	return fmt.Sprintf(`You are a orch management AI. The head node has failed and you must select the best replacement.

Candidates:
%s

Select considering: lower GPU utilization, more free memory, lower latency, fewer running jobs.

Respond with ONLY valid JSON:
{"node_id": "<selected_node_id>", "reason": "<brief explanation>"}`, string(candidateJSON))
}

// RuleBasedScheduler is the deterministic fallback scheduler.
// Scoring weights: GPU 40%, Memory 30%, CPU 20%, Queue depth 10%,
// multiplied by a priority factor (urgent 2.0, high 1.3, normal 1.0, low 0.7).
type RuleBasedScheduler struct{}

// ineligible is the sentinel score for a worker that cannot run a task.
const ineligible = -1.0

// Schedule picks the best eligible worker for task. Returns nil if none qualify.
func (s *RuleBasedScheduler) Schedule(task *domain.Task, workers []WorkerSnapshot) *ScheduleDecision {
	best := PickBestWorker(task, workers)
	if best == nil {
		return nil
	}
	return &ScheduleDecision{
		DeviceID:   best.DeviceID,
		Reason:     "rule-based: task-aware score with anti-affinity",
		Confidence: 0.7,
	}
}

// PickBestWorker returns the highest-scoring eligible worker for task, or nil.
func PickBestWorker(task *domain.Task, workers []WorkerSnapshot) *WorkerSnapshot {
	var best *WorkerSnapshot
	bestScore := ineligible
	for i := range workers {
		s := ScoreForTask(task, workers[i])
		if s <= ineligible {
			continue
		}
		if best == nil || s > bestScore {
			bestScore = s
			best = &workers[i]
		}
	}
	return best
}

// ScoreForTask returns a placement score for w given task. Higher = better.
// Returns `ineligible` if w cannot run task due to capability, resource, or
// anti-affinity constraints.
func ScoreForTask(task *domain.Task, w WorkerSnapshot) float64 {
	if task == nil {
		return ineligible
	}
	// Anti-affinity: worker previously failed this task.
	for _, blocked := range task.BlockedDeviceIDs {
		if blocked == w.DeviceID {
			return ineligible
		}
	}
	// Capability match: every required capability must be present.
	if !hasAllCapabilities(w.Capabilities, task.RequiredCapabilities) {
		return ineligible
	}
	// Strict resource check: reject if worker can't physically fit the task.
	if r := task.ResourceReqs; r != nil {
		if r.GPUMemoryMB > 0 && r.GPUMemoryMB > w.GPUMemoryFreeMB {
			return ineligible
		}
		if r.MemoryMB > 0 && float64(r.MemoryMB)/1024.0 > w.MemoryFreeGB {
			return ineligible
		}
	}
	// Soft score: weighted resource availability.
	gpuFree := 100 - w.GPUUtilization
	memScore := w.MemoryFreeGB * 5.0
	cpuFree := 100 - w.CPUUsage
	queueScore := float64(100 - w.RunningJobs*10)
	if queueScore < 0 {
		queueScore = 0
	}
	base := gpuFree*0.4 + memScore*0.3 + cpuFree*0.2 + queueScore*0.1
	return base * priorityMultiplier(task.Priority)
}

func priorityMultiplier(p domain.TaskPriority) float64 {
	switch p {
	case domain.TaskPriorityUrgent:
		return 2.0
	case domain.TaskPriorityHigh:
		return 1.3
	case domain.TaskPriorityLow:
		return 0.7
	default:
		return 1.0
	}
}

// PickTopKEligible returns workers whose rule-based score is within
// epsilonRatio of the top score, up to k entries. Returns nil if no worker
// is eligible. The returned slice is always ordered by score descending,
// and includes the top worker even if k == 0 (k is a soft cap, 0 disables).
func PickTopKEligible(task *domain.Task, workers []WorkerSnapshot, k int, epsilonRatio float64) []WorkerSnapshot {
	type scored struct {
		w WorkerSnapshot
		s float64
	}
	scoredWorkers := make([]scored, 0, len(workers))
	for _, w := range workers {
		s := ScoreForTask(task, w)
		if s <= ineligible {
			continue
		}
		scoredWorkers = append(scoredWorkers, scored{w, s})
	}
	if len(scoredWorkers) == 0 {
		return nil
	}
	sort.Slice(scoredWorkers, func(i, j int) bool {
		return scoredWorkers[i].s > scoredWorkers[j].s
	})
	top := scoredWorkers[0].s
	cutoff := top * (1 - epsilonRatio)
	var out []WorkerSnapshot
	for i, sw := range scoredWorkers {
		if sw.s < cutoff {
			break
		}
		if k > 0 && i >= k {
			break
		}
		out = append(out, sw.w)
	}
	return out
}

// ScheduleWithTiebreak picks the best worker for task. When more than one
// worker scores within epsilonRatio of the top, arbiter (AI provider) is
// asked to pick. Any arbiter error, nil arbiter, or decision that does not
// match a tied candidate falls back to the top rule-based candidate.
// timeout caps the arbiter call; 0 disables the timeout.
func ScheduleWithTiebreak(
	ctx context.Context,
	task *domain.Task,
	workers []WorkerSnapshot,
	arbiter TaskScheduler,
	epsilonRatio float64,
	timeout time.Duration,
) *WorkerSnapshot {
	tied := PickTopKEligible(task, workers, 5, epsilonRatio)
	if len(tied) == 0 {
		return nil
	}
	if len(tied) == 1 || arbiter == nil {
		top := tied[0]
		return &top
	}
	callCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	decision, err := arbiter.ScheduleTask(callCtx, task, tied)
	if err != nil || decision == nil || decision.DeviceID == "" {
		top := tied[0]
		return &top
	}
	for i := range tied {
		if tied[i].DeviceID == decision.DeviceID {
			return &tied[i]
		}
	}
	top := tied[0]
	return &top
}

// ScheduleAlways always asks the arbiter (AI provider) to pick from every
// eligible worker, not just the rule-based top tier. This is the path used
// when AlwaysConsult is enabled in config or per-task. Falls back to the
// highest-scoring rule-based candidate on arbiter error, nil arbiter, or
// an unrecognised decision. timeout caps the arbiter call; 0 disables it.
func ScheduleAlways(
	ctx context.Context,
	task *domain.Task,
	workers []WorkerSnapshot,
	arbiter TaskScheduler,
	timeout time.Duration,
) *WorkerSnapshot {
	type scoredWorker struct {
		w WorkerSnapshot
		s float64
	}
	scored := make([]scoredWorker, 0, len(workers))
	for _, w := range workers {
		s := ScoreForTask(task, w)
		if s <= ineligible {
			continue
		}
		scored = append(scored, scoredWorker{w, s})
	}
	if len(scored) == 0 {
		return nil
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].s > scored[j].s })

	if len(scored) == 1 || arbiter == nil {
		top := scored[0].w
		return &top
	}

	candidates := make([]WorkerSnapshot, 0, len(scored))
	for _, s := range scored {
		candidates = append(candidates, s.w)
	}

	callCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	decision, err := arbiter.ScheduleTask(callCtx, task, candidates)
	if err != nil || decision == nil || decision.DeviceID == "" {
		top := scored[0].w
		return &top
	}
	for i := range candidates {
		if candidates[i].DeviceID == decision.DeviceID {
			return &candidates[i]
		}
	}
	top := scored[0].w
	return &top
}

func hasAllCapabilities(have, need []string) bool {
	if len(need) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(have))
	for _, c := range have {
		set[c] = struct{}{}
	}
	for _, req := range need {
		if _, ok := set[req]; !ok {
			return false
		}
	}
	return true
}

// BuildTaskSchedulingPrompt constructs the prompt sent to an AI provider for
// task scheduling. The AI must respond with ONLY valid JSON.
func BuildTaskSchedulingPrompt(task *domain.Task, workers []WorkerSnapshot) string {
	taskJSON, _ := json.MarshalIndent(task, "", "  ")
	workersJSON, _ := json.MarshalIndent(workers, "", "  ")
	return fmt.Sprintf(`You are a orch task scheduler. Select the best worker for the given task.

Task:
%s

Available workers:
%s

Choose the worker that best matches the task requirements considering GPU availability, free memory, CPU usage, and current queue depth.

Respond with ONLY valid JSON (no explanation, no markdown):
{"device_id": "<selected_device_id>", "reason": "<brief explanation>", "confidence": <0.0-1.0>}`,
		string(taskJSON), string(workersJSON))
}

// BuildCapacityEstimationPrompt constructs the prompt for capacity estimation.
// The AI must respond with ONLY valid JSON.
func BuildCapacityEstimationPrompt(worker WorkerSnapshot, pendingTasks []*domain.Task) string {
	workerJSON, _ := json.MarshalIndent(worker, "", "  ")
	tasksJSON, _ := json.MarshalIndent(pendingTasks, "", "  ")
	return fmt.Sprintf(`You are a orch resource estimator. Estimate the remaining capacity of the given worker.

Worker:
%s

Pending tasks (already queued):
%s

Estimate how many additional task slots remain and identify the primary bottleneck.

Respond with ONLY valid JSON (no explanation, no markdown):
{"available_gpu_percent": <0-100>, "available_memory_gb": <float>, "estimated_slots": <int>, "bottleneck": "<gpu|memory|cpu|none>", "recommendation": "<brief advice>"}`,
		string(workerJSON), string(tasksJSON))
}
