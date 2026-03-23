package domain

import (
	"sync"
	"time"
)

// TaskQueue manages task lifecycle and capability-based matching
type TaskQueue struct {
	mu    sync.RWMutex
	tasks map[string]*Task // taskID -> task
	queue []*Task          // pending tasks ordered by priority/time
}

func NewTaskQueue() *TaskQueue {
	return &TaskQueue{
		tasks: make(map[string]*Task),
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

// Get returns a task by ID
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
