package ai

import (
	"encoding/json"
	"fmt"

	"github.com/dave/naga/internal/domain"
)

// BuildSelectionPrompt constructs the prompt sent to an AI provider for
// head node election. The AI must respond with ONLY valid JSON.
func BuildSelectionPrompt(candidates []domain.ElectionCandidate) string {
	candidateJSON, _ := json.MarshalIndent(candidates, "", "  ")
	return fmt.Sprintf(`You are a cluster management AI. The head node has failed and you must select the best replacement.

Candidates:
%s

Select considering: lower GPU utilization, more free memory, lower latency, fewer running jobs.

Respond with ONLY valid JSON:
{"node_id": "<selected_node_id>", "reason": "<brief explanation>"}`, string(candidateJSON))
}

// RuleBasedScheduler is the deterministic fallback scheduler.
// Scoring weights: GPU 40%, Memory 30%, CPU 20%, Queue depth 10%.
type RuleBasedScheduler struct{}

// Schedule picks the best worker for task using weighted resource scoring.
func (s *RuleBasedScheduler) Schedule(task *domain.Task, workers []WorkerSnapshot) *ScheduleDecision {
	if len(workers) == 0 {
		return nil
	}

	candidates := workers
	// No resource filtering needed here — capability filtering is done upstream.

	if len(candidates) == 0 {
		return nil
	}

	best := candidates[0]
	bestScore := workerScore(best)
	for _, w := range candidates[1:] {
		if sc := workerScore(w); sc > bestScore {
			bestScore = sc
			best = w
		}
	}

	return &ScheduleDecision{
		DeviceID:   best.DeviceID,
		Reason:     "rule-based: highest weighted resource score",
		Confidence: 0.7,
	}
}

// workerScore computes a weighted score for a worker (higher = better).
func workerScore(w WorkerSnapshot) float64 {
	gpuFree := 100 - w.GPUUtilization                         // 0–100
	memScore := w.MemoryFreeGB * 5.0                          // scale GB to comparable range
	cpuFree := 100 - w.CPUUsage                               // 0–100
	queueScore := float64(100 - w.RunningJobs*10)             // penalise busy workers
	if queueScore < 0 {
		queueScore = 0
	}
	return gpuFree*0.4 + memScore*0.3 + cpuFree*0.2 + queueScore*0.1
}

// BuildTaskSchedulingPrompt constructs the prompt sent to an AI provider for
// task scheduling. The AI must respond with ONLY valid JSON.
func BuildTaskSchedulingPrompt(task *domain.Task, workers []WorkerSnapshot) string {
	taskJSON, _ := json.MarshalIndent(task, "", "  ")
	workersJSON, _ := json.MarshalIndent(workers, "", "  ")
	return fmt.Sprintf(`You are a cluster task scheduler. Select the best worker for the given task.

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
	return fmt.Sprintf(`You are a cluster resource estimator. Estimate the remaining capacity of the given worker.

Worker:
%s

Pending tasks (already queued):
%s

Estimate how many additional task slots remain and identify the primary bottleneck.

Respond with ONLY valid JSON (no explanation, no markdown):
{"available_gpu_percent": <0-100>, "available_memory_gb": <float>, "estimated_slots": <int>, "bottleneck": "<gpu|memory|cpu|none>", "recommendation": "<brief advice>"}`,
		string(workerJSON), string(tasksJSON))
}
