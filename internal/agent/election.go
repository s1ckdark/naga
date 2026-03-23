package agent

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/dave/naga/internal/domain"
)

type AISelector interface {
	SelectHead(ctx context.Context, candidates []domain.ElectionCandidate) (nodeID string, reason string, err error)
}

type Election struct {
	ai AISelector
}

func NewElection(ai AISelector) *Election {
	return &Election{ai: ai}
}

func (e *Election) Elect(ctx context.Context, clusterID string, candidates []domain.ElectionCandidate) (*domain.ElectionResult, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no candidates available for election in cluster %s", clusterID)
	}

	result := &domain.ElectionResult{
		DecidedAt:  time.Now(),
		Candidates: candidates,
	}

	// Try AI selection first
	if e.ai != nil {
		aiCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		nodeID, reason, err := e.ai.SelectHead(aiCtx, candidates)
		if err == nil && nodeID != "" {
			for _, c := range candidates {
				if c.NodeID == nodeID {
					result.NewHeadID = nodeID
					result.Reason = reason
					result.AIDecision = true
					return result, nil
				}
			}
		}
	}

	// Rule-based fallback
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].RuleBasedScore() > candidates[j].RuleBasedScore()
	})

	result.NewHeadID = candidates[0].NodeID
	result.Reason = fmt.Sprintf("rule-based: highest score %.1f (GPU: %.0f%%, mem: %.1fGB free)",
		candidates[0].RuleBasedScore(), candidates[0].GPUUtilization, candidates[0].MemoryFreeGB)
	result.AIDecision = false

	return result, nil
}
