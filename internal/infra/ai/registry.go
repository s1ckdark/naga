package ai

import (
	"context"

	"github.com/s1ckdark/hydra/internal/domain"
)

// Registry routes AI calls to the appropriate provider, falling back to
// RuleBasedScheduler when no provider is configured or a call fails.
type Registry struct {
	headSelector      HeadSelector
	taskScheduler     TaskScheduler
	capacityEstimator CapacityEstimator
	fallback          *RuleBasedScheduler
}

// NewRegistry builds a Registry from cfg. Providers are populated by other
// packages (claude, openai, zai) that register themselves; for now any
// unconfigured role is left nil so the fallback is used.
func NewRegistry(cfg Config) *Registry {
	return &Registry{
		fallback: &RuleBasedScheduler{},
	}
}

// SetHeadSelector configures the provider used for head election.
func (r *Registry) SetHeadSelector(h HeadSelector) { r.headSelector = h }

// SetTaskScheduler configures the provider used for task scheduling.
func (r *Registry) SetTaskScheduler(ts TaskScheduler) { r.taskScheduler = ts }

// SelectHead implements HeadSelector, delegating to the configured provider
// or falling back to rule-based scoring.
func (r *Registry) SelectHead(ctx context.Context, candidates []domain.ElectionCandidate) (string, string, error) {
	if r.headSelector != nil {
		nodeID, reason, err := r.headSelector.SelectHead(ctx, candidates)
		if err == nil {
			return nodeID, reason, nil
		}
	}
	// Rule-based fallback: pick candidate with highest RuleBasedScore.
	if len(candidates) == 0 {
		return "", "no candidates", nil
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.RuleBasedScore() > best.RuleBasedScore() {
			best = c
		}
	}
	return best.NodeID, "rule-based fallback selection", nil
}

// ScheduleTask implements TaskScheduler, delegating to the configured provider
// or falling back to RuleBasedScheduler.
func (r *Registry) ScheduleTask(ctx context.Context, task *domain.Task, workers []WorkerSnapshot) (*ScheduleDecision, error) {
	if r.taskScheduler != nil {
		decision, err := r.taskScheduler.ScheduleTask(ctx, task, workers)
		if err == nil {
			return decision, nil
		}
	}
	return r.fallback.Schedule(task, workers), nil
}

// EstimateCapacity implements CapacityEstimator, delegating to the configured
// provider. Returns nil if no estimator is configured.
func (r *Registry) EstimateCapacity(ctx context.Context, worker WorkerSnapshot, pendingTasks []*domain.Task) (*CapacityEstimate, error) {
	if r.capacityEstimator != nil {
		return r.capacityEstimator.EstimateCapacity(ctx, worker, pendingTasks)
	}
	return nil, nil
}

// TaskSchedulerProvider returns the configured task scheduler or nil.
// Callers that want fallback-aware routing should use ScheduleTask directly.
func (r *Registry) TaskSchedulerProvider() TaskScheduler {
	return r.taskScheduler
}
