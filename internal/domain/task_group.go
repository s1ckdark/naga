package domain

import "time"

// TaskGroupStatus is the derived aggregate status of a TaskGroup.
type TaskGroupStatus string

const (
	TaskGroupStatusRunning   TaskGroupStatus = "running"
	TaskGroupStatusCompleted TaskGroupStatus = "completed"
	TaskGroupStatusPartial   TaskGroupStatus = "partial"
	TaskGroupStatusFailed    TaskGroupStatus = "failed"
)

// TaskGroup is the immutable identity of a fan-out batch. Aggregate progress
// is computed at read time from member tasks — there is no counter on the
// group to drift out of sync.
type TaskGroup struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name,omitempty"`
	CreatedAt  time.Time              `json:"createdAt"`
	CreatedBy  string                 `json:"createdBy,omitempty"`
	TotalTasks int                    `json:"totalTasks"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// TaskGroupSnapshot is the response shape of GET /api/groups/:id — group
// identity plus derived progress and (optionally) member tasks.
type TaskGroupSnapshot struct {
	TaskGroup
	Status    TaskGroupStatus `json:"status"`
	Completed int             `json:"completed"`
	Failed    int             `json:"failed"`
	Running   int             `json:"running"`
	Queued    int             `json:"queued"`
	Tasks     []*Task         `json:"tasks,omitempty"`
}

// DeriveGroupStatus computes the group's aggregate status from its member
// tasks. `total` is the original batch size; if fewer tasks are passed the
// missing ones are treated as still running (the safest interpretation).
// Cancelled tasks count as failed for the group's purposes.
func DeriveGroupStatus(tasks []*Task, total int) TaskGroupStatus {
	var completed, failed, terminal int
	for _, t := range tasks {
		switch t.Status {
		case TaskStatusCompleted:
			completed++
			terminal++
		case TaskStatusFailed, TaskStatusCancelled:
			failed++
			terminal++
		}
	}
	if terminal < total {
		return TaskGroupStatusRunning
	}
	if failed == 0 {
		return TaskGroupStatusCompleted
	}
	if completed == 0 {
		return TaskGroupStatusFailed
	}
	return TaskGroupStatusPartial
}
