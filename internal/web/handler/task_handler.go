package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	"github.com/s1ckdark/hydra/internal/domain"
	"github.com/s1ckdark/hydra/internal/web/ws"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now (Tailscale provides network-level auth)
	},
}

// HandleWebSocket upgrades HTTP to WebSocket connection
func (h *Handler) HandleWebSocket(c echo.Context) error {
	deviceID := c.QueryParam("device_id")
	if deviceID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "device_id required"})
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}

	client := &ws.Client{
		Hub:      h.wsHub,
		Conn:     conn,
		DeviceID: deviceID,
		Send:     make(chan []byte, 256),
	}

	h.wsHub.Register(client)

	go client.WritePump()
	go client.ReadPump()

	return nil
}

// APITaskCreate creates a new task
func (h *Handler) APITaskCreate(c echo.Context) error {
	if h.taskQueue == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "task queue not available"})
	}

	var req struct {
		Type                 string                 `json:"type"`
		Priority             string                 `json:"priority"`
		RequiredCapabilities []string               `json:"requiredCapabilities"`
		PreferredDeviceID    string                 `json:"preferredDeviceId"`
		Payload              map[string]interface{} `json:"payload"`
		Timeout              int                    `json:"timeout"` // seconds
		MaxRetries           int                    `json:"maxRetries"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	if req.Type == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "type is required"})
	}

	task := &domain.Task{
		ID:                   generateID(),
		Type:                 req.Type,
		Status:               domain.TaskStatusPending,
		Priority:             domain.TaskPriority(req.Priority),
		RequiredCapabilities: req.RequiredCapabilities,
		PreferredDeviceID:    req.PreferredDeviceID,
		Payload:              req.Payload,
		Timeout:              time.Duration(req.Timeout) * time.Second,
		MaxRetries:           req.MaxRetries,
	}

	if task.Priority == "" {
		task.Priority = domain.TaskPriorityNormal
	}

	h.taskQueue.Enqueue(task)

	// Run one scheduling pass immediately so we don't wait up to a tick
	// interval for the supervisor to pick this task up. The supervisor's
	// scheduleQueue is the single source of scheduling truth — capability
	// matching, metric-based scoring, and AI tiebreaking — so all task
	// assignments share the same code path whether they arrive via POST
	// or are leftovers from a previous tick.
	if h.taskSupervisor != nil {
		h.taskSupervisor.ScheduleNow(c.Request().Context())
	}

	// Refresh the task from the queue: ScheduleNow may have moved it from
	// queued to assigned, and we want the response to reflect that.
	if updated := h.taskQueue.Get(task.ID); updated != nil {
		task = updated
	}

	return c.JSON(http.StatusCreated, task)
}

// APITaskList lists tasks with optional status filter
func (h *Handler) APITaskList(c echo.Context) error {
	if h.taskQueue == nil {
		return c.JSON(http.StatusOK, []interface{}{})
	}

	status := c.QueryParam("status")
	deviceID := c.QueryParam("device_id")

	var tasks []*domain.Task
	if deviceID != "" {
		tasks = h.taskQueue.ListByDevice(deviceID)
	} else if status != "" {
		tasks = h.taskQueue.ListByStatus(domain.TaskStatus(status))
	} else {
		// Return all tasks - collect from all statuses
		for _, s := range []domain.TaskStatus{
			domain.TaskStatusQueued, domain.TaskStatusAssigned,
			domain.TaskStatusRunning, domain.TaskStatusCompleted,
			domain.TaskStatusFailed, domain.TaskStatusCancelled,
		} {
			tasks = append(tasks, h.taskQueue.ListByStatus(s)...)
		}
	}

	if tasks == nil {
		tasks = []*domain.Task{}
	}

	return c.JSON(http.StatusOK, tasks)
}

// APITaskDetail returns task details
func (h *Handler) APITaskDetail(c echo.Context) error {
	if h.taskQueue == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "task queue not available"})
	}

	task := h.taskQueue.Get(c.Param("id"))
	if task == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "task not found"})
	}

	return c.JSON(http.StatusOK, task)
}

// APITaskUpdateStatus updates a task's status
func (h *Handler) APITaskUpdateStatus(c echo.Context) error {
	if h.taskQueue == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "task queue not available"})
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	task := h.taskQueue.UpdateStatus(c.Param("id"), domain.TaskStatus(req.Status))
	if task == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "task not found"})
	}

	return c.JSON(http.StatusOK, task)
}

// APITaskSetResult sets a task's result
func (h *Handler) APITaskSetResult(c echo.Context) error {
	if h.taskQueue == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "task queue not available"})
	}

	var result domain.TaskResult
	if err := c.Bind(&result); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	task := h.taskQueue.SetResult(c.Param("id"), &result)
	if task == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "task not found"})
	}

	return c.JSON(http.StatusOK, task)
}

// APIRegisterCapabilities registers device capabilities
func (h *Handler) APIRegisterCapabilities(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.deviceUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "device service not available"})
	}

	var req struct {
		Capabilities []string `json:"capabilities"`
		DeviceToken  string   `json:"deviceToken"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	device, err := h.deviceUC.GetDevice(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "device not found"})
	}

	device.Capabilities = req.Capabilities
	if req.DeviceToken != "" {
		device.DeviceToken = req.DeviceToken
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"deviceId":     device.ID,
		"capabilities": device.Capabilities,
	})
}

// APIGetCapabilities returns device capabilities
func (h *Handler) APIGetCapabilities(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.deviceUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "device service not available"})
	}

	device, err := h.deviceUC.GetDevice(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "device not found"})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"deviceId":     device.ID,
		"capabilities": device.Capabilities,
	})
}

// generateID generates a simple unique ID
func generateID() string {
	return time.Now().Format("20060102150405") + "-" + randomHex(8)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
