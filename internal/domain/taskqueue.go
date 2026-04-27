package domain

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// TaskQueue manages task lifecycle and capability-based matching.
//
// Mutating methods must call q.persist(task) under q.mu after the
// in-memory mutation. Adding a new mutation method? Wire persist in.
type TaskQueue struct {
	mu    sync.RWMutex
	tasks map[string]*Task // taskID -> task
	queue []*Task          // pending tasks ordered by priority/time
	repo  TaskRepository   // optional write-through; nil disables persistence
}

func NewTaskQueue() *TaskQueue {
	return &TaskQueue{
		tasks: make(map[string]*Task),
	}
}

// WithRepo enables write-through persistence on this queue. Returns the
// queue for fluent chaining at construction sites.
//
// Intended to be called once at construction, before the queue is shared
// with goroutines. The mutex acquisition here is defensive only — concurrent
// repo swaps at runtime are not supported because persist reads q.repo
// without locking (see persist's contract).
func (q *TaskQueue) WithRepo(r TaskRepository) *TaskQueue {
	q.mu.Lock()
	q.repo = r
	q.mu.Unlock()
	return q
}

// persist writes a task to the repo if one is configured. Errors are logged
// and swallowed — DB downtime must not stall the in-memory scheduler.
//
// Caller must hold q.mu (Lock or RLock). The implementation reads q.repo
// without acquiring the lock, so callers from outside an already-locked
// section would race with WithRepo.
func (q *TaskQueue) persist(t *Task) {
	if q.repo == nil || t == nil {
		return
	}
	if err := q.repo.Save(context.Background(), t); err != nil {
		log.Printf("[taskqueue] persist failed for %s: %v", t.ID, err)
	}
}

// Enqueue adds a task to the queue
func (q *TaskQueue) Enqueue(task *Task) {
	q.mu.Lock()
	defer q.mu.Unlock()
	task.Status = TaskStatusQueued
	task.CreatedAt = time.Now()
	q.tasks[task.ID] = task
	q.insertByPriority(task)
	q.persist(task)
}

// insertByPriority inserts task maintaining priority order (urgent > high > normal > low)
func (q *TaskQueue) insertByPriority(task *Task) {
	pri := priorityValue(task.Priority)
	for i, t := range q.queue {
		if priorityValue(t.Priority) < pri {
			q.queue = append(q.queue[:i+1], q.queue[i:]...)
			q.queue[i] = task
			return
		}
	}
	q.queue = append(q.queue, task)
}

func priorityValue(p TaskPriority) int {
	switch p {
	case TaskPriorityUrgent:
		return 4
	case TaskPriorityHigh:
		return 3
	case TaskPriorityNormal:
		return 2
	case TaskPriorityLow:
		return 1
	default:
		return 2
	}
}

// FindMatchingTask finds the highest priority task that matches the device's capabilities
func (q *TaskQueue) FindMatchingTask(device *Device) *Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, task := range q.queue {
		if task.Status != TaskStatusQueued {
			continue
		}
		if task.PreferredDeviceID != "" && task.PreferredDeviceID != device.ID {
			continue
		}
		if q.deviceMatchesTask(device, task) {
			// Remove from queue
			q.queue = append(q.queue[:i], q.queue[i+1:]...)
			// Assign
			now := time.Now()
			task.Status = TaskStatusAssigned
			task.AssignedDeviceID = device.ID
			task.AssignedAt = &now
			q.persist(task)
			return task
		}
	}
	return nil
}

// deviceMatchesTask checks if device has all required capabilities
func (q *TaskQueue) deviceMatchesTask(device *Device, task *Task) bool {
	for _, req := range task.RequiredCapabilities {
		if !device.HasCapability(req) {
			return false
		}
	}
	return true
}

// Get returns a task by ID, or nil if not found.
//
// The returned pointer aliases the queue's internal storage. Callers MUST
// treat it as read-only — mutating fields on the returned task bypasses
// the queue's mutex and the write-through repository, leaving in-memory
// state and the persisted `tasks` row out of sync. Use UpdateStatus,
// SetResult, AssignToDevice, or one of the other documented mutation
// methods to change a task's state.
//
// If a defensive copy is needed (e.g. building a response that callers
// might mutate), copy the fields you need at the call site.
func (q *TaskQueue) Get(taskID string) *Task {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.tasks[taskID]
}

// UpdateStatus updates a task's status
func (q *TaskQueue) UpdateStatus(taskID string, status TaskStatus) *Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	task, ok := q.tasks[taskID]
	if !ok {
		return nil
	}
	task.Status = status
	now := time.Now()
	switch status {
	case TaskStatusRunning:
		task.StartedAt = &now
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		task.CompletedAt = &now
	}
	q.persist(task)
	return task
}

// SetResult sets the task result
func (q *TaskQueue) SetResult(taskID string, result *TaskResult) *Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	task, ok := q.tasks[taskID]
	if !ok {
		return nil
	}
	task.Result = result
	task.Status = TaskStatusCompleted
	now := time.Now()
	task.CompletedAt = &now
	q.persist(task)
	return task
}

// ListByStatus returns tasks filtered by status
func (q *TaskQueue) ListByStatus(status TaskStatus) []*Task {
	q.mu.RLock()
	defer q.mu.RUnlock()
	var result []*Task
	for _, t := range q.tasks {
		if t.Status == status {
			result = append(result, t)
		}
	}
	return result
}

// ListByDevice returns tasks assigned to a device
func (q *TaskQueue) ListByDevice(deviceID string) []*Task {
	q.mu.RLock()
	defer q.mu.RUnlock()
	var result []*Task
	for _, t := range q.tasks {
		if t.AssignedDeviceID == deviceID {
			result = append(result, t)
		}
	}
	return result
}

// PendingCount returns number of queued tasks
func (q *TaskQueue) PendingCount() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	count := 0
	for _, t := range q.queue {
		if t.Status == TaskStatusQueued {
			count++
		}
	}
	return count
}

// ReassignTasksFromDevice moves all non-terminal tasks from a failed device back to the queue
func (q *TaskQueue) ReassignTasksFromDevice(deviceID string) []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	var reassigned []*Task
	for _, task := range q.tasks {
		if task.AssignedDeviceID == deviceID && !task.IsTerminal() {
			task.RetryCount++
			if task.RetryCount > task.MaxRetries {
				now := time.Now()
				task.Status = TaskStatusFailed
				task.CompletedAt = &now
				task.Error = fmt.Sprintf("worker %s failed, max retries exceeded", deviceID)
				q.persist(task)
			} else {
				task.BlockedDeviceIDs = appendUnique(task.BlockedDeviceIDs, deviceID)
				task.Status = TaskStatusQueued
				task.AssignedDeviceID = ""
				task.AssignedAt = nil
				task.StartedAt = nil
				task.Error = fmt.Sprintf("worker %s failed, reassigning (retry %d/%d)", deviceID, task.RetryCount, task.MaxRetries)
				q.insertByPriority(task)
				reassigned = append(reassigned, task)
				q.persist(task)
			}
		}
	}
	return reassigned
}

// ListQueuedByPriority returns a snapshot of tasks currently in TaskStatusQueued,
// already ordered by priority (insertByPriority maintains ordering on enqueue).
// The returned slice is a copy; mutating it does not affect the queue.
func (q *TaskQueue) ListQueuedByPriority() []*Task {
	q.mu.RLock()
	defer q.mu.RUnlock()
	result := make([]*Task, 0, len(q.queue))
	for _, t := range q.queue {
		if t.Status == TaskStatusQueued {
			result = append(result, t)
		}
	}
	return result
}

// AssignToDevice atomically removes a queued task from the queue and marks it
// assigned to deviceID. Returns nil if the task is unknown or no longer queued
// (e.g. another scheduler pass already claimed it).
func (q *TaskQueue) AssignToDevice(taskID, deviceID string) *Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	task, ok := q.tasks[taskID]
	if !ok || task.Status != TaskStatusQueued {
		return nil
	}
	for i, t := range q.queue {
		if t.ID == taskID {
			q.queue = append(q.queue[:i], q.queue[i+1:]...)
			break
		}
	}
	now := time.Now()
	task.Status = TaskStatusAssigned
	task.AssignedDeviceID = deviceID
	task.AssignedAt = &now
	q.persist(task)
	return task
}

// appendUnique appends id to list only if not already present.
func appendUnique(list []string, id string) []string {
	for _, existing := range list {
		if existing == id {
			return list
		}
	}
	return append(list, id)
}

// WorkerCandidate wraps a Device with current resource metrics for AI scheduling.
type WorkerCandidate struct {
	Device         *Device
	GPUUtilization float64
	MemoryFreeGB   float64
	CPUUsage       float64
	RunningJobs    int
}

// ScheduleFunc is called by FindMatchingTaskWithAI to pick the best worker for a task.
// It returns the device ID that should run the task, or an error.
type ScheduleFunc func(task *Task, workers []WorkerCandidate) (deviceID string, err error)

// FindMatchingTaskWithAI finds a task for the given device using an optional AI scheduler.
// It filters by capability first; if schedule is non-nil it calls it to confirm placement.
// Falls back to FindMatchingTask behaviour if schedule is nil or returns a different device.
func (q *TaskQueue) FindMatchingTaskWithAI(device *Device, candidates []WorkerCandidate, schedule ScheduleFunc) *Task {
	if schedule == nil {
		return q.FindMatchingTask(device)
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	for i, task := range q.queue {
		if task.Status != TaskStatusQueued {
			continue
		}
		if task.PreferredDeviceID != "" && task.PreferredDeviceID != device.ID {
			continue
		}
		if !q.deviceMatchesTask(device, task) {
			continue
		}
		// Ask AI which worker should run this task.
		chosen, err := schedule(task, candidates)
		if err != nil || chosen != device.ID {
			continue
		}
		// Remove from queue and assign.
		q.queue = append(q.queue[:i], q.queue[i+1:]...)
		now := time.Now()
		task.Status = TaskStatusAssigned
		task.AssignedDeviceID = device.ID
		task.AssignedAt = &now
		q.persist(task)
		return task
	}
	return nil
}

// GetAssignedTasks returns all tasks currently assigned to a device
func (q *TaskQueue) GetAssignedTasks(deviceID string) []*Task {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var tasks []*Task
	for _, t := range q.tasks {
		if t.AssignedDeviceID == deviceID && (t.Status == TaskStatusAssigned || t.Status == TaskStatusRunning) {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// CheckTimeouts moves timed-out tasks back to queued status
func (q *TaskQueue) CheckTimeouts() []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	var timedOut []*Task
	now := time.Now()

	for _, task := range q.tasks {
		if task.Status == TaskStatusRunning && task.Timeout > 0 && task.StartedAt != nil {
			if now.Sub(*task.StartedAt) > task.Timeout {
				task.RetryCount++
				if task.RetryCount > task.MaxRetries {
					task.Status = TaskStatusFailed
					task.CompletedAt = &now
					task.Error = "task timed out, max retries exceeded"
					q.persist(task)
				} else {
					if task.AssignedDeviceID != "" {
						task.BlockedDeviceIDs = appendUnique(task.BlockedDeviceIDs, task.AssignedDeviceID)
					}
					task.Status = TaskStatusQueued
					task.AssignedDeviceID = ""
					task.AssignedAt = nil
					task.StartedAt = nil
					task.Error = fmt.Sprintf("task timed out (retry %d/%d)", task.RetryCount, task.MaxRetries)
					q.insertByPriority(task)
					timedOut = append(timedOut, task)
					q.persist(task)
				}
			}
		}
	}
	return timedOut
}
