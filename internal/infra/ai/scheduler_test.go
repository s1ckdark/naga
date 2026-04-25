package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/s1ckdark/hydra/internal/domain"
)

type fakeArbiter struct {
	decisionDeviceID string
	err              error
	called           int
}

func (f *fakeArbiter) ScheduleTask(_ context.Context, _ *domain.Task, _ []WorkerSnapshot) (*ScheduleDecision, error) {
	f.called++
	if f.err != nil {
		return nil, f.err
	}
	return &ScheduleDecision{DeviceID: f.decisionDeviceID, Confidence: 0.9}, nil
}

func fullyLoaded(id string) WorkerSnapshot {
	return WorkerSnapshot{
		DeviceID:        id,
		Capabilities:    []string{"gpu", "cuda"},
		GPUUtilization:  20,
		MemoryFreeGB:    32,
		CPUUsage:        25,
		RunningJobs:     0,
		GPUCount:        1,
		GPUMemoryFreeMB: 24000,
	}
}

func TestScoreForTask_PicksHigherCapacity(t *testing.T) {
	weak := fullyLoaded("weak")
	weak.GPUUtilization = 80
	weak.MemoryFreeGB = 4
	strong := fullyLoaded("strong")

	task := &domain.Task{Priority: domain.TaskPriorityNormal}
	if got := PickBestWorker(task, []WorkerSnapshot{weak, strong}); got == nil || got.DeviceID != "strong" {
		t.Fatalf("expected strong worker, got %+v", got)
	}
}

func TestScoreForTask_BlockedDeviceRejected(t *testing.T) {
	w := fullyLoaded("w1")
	task := &domain.Task{
		Priority:         domain.TaskPriorityNormal,
		BlockedDeviceIDs: []string{"w1"},
	}
	if s := ScoreForTask(task, w); s != ineligible {
		t.Fatalf("blocked worker should be ineligible, got %v", s)
	}
}

func TestScoreForTask_InsufficientGPUMemoryRejected(t *testing.T) {
	w := fullyLoaded("w1")
	w.GPUMemoryFreeMB = 8000
	task := &domain.Task{
		Priority:     domain.TaskPriorityNormal,
		ResourceReqs: &domain.ResourceRequirements{GPUMemoryMB: 16000},
	}
	if s := ScoreForTask(task, w); s != ineligible {
		t.Fatalf("task needing 16GB should be rejected on 8GB-free worker, got %v", s)
	}
}

func TestScoreForTask_CapabilityMismatchRejected(t *testing.T) {
	w := fullyLoaded("w1")
	w.Capabilities = []string{"cpu"}
	task := &domain.Task{
		Priority:             domain.TaskPriorityNormal,
		RequiredCapabilities: []string{"gpu"},
	}
	if s := ScoreForTask(task, w); s != ineligible {
		t.Fatalf("worker missing required capability should be rejected, got %v", s)
	}
}

func TestScoreForTask_UrgentBoostsScore(t *testing.T) {
	w := fullyLoaded("w1")
	normal := &domain.Task{Priority: domain.TaskPriorityNormal}
	urgent := &domain.Task{Priority: domain.TaskPriorityUrgent}

	nScore := ScoreForTask(normal, w)
	uScore := ScoreForTask(urgent, w)
	if uScore <= nScore {
		t.Fatalf("urgent should boost score: normal=%v urgent=%v", nScore, uScore)
	}
	// Urgent multiplier is 2.0
	if got, want := uScore, nScore*2.0; got != want {
		t.Fatalf("urgent should be 2x normal: got %v want %v", got, want)
	}
}

func TestPickBestWorker_NoneEligible(t *testing.T) {
	w := fullyLoaded("w1")
	w.GPUMemoryFreeMB = 100
	task := &domain.Task{
		Priority:     domain.TaskPriorityNormal,
		ResourceReqs: &domain.ResourceRequirements{GPUMemoryMB: 16000},
	}
	if got := PickBestWorker(task, []WorkerSnapshot{w}); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestPickBestWorker_SkipsBlockedPicksOther(t *testing.T) {
	a := fullyLoaded("a")
	b := fullyLoaded("b")
	task := &domain.Task{
		Priority:         domain.TaskPriorityNormal,
		BlockedDeviceIDs: []string{"a"},
	}
	got := PickBestWorker(task, []WorkerSnapshot{a, b})
	if got == nil || got.DeviceID != "b" {
		t.Fatalf("expected b, got %+v", got)
	}
}

func TestPickTopKEligible_ReturnsSingleWhenClearWinner(t *testing.T) {
	strong := fullyLoaded("strong")
	weak := fullyLoaded("weak")
	weak.GPUUtilization = 90
	weak.MemoryFreeGB = 2
	weak.CPUUsage = 80
	task := &domain.Task{Priority: domain.TaskPriorityNormal}

	got := PickTopKEligible(task, []WorkerSnapshot{strong, weak}, 5, 0.10)
	if len(got) != 1 || got[0].DeviceID != "strong" {
		t.Fatalf("expected only strong within epsilon, got %+v", got)
	}
}

func TestPickTopKEligible_ReturnsMultipleWhenClose(t *testing.T) {
	a := fullyLoaded("a")
	b := fullyLoaded("b")
	b.GPUUtilization = 22
	task := &domain.Task{Priority: domain.TaskPriorityNormal}

	got := PickTopKEligible(task, []WorkerSnapshot{a, b}, 5, 0.10)
	if len(got) != 2 {
		t.Fatalf("expected both within 10%% of top, got %+v", got)
	}
}

func TestScheduleWithTiebreak_UsesArbiterOnTie(t *testing.T) {
	a := fullyLoaded("a")
	b := fullyLoaded("b")
	arb := &fakeArbiter{decisionDeviceID: "b"}
	task := &domain.Task{Priority: domain.TaskPriorityNormal}

	got := ScheduleWithTiebreak(context.Background(), task, []WorkerSnapshot{a, b}, arb, 0.10, time.Second)
	if got == nil || got.DeviceID != "b" {
		t.Fatalf("expected arbiter to pick b, got %+v", got)
	}
	if arb.called != 1 {
		t.Fatalf("arbiter should be called exactly once on tie, got %d", arb.called)
	}
}

func TestScheduleWithTiebreak_SkipsArbiterWhenNoTie(t *testing.T) {
	strong := fullyLoaded("strong")
	weak := fullyLoaded("weak")
	weak.GPUUtilization = 90
	weak.MemoryFreeGB = 2
	weak.CPUUsage = 80
	arb := &fakeArbiter{decisionDeviceID: "weak"}
	task := &domain.Task{Priority: domain.TaskPriorityNormal}

	got := ScheduleWithTiebreak(context.Background(), task, []WorkerSnapshot{strong, weak}, arb, 0.10, time.Second)
	if got == nil || got.DeviceID != "strong" {
		t.Fatalf("expected rule-based strong, got %+v", got)
	}
	if arb.called != 0 {
		t.Fatalf("arbiter should not be called without a tie, got %d calls", arb.called)
	}
}

func TestScheduleWithTiebreak_FallsBackOnArbiterError(t *testing.T) {
	a := fullyLoaded("a")
	b := fullyLoaded("b")
	arb := &fakeArbiter{err: errors.New("boom")}
	task := &domain.Task{Priority: domain.TaskPriorityNormal}

	got := ScheduleWithTiebreak(context.Background(), task, []WorkerSnapshot{a, b}, arb, 0.10, time.Second)
	if got == nil {
		t.Fatal("expected rule-based fallback, got nil")
	}
	// Either tied candidate is acceptable; rule-based ordering determines top.
	if got.DeviceID != "a" && got.DeviceID != "b" {
		t.Fatalf("expected one of the tied candidates, got %+v", got)
	}
}

func TestScheduleWithTiebreak_RejectsUnknownDeviceFromArbiter(t *testing.T) {
	a := fullyLoaded("a")
	b := fullyLoaded("b")
	arb := &fakeArbiter{decisionDeviceID: "phantom"}
	task := &domain.Task{Priority: domain.TaskPriorityNormal}

	got := ScheduleWithTiebreak(context.Background(), task, []WorkerSnapshot{a, b}, arb, 0.10, time.Second)
	if got == nil {
		t.Fatal("expected fallback to top, got nil")
	}
	if got.DeviceID != "a" && got.DeviceID != "b" {
		t.Fatalf("expected a tied candidate, got %+v", got)
	}
}

func TestScheduleAlways_CallsArbiterEvenWithClearWinner(t *testing.T) {
	strong := fullyLoaded("strong")
	weak := fullyLoaded("weak")
	weak.GPUUtilization = 90
	weak.MemoryFreeGB = 2
	weak.CPUUsage = 80
	arb := &fakeArbiter{decisionDeviceID: "weak"}
	task := &domain.Task{Priority: domain.TaskPriorityNormal}

	got := ScheduleAlways(context.Background(), task, []WorkerSnapshot{strong, weak}, arb, time.Second)
	if got == nil || got.DeviceID != "weak" {
		t.Fatalf("expected arbiter pick to win even without tie, got %+v", got)
	}
	if arb.called != 1 {
		t.Fatalf("arbiter should be called once, got %d", arb.called)
	}
}

func TestScheduleAlways_FallsBackOnArbiterError(t *testing.T) {
	a := fullyLoaded("a")
	b := fullyLoaded("b")
	b.GPUUtilization = 60 // weaker than a, so a is rule-based top
	arb := &fakeArbiter{err: errors.New("boom")}
	task := &domain.Task{Priority: domain.TaskPriorityNormal}

	got := ScheduleAlways(context.Background(), task, []WorkerSnapshot{a, b}, arb, time.Second)
	if got == nil || got.DeviceID != "a" {
		t.Fatalf("expected rule-based top 'a' on arbiter error, got %+v", got)
	}
}

func TestScheduleAlways_NilArbiterReturnsTop(t *testing.T) {
	a := fullyLoaded("a")
	b := fullyLoaded("b")
	b.GPUUtilization = 60
	task := &domain.Task{Priority: domain.TaskPriorityNormal}

	got := ScheduleAlways(context.Background(), task, []WorkerSnapshot{a, b}, nil, time.Second)
	if got == nil || got.DeviceID != "a" {
		t.Fatalf("expected rule-based top with nil arbiter, got %+v", got)
	}
}

func TestScheduleAlways_NoEligibleReturnsNil(t *testing.T) {
	w := fullyLoaded("w")
	w.Capabilities = []string{"cpu"} // no gpu
	arb := &fakeArbiter{decisionDeviceID: "w"}
	task := &domain.Task{
		Priority:             domain.TaskPriorityNormal,
		RequiredCapabilities: []string{"gpu"},
	}

	got := ScheduleAlways(context.Background(), task, []WorkerSnapshot{w}, arb, time.Second)
	if got != nil {
		t.Fatalf("expected nil when no worker matches capabilities, got %+v", got)
	}
	if arb.called != 0 {
		t.Fatalf("arbiter should not be called when no eligible workers, got %d", arb.called)
	}
}

func TestScheduleAlways_RejectsUnknownDeviceFromArbiter(t *testing.T) {
	a := fullyLoaded("a")
	b := fullyLoaded("b")
	b.GPUUtilization = 60
	arb := &fakeArbiter{decisionDeviceID: "phantom"}
	task := &domain.Task{Priority: domain.TaskPriorityNormal}

	got := ScheduleAlways(context.Background(), task, []WorkerSnapshot{a, b}, arb, time.Second)
	if got == nil || got.DeviceID != "a" {
		t.Fatalf("expected fallback to top 'a' for unknown arbiter pick, got %+v", got)
	}
}
