package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/s1ckdark/hydra/internal/domain"
)

// APIGetGroup returns a TaskGroupSnapshot. With ?detail=full the response
// also embeds every member task; the default lightweight response only
// includes counters and derived status.
func (h *Handler) APIGetGroup(c echo.Context) error {
	if h.taskGroupRepo == nil || h.taskGroupTasks == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "task group service not available"})
	}
	id := c.Param("id")
	ctx := c.Request().Context()

	group, err := h.taskGroupRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "group not found"})
		}
		return internalError(c, "failed to load group", err)
	}

	tasks, err := h.taskGroupTasks.GetByGroup(ctx, id)
	if err != nil {
		return internalError(c, "failed to load group tasks", err)
	}

	snap := buildGroupSnapshot(group, tasks)
	if c.QueryParam("detail") != "full" {
		snap.Tasks = nil
	}
	return c.JSON(http.StatusOK, snap)
}

func buildGroupSnapshot(g *domain.TaskGroup, tasks []*domain.Task) domain.TaskGroupSnapshot {
	snap := domain.TaskGroupSnapshot{TaskGroup: *g, Tasks: tasks}
	for _, t := range tasks {
		switch t.Status {
		case domain.TaskStatusCompleted:
			snap.Completed++
		case domain.TaskStatusFailed, domain.TaskStatusCancelled:
			snap.Failed++
		case domain.TaskStatusRunning:
			snap.Running++
		case domain.TaskStatusQueued, domain.TaskStatusAssigned, domain.TaskStatusPending:
			snap.Queued++
		}
	}
	snap.Status = domain.DeriveGroupStatus(tasks, g.TotalTasks)
	return snap
}

// taskBatchRequest is the inbound JSON for POST /api/tasks/batch.
type taskBatchRequest struct {
	Name     string                 `json:"name"`
	Metadata map[string]interface{} `json:"metadata"`
	Tasks    []taskBatchEntry       `json:"tasks"`
}

type taskBatchEntry struct {
	Type                 string                 `json:"type"`
	Priority             string                 `json:"priority"`
	RequiredCapabilities []string               `json:"requiredCapabilities"`
	PreferredDeviceID    string                 `json:"preferredDeviceId"`
	Payload              map[string]interface{} `json:"payload"`
	Timeout              int                    `json:"timeout"`
	MaxRetries           int                    `json:"maxRetries"`
	AISchedule           *bool                  `json:"aiSchedule"`
}

// APITaskBatchCreate creates a TaskGroup and N tasks in a single API call.
// Validation runs first; on any failure (empty list, missing task type) no
// group row is written and no task is enqueued. There is no DB transaction
// — durability across a mid-flight server crash is out of scope (queue is
// in-memory). After successful enqueue the supervisor's ScheduleNow is
// invoked once so the whole batch is considered for placement in a single
// pass rather than N separate calls.
func (h *Handler) APITaskBatchCreate(c echo.Context) error {
	if h.taskQueue == nil || h.taskGroupSaver == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "task batch service not available"})
	}

	var req taskBatchRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if len(req.Tasks) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "tasks: must contain at least one task"})
	}

	for i, e := range req.Tasks {
		if e.Type == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("tasks[%d]: type is required", i),
			})
		}
	}

	ctx := c.Request().Context()
	now := time.Now()

	group := &domain.TaskGroup{
		ID:         generateID(),
		Name:       req.Name,
		CreatedAt:  now,
		TotalTasks: len(req.Tasks),
		Metadata:   req.Metadata,
	}
	if err := h.taskGroupSaver.Save(ctx, group); err != nil {
		return internalError(c, "failed to save group", err)
	}

	created := make([]*domain.Task, 0, len(req.Tasks))
	for _, e := range req.Tasks {
		task := &domain.Task{
			ID:                   generateID(),
			Type:                 e.Type,
			Status:               domain.TaskStatusPending,
			Priority:             domain.TaskPriority(e.Priority),
			RequiredCapabilities: e.RequiredCapabilities,
			PreferredDeviceID:    e.PreferredDeviceID,
			Payload:              e.Payload,
			Timeout:              time.Duration(e.Timeout) * time.Second,
			MaxRetries:           e.MaxRetries,
			AISchedule:           e.AISchedule,
			GroupID:              group.ID,
		}
		if task.Priority == "" {
			task.Priority = domain.TaskPriorityNormal
		}
		h.taskQueue.Enqueue(task)
		created = append(created, task)
	}

	if h.taskSupervisor != nil {
		h.taskSupervisor.ScheduleNow(ctx)
	}

	// Refresh state from queue (some may have been assigned by ScheduleNow).
	for i, t := range created {
		if updated := h.taskQueue.Get(t.ID); updated != nil {
			created[i] = updated
		}
	}

	snap := domain.TaskGroupSnapshot{
		TaskGroup: *group,
		Tasks:     created,
	}
	for _, t := range created {
		switch t.Status {
		case domain.TaskStatusCompleted:
			snap.Completed++
		case domain.TaskStatusFailed, domain.TaskStatusCancelled:
			snap.Failed++
		case domain.TaskStatusRunning:
			snap.Running++
		default:
			snap.Queued++
		}
	}
	snap.Status = domain.DeriveGroupStatus(created, group.TotalTasks)

	return c.JSON(http.StatusCreated, snap)
}
