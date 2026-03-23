package agent

import (
	"context"
	"testing"
	"time"

	"github.com/dave/naga/internal/domain"
)

type mockAISelector struct {
	result    string
	reason    string
	shouldErr bool
}

func (m *mockAISelector) SelectHead(ctx context.Context, candidates []domain.ElectionCandidate) (string, string, error) {
	if m.shouldErr {
		return "", "", context.DeadlineExceeded
	}
	return m.result, m.reason, nil
}

func TestElection_RuleBasedFallback(t *testing.T) {
	candidates := []domain.ElectionCandidate{
		{NodeID: "worker-1", GPUUtilization: 80, MemoryFreeGB: 4, Latency: 10 * time.Millisecond},
		{NodeID: "worker-2", GPUUtilization: 20, MemoryFreeGB: 16, Latency: 5 * time.Millisecond},
		{NodeID: "worker-3", GPUUtilization: 50, MemoryFreeGB: 8, Latency: 8 * time.Millisecond},
	}

	e := NewElection(&mockAISelector{shouldErr: true})
	result, err := e.Elect(context.Background(), "cluster-1", candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NewHeadID != "worker-2" {
		t.Errorf("expected worker-2, got %s", result.NewHeadID)
	}
	if result.AIDecision {
		t.Error("expected rule-based decision")
	}
}

func TestElection_AIDecision(t *testing.T) {
	candidates := []domain.ElectionCandidate{
		{NodeID: "worker-1", GPUUtilization: 80, MemoryFreeGB: 4},
		{NodeID: "worker-2", GPUUtilization: 20, MemoryFreeGB: 16},
	}

	ai := &mockAISelector{result: "worker-1", reason: "better network position"}
	e := NewElection(ai)
	result, err := e.Elect(context.Background(), "cluster-1", candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NewHeadID != "worker-1" {
		t.Errorf("expected worker-1, got %s", result.NewHeadID)
	}
	if !result.AIDecision {
		t.Error("expected AI decision flag")
	}
}

func TestElection_NoCandidates(t *testing.T) {
	e := NewElection(&mockAISelector{shouldErr: true})
	_, err := e.Elect(context.Background(), "cluster-1", nil)
	if err == nil {
		t.Error("expected error for empty candidates")
	}
}
