package domain

import "time"

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusAssigned  TaskStatus = "assigned"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

type TaskPriority string

const (
	TaskPriorityLow    TaskPriority = "low"
	TaskPriorityNormal TaskPriority = "normal"
	TaskPriorityHigh   TaskPriority = "high"
	TaskPriorityUrgent TaskPriority = "urgent"
)

// Task represents a unit of work to be executed on a capable node
type Task struct {
	ID                   string                 `json:"id"`
	ParentID             string                 `json:"parentId,omitempty"`        // for sub-tasks split by head
	ClusterID            string                 `json:"clusterId,omitempty"`
	Type                 string                 `json:"type"`                      // "command", "gps", "camera", "sms", "phone", "sensor"
	Status               TaskStatus             `json:"status"`
	Priority             TaskPriority           `json:"priority"`
	RequiredCapabilities []string               `json:"requiredCapabilities"`      // capabilities needed to run this task
	PreferredDeviceID    string                 `json:"preferredDeviceId,omitempty"`
	AssignedDeviceID     string                 `json:"assignedDeviceId,omitempty"`
	Payload              map[string]interface{} `json:"payload"`                   // task-specific data
	Result               *TaskResult            `json:"result,omitempty"`
	Error                string                 `json:"error,omitempty"`
	CreatedAt            time.Time              `json:"createdAt"`
	AssignedAt           *time.Time             `json:"assignedAt,omitempty"`
	StartedAt            *time.Time             `json:"startedAt,omitempty"`
	CompletedAt          *time.Time             `json:"completedAt,omitempty"`
	Timeout              time.Duration          `json:"timeout,omitempty"`         // max execution time
	RetryCount           int                    `json:"retryCount"`
	MaxRetries           int                    `json:"maxRetries"`
	CreatedBy            string                 `json:"createdBy,omitempty"`       // device ID of creator/head
}

// TaskResult holds the output of a completed task
type TaskResult struct {
	DeviceID   string                 `json:"deviceId"`
	DeviceName string                 `json:"deviceName"`
	Output     map[string]interface{} `json:"output"`    // flexible output (GPS coords, image URL, command stdout, etc.)
	Duration   time.Duration          `json:"durationMs"`
}

// IsTerminal returns true if the task is in a final state
func (t *Task) IsTerminal() bool {
	return t.Status == TaskStatusCompleted || t.Status == TaskStatusFailed || t.Status == TaskStatusCancelled
}

// CanRetry returns true if the task can be retried
func (t *Task) CanRetry() bool {
	return t.Status == TaskStatusFailed && t.RetryCount < t.MaxRetries
}
