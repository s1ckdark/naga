package usecase

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/s1ckdark/hydra/internal/domain"
	"github.com/s1ckdark/hydra/internal/infra/ai"
	"github.com/s1ckdark/hydra/internal/web/ws"
)

// TaskSupervisor periodically reassigns work from failed/timed-out workers
// and push-schedules queued tasks onto the best available worker.
type TaskSupervisor struct {
	taskQueue    *domain.TaskQueue
	wsHub        *ws.Hub
	deviceUC     *DeviceUseCase
	monitorUC    *MonitorUseCase
	mu           sync.Mutex
	knownWorkers map[string]time.Time // deviceID -> last seen time
	interval     time.Duration

	// AI tiebreaker (optional). When aiArbiter is non-nil, the scheduler
	// asks it to pick between workers whose scores are within tiebreakEpsilon
	// of each other. aiCallBudget caps the arbiter calls per scheduling tick
	// so a flood of near-ties cannot blow past the tick deadline.
	//
	// alwaysConsultAI promotes the AI from a tiebreaker to the primary
	// scheduler: when true (or when a task's AISchedule pointer is true),
	// every eligible-worker decision is delegated to the arbiter, not just
	// rule-based ties. Per-task AISchedule overrides this default.
	aiArbiter       ai.TaskScheduler
	tiebreakEpsilon float64
	aiCallBudget    int
	aiCallTimeout   time.Duration
	alwaysConsultAI bool
}

func NewTaskSupervisor(taskQueue *domain.TaskQueue, wsHub *ws.Hub, deviceUC *DeviceUseCase, monitorUC *MonitorUseCase) *TaskSupervisor {
	return &TaskSupervisor{
		taskQueue:    taskQueue,
		wsHub:        wsHub,
		deviceUC:     deviceUC,
		monitorUC:    monitorUC,
		knownWorkers: make(map[string]time.Time),
		interval:     10 * time.Second,
	}
}

// SetAIArbiter enables AI tiebreaking between workers whose rule-based
// scores are within epsilon of each other (e.g. 0.10 = 10%). budget caps
// AI calls per scheduling tick; timeout caps an individual arbiter call.
// Passing a nil arbiter disables tiebreaking.
func (s *TaskSupervisor) SetAIArbiter(arbiter ai.TaskScheduler, epsilon float64, budget int, timeout time.Duration) {
	s.aiArbiter = arbiter
	s.tiebreakEpsilon = epsilon
	s.aiCallBudget = budget
	s.aiCallTimeout = timeout
}

// SetAlwaysConsultAI sets the server-wide default for whether every task
// scheduling decision goes through the AI arbiter. Per-task AISchedule
// (when non-nil) overrides this. The aiCallBudget still caps how many AI
// calls can fire per scheduling tick.
func (s *TaskSupervisor) SetAlwaysConsultAI(enable bool) {
	s.alwaysConsultAI = enable
}

// resolveAlwaysConsult returns whether AI scheduling should be invoked for
// task — task-level AISchedule wins over the supervisor default.
func (s *TaskSupervisor) resolveAlwaysConsult(task *domain.Task) bool {
	if task != nil && task.AISchedule != nil {
		return *task.AISchedule
	}
	return s.alwaysConsultAI
}

// ScheduleNow runs one scheduling pass immediately, outside the periodic
// ticker. Callers (e.g. the POST /api/tasks handler) use this to push a
// freshly enqueued task through the same AI-aware path the supervisor uses
// during its tick, instead of bypassing it with a separate immediate-assign
// codepath. Locks the same mutex as the periodic check.
func (s *TaskSupervisor) ScheduleNow(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scheduleQueue(ctx)
}

// Start begins the supervision loop
func (s *TaskSupervisor) Start(ctx context.Context) {
	log.Println("[supervisor] task supervisor started")
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[supervisor] task supervisor stopped")
			return
		case <-ticker.C:
			s.check(ctx)
		}
	}
}

func (s *TaskSupervisor) check(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Reassign tasks from disconnected workers back to the queue.
	if s.wsHub != nil {
		connectedDevices := make(map[string]bool)
		for _, id := range s.wsHub.ConnectedDevices() {
			connectedDevices[id] = true
			s.knownWorkers[id] = time.Now()
		}
		for deviceID, lastSeen := range s.knownWorkers {
			if !connectedDevices[deviceID] && time.Since(lastSeen) > 30*time.Second {
				assignedTasks := s.taskQueue.GetAssignedTasks(deviceID)
				if len(assignedTasks) > 0 {
					log.Printf("[supervisor] worker %s disconnected with %d assigned tasks, reassigning", deviceID, len(assignedTasks))
					reassigned := s.taskQueue.ReassignTasksFromDevice(deviceID)
					for _, task := range reassigned {
						log.Printf("[supervisor] task %s requeued (was on %s)", task.ID, deviceID)
					}
				}
				delete(s.knownWorkers, deviceID)
			}
		}
	}

	// 2. Timed-out running tasks flow back into the queue.
	timedOut := s.taskQueue.CheckTimeouts()
	for _, task := range timedOut {
		log.Printf("[supervisor] task %s timed out, requeued", task.ID)
	}

	// 3. Push-schedule everything currently queued onto the best worker.
	s.scheduleQueue(ctx)
}

// scheduleQueue walks the priority-ordered queue once and assigns each task
// to its highest-scoring eligible connected worker. Running-jobs counts are
// updated in the local snapshot slice so later tasks in the same tick see
// accurate load and spread across workers instead of piling on one.
func (s *TaskSupervisor) scheduleQueue(ctx context.Context) {
	if s.wsHub == nil || s.deviceUC == nil {
		return
	}
	connected := s.wsHub.ConnectedDevices()
	if len(connected) == 0 {
		return
	}
	snaps := make([]ai.WorkerSnapshot, 0, len(connected))
	for _, id := range connected {
		dev, err := s.deviceUC.GetDevice(ctx, id)
		if err != nil {
			continue
		}
		var metrics *domain.DeviceMetrics
		if s.monitorUC != nil {
			metrics = s.monitorUC.GetLatestCached(id)
		}
		running := len(s.taskQueue.GetAssignedTasks(id))
		snaps = append(snaps, buildWorkerSnapshot(dev, metrics, running))
	}
	if len(snaps) == 0 {
		return
	}

	aiCallsRemaining := s.aiCallBudget
	queued := s.taskQueue.ListQueuedByPriority()
	if len(queued) > 0 {
		log.Printf("[supervisor] tick: queued=%d snapshots=%d aiBudget=%d", len(queued), len(snaps), aiCallsRemaining)
	}
	for _, task := range queued {
		var best *ai.WorkerSnapshot
		alwaysAI := s.resolveAlwaysConsult(task) && s.aiArbiter != nil && aiCallsRemaining > 0
		if alwaysAI {
			log.Printf("[supervisor] task %s always-consult -> calling AI scheduler", task.ID)
			best = ai.ScheduleAlways(ctx, task, snaps, s.aiArbiter, s.aiCallTimeout)
			aiCallsRemaining--
			if best != nil {
				log.Printf("[supervisor] task %s AI picked %s", task.ID, best.DeviceID)
			} else {
				log.Printf("[supervisor] task %s AI returned nil (no eligible)", task.ID)
			}
		} else if s.aiArbiter != nil && aiCallsRemaining > 0 {
			tied := ai.PickTopKEligible(task, snaps, 5, s.tiebreakEpsilon)
			log.Printf("[supervisor] task %s eligible=%d", task.ID, len(tied))
			if len(tied) > 1 {
				log.Printf("[supervisor] task %s tied=%d -> calling AI tiebreaker", task.ID, len(tied))
				best = ai.ScheduleWithTiebreak(ctx, task, snaps, s.aiArbiter, s.tiebreakEpsilon, s.aiCallTimeout)
				aiCallsRemaining--
				if best != nil {
					log.Printf("[supervisor] task %s AI picked %s", task.ID, best.DeviceID)
				} else {
					log.Printf("[supervisor] task %s AI returned nil (fallback)", task.ID)
				}
			} else if len(tied) == 1 {
				w := tied[0]
				best = &w
			}
		} else {
			best = ai.PickBestWorker(task, snaps)
		}
		if best == nil {
			log.Printf("[supervisor] task %s: no worker selected", task.ID)
			continue
		}
		assigned := s.taskQueue.AssignToDevice(task.ID, best.DeviceID)
		if assigned == nil {
			continue // raced: another pass claimed it
		}
		log.Printf("[supervisor] task %s assigned to %s", task.ID, best.DeviceID)
		s.notifyDeviceOfTask(best.DeviceID, assigned)
		bumpRunningJobs(snaps, best.DeviceID)
	}
}

func buildWorkerSnapshot(dev *domain.Device, metrics *domain.DeviceMetrics, runningJobs int) ai.WorkerSnapshot {
	snap := ai.WorkerSnapshot{
		DeviceID:     dev.ID,
		Capabilities: dev.Capabilities,
		RunningJobs:  runningJobs,
		GPUCount:     dev.GPUCount,
	}
	if metrics == nil || metrics.HasError() {
		return snap
	}
	snap.CPUUsage = metrics.CPU.UsagePercent
	snap.MemoryFreeGB = float64(metrics.Memory.Free) / (1024 * 1024 * 1024)
	if metrics.GPU != nil && len(metrics.GPU.GPUs) > 0 {
		var utilSum float64
		var freeBytes uint64
		for _, g := range metrics.GPU.GPUs {
			utilSum += g.UsagePercent
			freeBytes += g.MemoryFree
		}
		snap.GPUUtilization = utilSum / float64(len(metrics.GPU.GPUs))
		snap.GPUMemoryFreeMB = int(freeBytes / (1024 * 1024))
	}
	return snap
}

func bumpRunningJobs(snaps []ai.WorkerSnapshot, deviceID string) {
	for i := range snaps {
		if snaps[i].DeviceID == deviceID {
			snaps[i].RunningJobs++
			return
		}
	}
}

// notifyDeviceOfTask sends a task assignment notification via WebSocket
func (s *TaskSupervisor) notifyDeviceOfTask(deviceID string, task *domain.Task) {
	if s.wsHub == nil {
		return
	}

	payload, err := json.Marshal(task)
	if err != nil {
		return
	}

	msg := &ws.Message{
		Type:      ws.MsgTaskAssign,
		DeviceID:  deviceID,
		TaskID:    task.ID,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	if err := s.wsHub.SendToDevice(deviceID, msg); err != nil {
		log.Printf("[supervisor] failed to send task %s to %s: %v", task.ID, deviceID, err)
	}
}
