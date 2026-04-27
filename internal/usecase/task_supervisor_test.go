package usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/s1ckdark/hydra/internal/domain"
)

// TestTaskSupervisor_ConcurrentSettersNoRace exercises the setter paths
// against the scheduling read paths so `go test -race` flags any race.
// resolveAlwaysConsult is only called from within the s.mu-locked region
// (via scheduleQueue), so the reader goroutine uses ScheduleNow which
// acquires s.mu before reading the same fields.
func TestTaskSupervisor_ConcurrentSettersNoRace(t *testing.T) {
	taskQueue := domain.NewTaskQueue()
	s := NewTaskSupervisor(taskQueue, nil, nil, nil)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer goroutine: hammers both setters concurrently.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.SetAlwaysConsultAI(true)
				s.SetAIArbiter(nil, 0.10, 5, 3*time.Second)
				s.SetAlwaysConsultAI(false)
			}
		}
	}()

	// Reader goroutine: ScheduleNow acquires s.mu and calls scheduleQueue
	// which internally calls resolveAlwaysConsult — reads the same fields.
	// Using ScheduleNow (not resolveAlwaysConsult directly) because
	// resolveAlwaysConsult is only safe to call while holding s.mu.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx := context.Background()
		for {
			select {
			case <-stop:
				return
			default:
				s.ScheduleNow(ctx)
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
