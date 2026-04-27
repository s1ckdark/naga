package domain

import (
	"context"
	"sync"
	"testing"
	"time"
)

// recordingRepo counts and stores every Save call, thread-safe.
type recordingRepo struct {
	mu    sync.Mutex
	saved []*Task
}

func (r *recordingRepo) Save(_ context.Context, t *Task) error {
	r.mu.Lock()
	r.saved = append(r.saved, t)
	r.mu.Unlock()
	return nil
}
func (r *recordingRepo) Delete(_ context.Context, _ string) error { return nil }
func (r *recordingRepo) GetByID(_ context.Context, _ string) (*Task, error) {
	return nil, nil
}
func (r *recordingRepo) GetByGroup(_ context.Context, _ string) ([]*Task, error) {
	return nil, nil
}
func (r *recordingRepo) MarkStaleTasksFailed(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}
func (r *recordingRepo) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.saved) }
func (r *recordingRepo) lastByID(id string) *Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.saved) - 1; i >= 0; i-- {
		if r.saved[i].ID == id {
			return r.saved[i]
		}
	}
	return nil
}

func TestTaskQueue_AsyncPersist_DrainsAllSends(t *testing.T) {
	q := NewTaskQueue()
	repo := &recordingRepo{}
	q.WithRepo(repo).WithAsyncPersist(64)
	t.Cleanup(func() { q.Close() })

	for i := 0; i < 20; i++ {
		q.Enqueue(&Task{ID: "t" + itoaShort(i), Type: "shell"})
	}
	q.Close()

	if got := repo.count(); got != 20 {
		t.Errorf("saved = %d, want 20", got)
	}
}

func TestTaskQueue_AsyncPersist_TaskMutationDoesNotCorruptSnapshot(t *testing.T) {
	q := NewTaskQueue()
	repo := &recordingRepo{}
	q.WithRepo(repo).WithAsyncPersist(64)
	defer q.Close()

	task := &Task{ID: "snap-test", Type: "shell", Payload: map[string]interface{}{"v": 1}}
	q.Enqueue(task)
	// Mutate the original right after enqueue. The async worker should see
	// the cloned snapshot (v=1), not the post-mutation value (v=999).
	task.Payload["v"] = 999

	q.Close()

	got := repo.lastByID("snap-test")
	if got == nil {
		t.Fatal("snap-test not persisted")
	}
	if v, _ := got.Payload["v"].(int); v != 1 {
		t.Errorf("snapshot saw mutated value: v=%d, want 1", v)
	}
}

func TestTaskQueue_AsyncPersist_DoubleEnableNoop(t *testing.T) {
	q := NewTaskQueue()
	repo := &recordingRepo{}
	q.WithRepo(repo).WithAsyncPersist(8).WithAsyncPersist(8)
	t.Cleanup(func() { q.Close() })

	q.Enqueue(&Task{ID: "x", Type: "shell"})
	q.Close()

	if got := repo.count(); got != 1 {
		t.Errorf("saved = %d, want 1 (double WithAsyncPersist must be idempotent)", got)
	}
}

func TestTaskQueue_AsyncPersist_FullChannelDrops(t *testing.T) {
	// Slow repo: each Save blocks long enough that the buffer (size 1) fills
	// quickly and the next sends must drop.
	slow := &slowingRepo{delay: 50 * time.Millisecond}
	q := NewTaskQueue()
	q.WithRepo(slow).WithAsyncPersist(1)
	t.Cleanup(func() { q.Close() })

	// Pipe 20 sends in tight loop. The first goes into the channel, then the
	// worker grabs it and starts the slow Save. The second fills the buffer
	// while the worker is still busy. Subsequent sends drop.
	for i := 0; i < 20; i++ {
		q.Enqueue(&Task{ID: "drop" + itoaShort(i), Type: "shell"})
	}
	q.Close()

	// We expect SOME drops — the delivery count should be < 20 — but at
	// least the first task should land. A precise count is timing-dependent
	// so just verify we hit the drop path at least once.
	if slow.count() >= 20 {
		t.Errorf("expected drops with buf=1 + slow repo, got %d/20 saved", slow.count())
	}
	if slow.count() == 0 {
		t.Errorf("expected at least one save to land, got 0")
	}
}

// itoaShort converts small ints to short strings for test ids.
func itoaShort(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('a'+(i/10))) + string(rune('0'+(i%10)))
}

// slowingRepo simulates a slow DB. Goroutine-safe.
type slowingRepo struct {
	delay time.Duration
	mu    sync.Mutex
	calls int
}

func (r *slowingRepo) Save(_ context.Context, _ *Task) error {
	time.Sleep(r.delay)
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return nil
}
func (r *slowingRepo) Delete(_ context.Context, _ string) error           { return nil }
func (r *slowingRepo) GetByID(_ context.Context, _ string) (*Task, error) { return nil, nil }
func (r *slowingRepo) GetByGroup(_ context.Context, _ string) ([]*Task, error) {
	return nil, nil
}
func (r *slowingRepo) MarkStaleTasksFailed(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}
func (r *slowingRepo) count() int { r.mu.Lock(); defer r.mu.Unlock(); return r.calls }
