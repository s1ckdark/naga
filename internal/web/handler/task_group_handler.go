package handler

import (
	"database/sql"
	"errors"
	"net/http"

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
