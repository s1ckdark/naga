package usecase

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/dave/naga/internal/domain"
	"github.com/dave/naga/internal/web/ws"
)

// TaskSupervisor monitors worker health and reassigns tasks from failed workers
type TaskSupervisor struct {
	taskQueue    *domain.TaskQueue
	wsHub        *ws.Hub
	deviceUC     *DeviceUseCase
	mu           sync.Mutex
	knownWorkers map[string]time.Time // deviceID -> last seen time
	interval     time.Duration
}

func NewTaskSupervisor(taskQueue *domain.TaskQueue, wsHub *ws.Hub, deviceUC *DeviceUseCase) *TaskSupervisor {
	return &TaskSupervisor{
		taskQueue:    taskQueue,
		wsHub:        wsHub,
		deviceUC:     deviceUC,
		knownWorkers: make(map[string]time.Time),
		interval:     10 * time.Second,
	}
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

	// 1. Check for disconnected WebSocket workers with assigned tasks
	if s.wsHub != nil {
		connectedDevices := make(map[string]bool)
		for _, id := range s.wsHub.ConnectedDevices() {
			connectedDevices[id] = true
			s.knownWorkers[id] = time.Now()
		}

		// Find workers that were known but are no longer connected
		for deviceID, lastSeen := range s.knownWorkers {
			if !connectedDevices[deviceID] && time.Since(lastSeen) > 30*time.Second {
				// Worker disconnected - check for assigned tasks
				assignedTasks := s.taskQueue.GetAssignedTasks(deviceID)
				if len(assignedTasks) > 0 {
					log.Printf("[supervisor] worker %s disconnected with %d assigned tasks, reassigning", deviceID, len(assignedTasks))
					reassigned := s.taskQueue.ReassignTasksFromDevice(deviceID)
					for _, task := range reassigned {
						log.Printf("[supervisor] task %s reassigned to queue (was on %s)", task.ID, deviceID)
						s.tryAssignTask(ctx, task)
					}
				}
				delete(s.knownWorkers, deviceID)
			}
		}
	}

	// 2. Check for timed-out tasks
	timedOut := s.taskQueue.CheckTimeouts()
	for _, task := range timedOut {
		log.Printf("[supervisor] task %s timed out, reassigning", task.ID)
		s.tryAssignTask(ctx, task)
	}
}

// tryAssignTask attempts to assign a queued task to a connected device
func (s *TaskSupervisor) tryAssignTask(ctx context.Context, task *domain.Task) {
	if s.wsHub == nil || s.deviceUC == nil {
		return
	}

	for _, connDeviceID := range s.wsHub.ConnectedDevices() {
		dev, err := s.deviceUC.GetDevice(ctx, connDeviceID)
		if err != nil {
			continue
		}

		matched := s.taskQueue.FindMatchingTask(dev)
		if matched != nil && matched.ID == task.ID {
			s.notifyDeviceOfTask(connDeviceID, matched)
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
