package handler

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/s1ckdark/hydra/config"
	"github.com/s1ckdark/hydra/internal/domain"
	"github.com/s1ckdark/hydra/internal/usecase"
	"github.com/s1ckdark/hydra/internal/web/ws"
)

// RemoteExecutor executes commands on remote devices
type RemoteExecutor interface {
	Execute(ctx context.Context, device *domain.Device, command string) (string, error)
}

// TaskResult holds the result of a distributed task execution on one node
type TaskResult struct {
	DeviceID   string  `json:"deviceId"`
	DeviceName string  `json:"deviceName"`
	GPU        string  `json:"gpu"`
	Output     string  `json:"output"`
	Error      string  `json:"error,omitempty"`
	Duration   float64 `json:"durationMs"`
}

// esc escapes a string for safe HTML output
func esc(s string) string {
	return html.EscapeString(s)
}

// internalError logs the full error and returns a generic message to the client.
func internalError(c echo.Context, msg string, err error) error {
	log.Printf("internal error: %s: %v", msg, err)
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": msg})
}

// taskGroupReader is the subset of repository.TaskGroupRepository the
// /api/groups endpoint needs. Local interface so handler tests can supply
// stubs without pulling sqlite into the test build graph.
type taskGroupReader interface {
	GetByID(ctx context.Context, id string) (*domain.TaskGroup, error)
}

// taskGroupSaver is the write-side of TaskGroupRepository.
type taskGroupSaver interface {
	Save(ctx context.Context, group *domain.TaskGroup) error
}

// Handler handles HTTP requests
type Handler struct {
	deviceUC           *usecase.DeviceUseCase
	orchUC             *usecase.OrchUseCase
	monitorUC          *usecase.MonitorUseCase
	failoverUC         *usecase.FailoverUseCase
	executor           RemoteExecutor
	cfg                *config.Config
	wsHub              *ws.Hub
	taskQueue          *domain.TaskQueue
	taskSupervisor     *usecase.TaskSupervisor
	taskGroupRepo      taskGroupReader
	taskGroupTasks     domain.TaskRepository
	taskGroupSaver     taskGroupSaver
	aiArbiterRebuilder func(ai config.AIConfig)
}

// NewHandler creates a new Handler
func NewHandler(
	deviceUC *usecase.DeviceUseCase,
	orchUC *usecase.OrchUseCase,
	monitorUC *usecase.MonitorUseCase,
	failoverUC *usecase.FailoverUseCase,
	cfg *config.Config,
) *Handler {
	return &Handler{
		deviceUC:   deviceUC,
		orchUC:  orchUC,
		monitorUC:  monitorUC,
		failoverUC: failoverUC,
		cfg:        cfg,
	}
}

// SetExecutor sets the remote executor for distributed task execution
func (h *Handler) SetExecutor(executor RemoteExecutor) {
	h.executor = executor
}

// SetWebSocketHub sets the WebSocket hub
func (h *Handler) SetWebSocketHub(hub *ws.Hub) {
	h.wsHub = hub
}

// SetTaskQueue sets the task queue
func (h *Handler) SetTaskQueue(queue *domain.TaskQueue) {
	h.taskQueue = queue
}

// SetTaskSupervisor wires the task supervisor so APITaskCreate can route
// freshly enqueued tasks through the AI-aware scheduler instead of the
// legacy immediate-assign path.
func (h *Handler) SetTaskSupervisor(s *usecase.TaskSupervisor) {
	h.taskSupervisor = s
}

// SetAIArbiterRebuilder wires a callback the handler invokes after
// PUT /api/config/ai persists the new config so the running supervisor
// picks up provider/key/endpoint changes without a process restart.
// Pass nil to disable; if nil the supervisor's arbiter stays at boot config.
func (h *Handler) SetAIArbiterRebuilder(rebuild func(ai config.AIConfig)) {
	h.aiArbiterRebuilder = rebuild
}

// SetTaskGroupRepos wires the read and write dependencies for /api/groups
// and /api/tasks/batch. The concrete type passed as g must implement both
// taskGroupReader and taskGroupSaver (e.g. *sqlite.TaskGroupRepository).
func (h *Handler) SetTaskGroupRepos(g interface {
	taskGroupReader
	taskGroupSaver
}, t domain.TaskRepository) {
	h.taskGroupRepo = g
	h.taskGroupSaver = g
	h.taskGroupTasks = t
}

// Dashboard renders the main dashboard
func (h *Handler) Dashboard(c echo.Context) error {
	ctx := c.Request().Context()

	// Gather stats
	var onlineCount, totalCount, gpuNodeCount, totalGPUs, orchCount, orchRunning int
	type gpuDevice struct {
		ID, Name, IP, GPUModel string
		GPUCount               int
		Online                 bool
	}
	var gpuDevices []gpuDevice

	if h.deviceUC != nil {
		if devices, err := h.deviceUC.ListDevices(ctx, false); err == nil {
			totalCount = len(devices)
			for _, d := range devices {
				if d.IsOnline() {
					onlineCount++
				}
				if d.HasGPU {
					gpuNodeCount++
					totalGPUs += d.GPUCount
					gpuDevices = append(gpuDevices, gpuDevice{
						ID: d.ID, Name: d.GetDisplayName(), IP: d.TailscaleIP,
						GPUModel: d.GPUModel, GPUCount: d.GPUCount, Online: d.IsOnline(),
					})
				}
			}
		}
	}
	if h.orchUC != nil {
		if orchs, err := h.orchUC.ListOrchs(ctx); err == nil {
			orchCount = len(orchs)
			for _, o := range orchs {
				if o.IsRunning() {
					orchRunning++
				}
			}
		}
	}

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html>
<head>
	<title>Naga</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<div class="flex justify-between items-center">
				<h1 class="text-xl font-bold text-gray-800">Naga</h1>
				<div class="space-x-4">
					<a href="/devices" class="text-gray-600 hover:text-gray-900">Devices</a>
					<a href="/orchs" class="text-gray-600 hover:text-gray-900">Orchs</a>
					<a href="/monitor" class="text-gray-600 hover:text-gray-900">GPU Monitor</a>
				</div>
			</div>
		</div>
	</nav>
	<main class="max-w-7xl mx-auto px-4 py-8">
		<div class="grid grid-cols-1 md:grid-cols-3 gap-6">`)

	// Card 1: Devices
	b.WriteString(fmt.Sprintf(`
			<div class="bg-white rounded-lg shadow p-6">
				<h2 class="text-lg font-semibold text-gray-700">Devices</h2>
				<p class="text-3xl font-bold text-blue-600 mt-2">%d <span class="text-lg text-gray-400">/ %d</span></p>
				<p class="text-sm text-gray-500 mt-1">online</p>
				<a href="/devices" class="text-blue-500 text-sm">View all →</a>
			</div>`, onlineCount, totalCount))

	// Card 2: GPU Nodes
	b.WriteString(fmt.Sprintf(`
			<div class="bg-white rounded-lg shadow p-6">
				<h2 class="text-lg font-semibold text-gray-700">GPU Nodes</h2>
				<p class="text-3xl font-bold text-purple-600 mt-2">%d <span class="text-lg text-gray-400">nodes</span></p>
				<p class="text-sm text-gray-500 mt-1">%d GPUs total</p>
				<a href="/monitor" class="text-purple-500 text-sm">Monitor →</a>
			</div>`, gpuNodeCount, totalGPUs))

	// Card 3: Orchs
	b.WriteString(fmt.Sprintf(`
			<div class="bg-white rounded-lg shadow p-6">
				<h2 class="text-lg font-semibold text-gray-700">Orchestrations</h2>
				<p class="text-3xl font-bold text-green-600 mt-2">%d <span class="text-lg text-gray-400">total</span></p>
				<p class="text-sm text-gray-500 mt-1">%d running</p>
				<a href="/orchs" class="text-green-500 text-sm">View all →</a>
			</div>`, orchCount, orchRunning))

	b.WriteString(`
		</div>`)

	// GPU Devices section
	if len(gpuDevices) > 0 {
		b.WriteString(`
		<div class="mt-8 bg-white rounded-lg shadow">
			<div class="px-6 py-4 border-b">
				<h2 class="text-lg font-semibold text-gray-700">GPU Devices</h2>
			</div>
			<div class="divide-y">`)
		for _, gd := range gpuDevices {
			statusDot := `<span class="inline-block w-2.5 h-2.5 rounded-full bg-green-500"></span>`
			if !gd.Online {
				statusDot = `<span class="inline-block w-2.5 h-2.5 rounded-full bg-red-500"></span>`
			}
			b.WriteString(fmt.Sprintf(`
				<a href="/devices/%s" class="flex items-center justify-between px-6 py-4 hover:bg-gray-50">
					<div class="flex items-center gap-3">
						%s
						<span class="font-medium text-gray-800">%s</span>
					</div>
					<div class="flex items-center gap-6">
						<span class="text-sm text-purple-600 font-medium">%dx %s</span>
						<span class="text-sm text-gray-400 font-mono">%s</span>
					</div>
				</a>`, url.PathEscape(gd.ID), statusDot, esc(gd.Name), gd.GPUCount, esc(gd.GPUModel), esc(gd.IP)))
		}
		b.WriteString(`
			</div>
		</div>`)
	}

	b.WriteString(`
	</main>
</body>
</html>`)

	return c.HTML(http.StatusOK, b.String())
}

// DeviceList renders the device list page
func (h *Handler) DeviceList(c echo.Context) error {
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>Devices - Naga</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<div class="flex justify-between items-center">
				<h1 class="text-xl font-bold text-gray-800"><a href="/">Naga</a></h1>
				<div class="space-x-4">
					<a href="/devices" class="text-blue-600 font-semibold">Devices</a>
					<a href="/orchs" class="text-gray-600 hover:text-gray-900">Orchs</a>
				</div>
			</div>
		</div>
	</nav>
	<main class="max-w-7xl mx-auto px-4 py-8">
		<div class="flex justify-between items-center mb-6">
			<h2 class="text-2xl font-bold text-gray-800">Devices</h2>
			<button hx-get="/htmx/devices?refresh=true" hx-target="#device-list" hx-swap="innerHTML" class="bg-blue-500 text-white px-4 py-2 rounded hover:bg-blue-600">
				Refresh
			</button>
		</div>
		<div id="device-list" hx-get="/htmx/devices" hx-trigger="load" hx-swap="innerHTML">
			<p class="text-gray-500">Loading devices...</p>
		</div>
	</main>
</body>
</html>`)
}

// DeviceDetail renders the device detail page
func (h *Handler) DeviceDetail(c echo.Context) error {
	id := c.Param("id")
	escapedID := url.PathEscape(id)
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>Device Details - Naga</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<a href="/" class="text-xl font-bold text-gray-800">Naga</a>
		</div>
	</nav>
		<main class="max-w-7xl mx-auto px-4 py-8">
			<div hx-get="/htmx/devices/`+escapedID+`" hx-trigger="load" hx-swap="innerHTML">
				Loading device details...
			</div>
		</main>
</body>
</html>`)
}

// OrchList renders the orch list page
func (h *Handler) OrchList(c echo.Context) error {
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>Orchs - Naga</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<div class="flex justify-between items-center">
				<h1 class="text-xl font-bold text-gray-800"><a href="/">Naga</a></h1>
				<div class="space-x-4">
					<a href="/devices" class="text-gray-600 hover:text-gray-900">Devices</a>
					<a href="/orchs" class="text-blue-600 font-semibold">Orchs</a>
				</div>
			</div>
		</div>
	</nav>
	<main class="max-w-7xl mx-auto px-4 py-8">
		<div class="flex justify-between items-center mb-6">
			<h2 class="text-2xl font-bold text-gray-800">Orchs</h2>
			<a href="/orchs/new" class="bg-blue-500 text-white px-4 py-2 rounded hover:bg-blue-600">
				Create Orch
			</a>
		</div>
		<div id="orch-list" hx-get="/htmx/orchs" hx-trigger="load" hx-swap="innerHTML">
			<p class="text-gray-500">Loading orchs...</p>
		</div>
	</main>
</body>
</html>`)
}

// OrchNew renders the new orch form
func (h *Handler) OrchNew(c echo.Context) error {
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>Create Orch - Naga</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<a href="/" class="text-xl font-bold text-gray-800">Naga</a>
		</div>
	</nav>
	<main class="max-w-7xl mx-auto px-4 py-8">
		<div class="bg-white rounded-lg shadow p-6 max-w-2xl">
			<h2 class="text-2xl font-bold text-gray-800 mb-6">Create New Orch</h2>
			<form hx-post="/orchs" hx-target="#result" class="space-y-4">
				<div>
					<label class="block text-sm font-medium text-gray-700">Orch Name</label>
					<input type="text" name="name" required class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500 p-2 border">
				</div>
				<div>
					<label class="block text-sm font-medium text-gray-700">Description</label>
					<textarea name="description" rows="2" class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500 p-2 border"></textarea>
				</div>
				<div>
					<label class="block text-sm font-medium text-gray-700">Step 1: Select Orch Nodes</label>
					<p class="text-xs text-gray-500 mb-2">GPU nodes are shown first. Select the nodes for your orch.</p>
					<div id="worker-select" hx-get="/htmx/device-checkboxes" hx-trigger="load" hx-swap="innerHTML">
						<p class="text-gray-400 text-sm animate-pulse">Loading devices and checking GPU availability...</p>
					</div>
				</div>
				<div>
					<label class="block text-sm font-medium text-gray-700">Step 2: Select Head Node</label>
					<p class="text-xs text-gray-500 mb-2">Choose a head node from your selected nodes. Head node is a scheduler — GPU is not required.</p>
					<select name="coordinator" id="head-select" required class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500 p-2 border">
						<option value="">Select nodes first...</option>
					</select>
				</div>
				<div class="pt-4">
					<button type="submit" class="bg-blue-500 text-white px-4 py-2 rounded hover:bg-blue-600">
						Create Orch
					</button>
					<a href="/orchs" class="ml-4 text-gray-600 hover:text-gray-800">Cancel</a>
				</div>
			</form>
			<script>
			document.addEventListener('change', function(e) {
				if (e.target.name !== 'workers') return;
				var headSelect = document.getElementById('head-select');
				var checked = document.querySelectorAll('input[name="workers"]:checked');
				headSelect.innerHTML = '<option value="">Select head node...</option>';
				checked.forEach(function(cb) {
					var opt = document.createElement('option');
					opt.value = cb.value;
					opt.textContent = cb.closest('label').querySelector('.node-name').textContent;
					headSelect.appendChild(opt);
				});
				if (checked.length === 0) {
					headSelect.innerHTML = '<option value="">Select nodes first...</option>';
				}
			});
			</script>
			<div id="result" class="mt-4"></div>
		</div>
	</main>
</body>
</html>`)
}

// OrchCreate handles orch creation (form submission)
func (h *Handler) OrchCreate(c echo.Context) error {
	name := c.FormValue("name")
	if name == "" {
		return c.HTML(http.StatusBadRequest, `<p class="text-red-500">Orch name is required</p>`)
	}

	headID := c.FormValue("coordinator")
	if headID == "" {
		return c.HTML(http.StatusBadRequest, `<p class="text-red-500">Head node is required</p>`)
	}

	workerIDs := c.Request().Form["workers"]

	if h.orchUC == nil {
		return c.HTML(http.StatusServiceUnavailable, `<p class="text-red-500">Orch service not available</p>`)
	}

	// Remove head from workers list (head is selected from workers)
	var filteredWorkers []string
	for _, wid := range workerIDs {
		if wid != headID {
			filteredWorkers = append(filteredWorkers, wid)
		}
	}

	orch, err := h.orchUC.CreateOrch(c.Request().Context(), name, headID, filteredWorkers)
	if err != nil {
		log.Printf("internal error: create orch: %v", err)
		return c.HTML(http.StatusOK, fmt.Sprintf(`<p class="text-red-500">Failed to create orch: %s</p>`, esc(err.Error())))
	}

	return c.HTML(http.StatusOK, fmt.Sprintf(`<div class="bg-green-50 border border-green-200 rounded p-4">
		<p class="text-green-700 font-medium">Orch "%s" created successfully!</p>
		<a href="/orchs/%s" class="text-blue-500 hover:underline text-sm">View orch →</a>
	</div>`, orch.Name, orch.ID))
}

// OrchDetail renders the orch detail page
func (h *Handler) OrchDetail(c echo.Context) error {
	id := c.Param("id")
	escapedID := url.PathEscape(id)
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>Orch Details - Naga</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<a href="/" class="text-xl font-bold text-gray-800">Naga</a>
		</div>
	</nav>
		<main class="max-w-7xl mx-auto px-4 py-8">
			<div hx-get="/htmx/orchs/`+escapedID+`" hx-trigger="load" hx-swap="innerHTML">
				Loading orch details...
			</div>
		</main>
</body>
</html>`)
}

// OrchDelete handles orch deletion (form submission)
func (h *Handler) OrchDelete(c echo.Context) error {
	return h.APIOrchDelete(c)
}

// APIOrchCreate handles orch creation API
func (h *Handler) APIOrchCreate(c echo.Context) error {
	var req struct {
		Name      string   `json:"name"`
		HeadID    string   `json:"coordinator_id"`
		WorkerIDs []string `json:"worker_ids"`
		Mode      string   `json:"mode"` // "basic" or "ray"
	}
	if err := c.Bind(&req); err != nil {
		// Fallback to form values for HTMX compatibility
		req.Name = c.FormValue("name")
		req.HeadID = c.FormValue("coordinator")
	}
	if req.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}
	if req.HeadID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "coordinator_id is required"})
	}

	if h.orchUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "orch service not available"})
	}

	mode := domain.OrchModeBasic
	if req.Mode == "ray" {
		mode = domain.OrchModeRay
	}

	orch, err := h.orchUC.CreateOrch(c.Request().Context(), req.Name, req.HeadID, req.WorkerIDs, mode)
	if err != nil {
		return internalError(c, "failed to create orch", err)
	}

	return c.JSON(http.StatusOK, orch)
}

// APIOrchDelete handles orch deletion API
func (h *Handler) APIOrchDelete(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "orch id is required"})
	}
	force := c.QueryParam("force") == "true"

	if h.orchUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "orch service not available"})
	}

	devices, err := h.deviceUC.GetDeviceMap(c.Request().Context())
	if err != nil {
		return internalError(c, "failed to get devices", err)
	}

	if err := h.orchUC.DeleteOrch(c.Request().Context(), id, devices, force); err != nil {
		return internalError(c, "failed to delete orch", err)
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

// HTMXDeviceList returns device list as HTML fragment for HTMX
func (h *Handler) HTMXDeviceList(c echo.Context) error {
	ctx := c.Request().Context()

	if h.deviceUC == nil {
		return c.HTML(http.StatusServiceUnavailable, `<p class="text-red-500">Device service not available</p>`)
	}

	forceRefresh := c.QueryParam("refresh") == "true"
	devices, err := h.deviceUC.ListDevices(ctx, forceRefresh)
	if err != nil {
		log.Printf("internal error: list devices: %v", err)
		return c.HTML(http.StatusInternalServerError, `<p class="text-red-500">Failed to load devices</p>`)
	}

	var metricsMap map[string]*domain.DeviceMetrics
	if h.monitorUC != nil {
		snapshot, err := h.monitorUC.GetAllMetrics(ctx)
		if err == nil && snapshot != nil {
			metricsMap = snapshot.Devices
		}
	}

	if len(devices) == 0 {
		return c.HTML(http.StatusOK, `<p class="text-gray-500">No devices found</p>`)
	}

	var b strings.Builder
	b.WriteString(`<div class="bg-white rounded-lg shadow overflow-hidden">
<table class="min-w-full divide-y divide-gray-200">
<thead class="bg-gray-50">
<tr>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">IP</th>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">OS</th>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">SSH</th>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">GPU</th>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">CPU</th>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Memory</th>
</tr>
</thead>
<tbody class="bg-white divide-y divide-gray-200">`)

	for _, d := range devices {
		statusColor := "red"
		statusLabel := string(d.Status)
		if d.IsOnline() {
			statusColor = "green"
		}
		sshBadge := `<span class="text-gray-400">-</span>`
		if d.SSHEnabled {
			sshBadge = `<span class="text-green-600 font-medium">Yes</span>`
		}
		gpuBadge := `<span class="text-gray-400">-</span>`
		if d.HasGPU {
			gpuBadge = fmt.Sprintf(`<span class="text-purple-600 font-medium">%dx %s</span>`, d.GPUCount, d.GPUModel)
		} else if d.GPUModel == "none" {
			gpuBadge = `<span class="text-gray-400">None</span>`
		}
		cpuCell := `<span class="text-gray-400">-</span>`
		memCell := `<span class="text-gray-400">-</span>`
		if metricsMap != nil {
			if m, ok := metricsMap[d.ID]; ok && !m.HasError() {
				cpuColor := "green"
				if m.CPU.UsagePercent > 80 {
					cpuColor = "red"
				} else if m.CPU.UsagePercent > 50 {
					cpuColor = "yellow"
				}
				cpuCell = fmt.Sprintf(`<span class="text-%s-600 font-medium">%.1f%%</span>`, cpuColor, m.CPU.UsagePercent)

				memColor := "green"
				if m.Memory.UsagePercent > 80 {
					memColor = "red"
				} else if m.Memory.UsagePercent > 50 {
					memColor = "yellow"
				}
				memGB := float64(m.Memory.Used) / 1073741824
				totalGB := float64(m.Memory.Total) / 1073741824
				memCell = fmt.Sprintf(`<span class="text-%s-600 font-medium">%.1f%% <span class="text-gray-400 text-xs">(%.1f/%.1fG)</span></span>`, memColor, m.Memory.UsagePercent, memGB, totalGB)
			}
		}
		b.WriteString(fmt.Sprintf(`<tr class="hover:bg-gray-50 cursor-pointer" onclick="window.location='/devices/%s'">
	<td class="px-6 py-4 whitespace-nowrap">
		<div class="text-sm font-medium text-gray-900">%s</div>
		<div class="text-xs text-gray-500">%s</div>
	</td>
	<td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">%s</td>
	<td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">%s</td>
	<td class="px-6 py-4 whitespace-nowrap">
		<span class="px-2 py-1 text-xs rounded-full bg-%s-100 text-%s-800">%s</span>
	</td>
	<td class="px-6 py-4 whitespace-nowrap text-sm">%s</td>
	<td class="px-6 py-4 whitespace-nowrap text-sm">%s</td>
	<td class="px-6 py-4 whitespace-nowrap text-sm">%s</td>
	<td class="px-6 py-4 whitespace-nowrap text-sm">%s</td>
</tr>`,
			url.PathEscape(d.ID),
			esc(d.GetDisplayName()), esc(d.Hostname),
			esc(d.TailscaleIP),
			esc(d.OS),
			statusColor, statusColor, esc(statusLabel),
			sshBadge,
			gpuBadge,
			cpuCell, memCell,
		))
	}

	b.WriteString(`</tbody></table></div>`)
	b.WriteString(fmt.Sprintf(`<p class="text-sm text-gray-500 mt-2">%d devices found</p>`, len(devices)))

	return c.HTML(http.StatusOK, b.String())
}

// HTMXDeviceCount returns device count as HTML fragment
func (h *Handler) HTMXDeviceCount(c echo.Context) error {
	ctx := c.Request().Context()
	if h.deviceUC == nil {
		return c.HTML(http.StatusOK, `0`)
	}
	devices, err := h.deviceUC.ListDevices(ctx, false)
	if err != nil {
		return c.HTML(http.StatusOK, `?`)
	}
	return c.HTML(http.StatusOK, fmt.Sprintf(`%d`, len(devices)))
}

// HTMXDeviceDetail returns device detail as HTML fragment
func (h *Handler) HTMXDeviceDetail(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.deviceUC == nil {
		return c.HTML(http.StatusServiceUnavailable, `<p class="text-red-500">Device service not available</p>`)
	}

	device, err := h.deviceUC.GetDevice(ctx, id)
	if err != nil {
		log.Printf("internal error: get device %s: %v", id, err)
		return c.HTML(http.StatusNotFound, `<p class="text-red-500">Device not found</p>`)
	}

	var b strings.Builder
	b.WriteString(`<div class="bg-white rounded-lg shadow p-6">`)
	b.WriteString(fmt.Sprintf(`<h2 class="text-2xl font-bold text-gray-800 mb-4">%s</h2>`, esc(device.GetDisplayName())))
	b.WriteString(`<div class="grid grid-cols-2 gap-4">`)

	fields := [][2]string{
		{"Hostname", esc(device.Hostname)},
		{"Tailscale IP", esc(device.TailscaleIP)},
		{"OS", esc(device.OS)},
		{"Status", esc(string(device.Status))},
		{"User", esc(device.User)},
		{"Last Seen", device.LastSeen.Format("2006-01-02 15:04:05")},
	}
	for _, f := range fields {
		b.WriteString(fmt.Sprintf(`<div><span class="text-sm text-gray-500">%s</span><p class="font-medium">%s</p></div>`, f[0], f[1]))
	}

	sshStatus := `<span class="text-red-500">Disabled</span>`
	if device.SSHEnabled {
		sshStatus = `<span class="text-green-600">Enabled</span>`
	}
	b.WriteString(fmt.Sprintf(`<div><span class="text-sm text-gray-500">SSH</span><p class="font-medium">%s</p></div>`, sshStatus))
	b.WriteString(`</div>`)

	// Metrics section
	if h.monitorUC != nil {
		metrics, err := h.monitorUC.GetDeviceMetrics(ctx, id)
		if err == nil && metrics != nil && !metrics.HasError() {
			b.WriteString(`<div class="mt-6 border-t pt-4">`)
			b.WriteString(`<h3 class="text-lg font-semibold text-gray-700 mb-3">System Metrics</h3>`)
			b.WriteString(`<div class="grid grid-cols-1 md:grid-cols-3 gap-4">`)

			// CPU card
			cpuColor := "green"
			if metrics.CPU.UsagePercent > 80 {
				cpuColor = "red"
			} else if metrics.CPU.UsagePercent > 50 {
				cpuColor = "yellow"
			}
			b.WriteString(fmt.Sprintf(`<div class="bg-gray-50 rounded-lg p-4">
            <div class="text-sm text-gray-500 mb-1">CPU</div>
            <div class="text-2xl font-bold text-%s-600">%.1f%%</div>
            <div class="text-xs text-gray-400 mt-1">%d cores · %s</div>
            <div class="text-xs text-gray-400">Load: %.2f / %.2f / %.2f</div>
            <div class="w-full bg-gray-200 rounded-full h-2 mt-2">
                <div class="bg-%s-500 h-2 rounded-full" style="width: %.0f%%"></div>
            </div>
        </div>`, cpuColor, metrics.CPU.UsagePercent, metrics.CPU.Cores, esc(metrics.CPU.ModelName),
				metrics.CPU.LoadAvg1, metrics.CPU.LoadAvg5, metrics.CPU.LoadAvg15,
				cpuColor, metrics.CPU.UsagePercent))

			// Memory card
			memColor := "green"
			if metrics.Memory.UsagePercent > 80 {
				memColor = "red"
			} else if metrics.Memory.UsagePercent > 50 {
				memColor = "yellow"
			}
			usedGB := float64(metrics.Memory.Used) / 1073741824
			totalGB := float64(metrics.Memory.Total) / 1073741824
			availGB := float64(metrics.Memory.Available) / 1073741824
			b.WriteString(fmt.Sprintf(`<div class="bg-gray-50 rounded-lg p-4">
            <div class="text-sm text-gray-500 mb-1">Memory</div>
            <div class="text-2xl font-bold text-%s-600">%.1f%%</div>
            <div class="text-xs text-gray-400 mt-1">%.1fG used / %.1fG total</div>
            <div class="text-xs text-gray-400">Available: %.1fG</div>
            <div class="w-full bg-gray-200 rounded-full h-2 mt-2">
                <div class="bg-%s-500 h-2 rounded-full" style="width: %.0f%%"></div>
            </div>
        </div>`, memColor, metrics.Memory.UsagePercent, usedGB, totalGB, availGB,
				memColor, metrics.Memory.UsagePercent))

			// Disk card
			if len(metrics.Disk.Partitions) > 0 {
				b.WriteString(`<div class="bg-gray-50 rounded-lg p-4">
                <div class="text-sm text-gray-500 mb-1">Disk</div>`)
				for _, p := range metrics.Disk.Partitions {
					diskColor := "green"
					if p.UsagePercent > 90 {
						diskColor = "red"
					} else if p.UsagePercent > 70 {
						diskColor = "yellow"
					}
					pUsedGB := float64(p.Used) / 1073741824
					pTotalGB := float64(p.Total) / 1073741824
					b.WriteString(fmt.Sprintf(`<div class="mb-2">
                    <div class="text-xs text-gray-500">%s</div>
                    <div class="text-sm font-bold text-%s-600">%.1f%% <span class="font-normal text-gray-400">(%.0fG/%.0fG)</span></div>
                    <div class="w-full bg-gray-200 rounded-full h-1.5 mt-1">
                        <div class="bg-%s-500 h-1.5 rounded-full" style="width: %.0f%%"></div>
                    </div>
                </div>`, esc(p.MountPoint), diskColor, p.UsagePercent, pUsedGB, pTotalGB,
						diskColor, p.UsagePercent))
				}
				b.WriteString(`</div>`)
			}

			b.WriteString(`</div></div>`)
		}
	}

	b.WriteString(`<div class="mt-4"><a href="/devices" class="text-blue-500 hover:underline">← Back to devices</a></div>`)
	b.WriteString(`</div>`)

	return c.HTML(http.StatusOK, b.String())
}

// HTMXOrchList returns orch list as HTML fragment
func (h *Handler) HTMXOrchList(c echo.Context) error {
	ctx := c.Request().Context()

	if h.orchUC == nil {
		return c.HTML(http.StatusOK, `<p class="text-gray-500">No orchs yet. <a href="/orchs/new" class="text-blue-500 hover:underline">Create one</a></p>`)
	}

	orchs, err := h.orchUC.ListOrchs(ctx)
	if err != nil {
		log.Printf("internal error: list orchs: %v", err)
		return c.HTML(http.StatusInternalServerError, `<p class="text-red-500">Failed to load orchs</p>`)
	}

	if len(orchs) == 0 {
		return c.HTML(http.StatusOK, `<p class="text-gray-500">No orchs yet. <a href="/orchs/new" class="text-blue-500 hover:underline">Create one</a></p>`)
	}

	var b strings.Builder
	b.WriteString(`<div class="bg-white rounded-lg shadow overflow-hidden">
<table class="min-w-full divide-y divide-gray-200">
<thead class="bg-gray-50">
<tr>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Head Node</th>
	<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Workers</th>
</tr>
</thead>
<tbody class="bg-white divide-y divide-gray-200">`)

	for _, cl := range orchs {
		statusColor := "gray"
		switch cl.Status {
		case "running":
			statusColor = "green"
		case "starting":
			statusColor = "yellow"
		case "error":
			statusColor = "red"
		}
		b.WriteString(fmt.Sprintf(`<tr class="hover:bg-gray-50 cursor-pointer" onclick="window.location='/orchs/%s'">
	<td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">%s</td>
	<td class="px-6 py-4 whitespace-nowrap">
		<span class="px-2 py-1 text-xs rounded-full bg-%s-100 text-%s-800">%s</span>
	</td>
	<td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">%s</td>
	<td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">%d</td>
</tr>`, url.PathEscape(cl.ID), esc(cl.Name), statusColor, statusColor, esc(string(cl.Status)), esc(cl.CoordinatorID), len(cl.WorkerIDs)))
	}
	b.WriteString(`</tbody></table></div>`)

	return c.HTML(http.StatusOK, b.String())
}

// HTMXOrchDetail returns orch detail as HTML fragment
func (h *Handler) HTMXOrchDetail(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.orchUC == nil {
		return c.HTML(http.StatusServiceUnavailable, `<p class="text-red-500">Orch service not available</p>`)
	}

	orch, err := h.orchUC.GetOrch(ctx, id)
	if err != nil {
		log.Printf("internal error: get orch %s: %v", id, err)
		return c.HTML(http.StatusNotFound, `<p class="text-red-500">Orch not found</p>`)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<div class="bg-white rounded-lg shadow p-6">
<h2 class="text-2xl font-bold text-gray-800 mb-4">%s</h2>
<div class="grid grid-cols-2 gap-4">
	<div><span class="text-sm text-gray-500">Status</span><p class="font-medium">%s</p></div>
	<div><span class="text-sm text-gray-500">Head Node</span><p class="font-medium">%s</p></div>
	<div><span class="text-sm text-gray-500">Workers</span><p class="font-medium">%d nodes</p></div>
	<div><span class="text-sm text-gray-500">Dashboard</span><p class="font-medium">%s</p></div>
</div>`, esc(orch.Name), esc(string(orch.Status)), esc(orch.CoordinatorID), len(orch.WorkerIDs), esc(orch.DashboardURL)))

	if len(orch.WorkerIDs) > 0 {
		b.WriteString(`<div class="mt-4"><h3 class="text-lg font-semibold text-gray-700 mb-2">Worker Nodes</h3><ul class="list-disc list-inside">`)
		for _, wid := range orch.WorkerIDs {
			b.WriteString(fmt.Sprintf(`<li class="text-sm text-gray-600">%s</li>`, esc(wid)))
		}
		b.WriteString(`</ul></div>`)
	}

	b.WriteString(fmt.Sprintf(`<div class="mt-4 flex items-center gap-4">
<a href="/orchs" class="text-blue-500 hover:underline">← Back to orchs</a>
<button hx-delete="/api/orchs/%s?force=true" hx-confirm="Are you sure you want to delete this orch?" hx-target="body" hx-swap="innerHTML" class="px-3 py-1 bg-red-500 text-white text-sm rounded hover:bg-red-600" onclick="event.preventDefault(); if(confirm('Delete this orch?')){fetch('/api/orchs/%s?force=true',{method:'DELETE'}).then(()=>window.location.href='/orchs')}">Delete Orch</button>
</div>`, url.PathEscape(orch.ID), url.PathEscape(orch.ID)))
	b.WriteString(`</div>`)

	return c.HTML(http.StatusOK, b.String())
}

// HTMXDeviceOptions returns device list as <option> tags for select dropdown
func (h *Handler) HTMXDeviceOptions(c echo.Context) error {
	ctx := c.Request().Context()
	if h.deviceUC == nil {
		return c.HTML(http.StatusOK, `<option disabled>Service unavailable</option>`)
	}
	devices, err := h.deviceUC.ListDevices(ctx, false)
	if err != nil {
		return c.HTML(http.StatusOK, `<option disabled>Failed to load</option>`)
	}
	var b strings.Builder
	b.WriteString(`<option value="">Select a device...</option>`)
	for _, d := range devices {
		if d.IsOnline() {
			b.WriteString(fmt.Sprintf(`<option value="%s">%s (%s)</option>`, url.PathEscape(d.ID), esc(d.GetDisplayName()), esc(d.TailscaleIP)))
		}
	}
	return c.HTML(http.StatusOK, b.String())
}

// HTMXDeviceCheckboxes returns device list as checkboxes for worker selection.
// GPU nodes are shown first with a purple badge, non-GPU nodes below.
func (h *Handler) HTMXDeviceCheckboxes(c echo.Context) error {
	ctx := c.Request().Context()
	if h.deviceUC == nil {
		return c.HTML(http.StatusOK, `<p class="text-red-500">Service unavailable</p>`)
	}
	devices, err := h.deviceUC.ListDevices(ctx, false)
	if err != nil {
		return c.HTML(http.StatusOK, `<p class="text-red-500">Failed to load</p>`)
	}

	// Split into GPU and non-GPU
	var gpuDevices, otherDevices []*domain.Device
	for _, d := range devices {
		if !d.IsOnline() {
			continue
		}
		if d.HasGPU {
			gpuDevices = append(gpuDevices, d)
		} else {
			otherDevices = append(otherDevices, d)
		}
	}

	var b strings.Builder
	b.WriteString(`<div class="space-y-2 mt-1">`)

	if len(gpuDevices) > 0 {
		b.WriteString(`<p class="text-xs font-semibold text-purple-600 uppercase mt-1">GPU Nodes</p>`)
		for _, d := range gpuDevices {
			b.WriteString(fmt.Sprintf(`<label class="flex items-center space-x-2 p-2 rounded bg-purple-50 border border-purple-200">
	<input type="checkbox" name="workers" value="%s" class="rounded border-gray-300">
	<span class="text-sm node-name">%s</span>
	<span class="text-xs text-purple-600 font-medium">%dx %s</span>
	<span class="text-xs text-gray-400">%s</span>
</label>`, url.PathEscape(d.ID), esc(d.GetDisplayName()), d.GPUCount, esc(d.GPUModel), esc(d.TailscaleIP)))
		}
	}

	if len(otherDevices) > 0 {
		b.WriteString(`<p class="text-xs font-semibold text-gray-500 uppercase mt-3">Other Nodes</p>`)
		for _, d := range otherDevices {
			b.WriteString(fmt.Sprintf(`<label class="flex items-center space-x-2 p-2 rounded bg-gray-50 border border-gray-200">
	<input type="checkbox" name="workers" value="%s" class="rounded border-gray-300">
	<span class="text-sm node-name">%s</span>
	<span class="text-xs text-gray-400">%s · %s</span>
</label>`, url.PathEscape(d.ID), esc(d.GetDisplayName()), esc(d.OS), esc(d.TailscaleIP)))
		}
	}

	b.WriteString(`</div>`)
	return c.HTML(http.StatusOK, b.String())
}

// APIDeviceList returns list of devices as JSON
func (h *Handler) APIDeviceList(c echo.Context) error {
	ctx := c.Request().Context()

	if h.deviceUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "device service not available"})
	}

	forceRefresh := c.QueryParam("refresh") == "true"
	devices, err := h.deviceUC.ListDevices(ctx, forceRefresh)
	if err != nil {
		return internalError(c, "failed to list devices", err)
	}

	return c.JSON(http.StatusOK, devices)
}

// APIDeviceDetail returns device details as JSON
func (h *Handler) APIDeviceDetail(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.deviceUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "device service not available"})
	}

	device, err := h.deviceUC.GetDevice(ctx, id)
	if err != nil {
		log.Printf("internal error: get device %s: %v", id, err)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "device not found"})
	}

	return c.JSON(http.StatusOK, device)
}

// APIDeviceMetrics returns device metrics as JSON
func (h *Handler) APIDeviceMetrics(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.monitorUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "monitor service not available"})
	}

	metrics, err := h.monitorUC.GetDeviceMetrics(ctx, id)
	if err != nil {
		return internalError(c, "failed to get device metrics", err)
	}

	return c.JSON(http.StatusOK, metrics)
}

// APIOrchList returns list of orchs as JSON
func (h *Handler) APIOrchList(c echo.Context) error {
	ctx := c.Request().Context()

	if h.orchUC == nil {
		return c.JSON(http.StatusOK, []interface{}{})
	}

	orchs, err := h.orchUC.ListOrchs(ctx)
	if err != nil {
		return internalError(c, "failed to list orchs", err)
	}
	if orchs == nil {
		orchs = []*domain.Orch{}
	}

	return c.JSON(http.StatusOK, orchs)
}

// APIOrchDetail returns orch details as JSON
func (h *Handler) APIOrchDetail(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.orchUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "orch service not available"})
	}

	orch, err := h.orchUC.GetOrch(ctx, id)
	if err != nil {
		log.Printf("internal error: get orch %s: %v", id, err)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "orch not found"})
	}

	return c.JSON(http.StatusOK, orch)
}

// APIOrchStart starts a orch
func (h *Handler) APIOrchStart(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.orchUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "orch service not available"})
	}

	// Get device map for orch operations
	devices, err := h.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return internalError(c, "failed to get devices", err)
	}

	err = h.orchUC.StartOrch(ctx, id, devices)
	if err != nil {
		return internalError(c, "failed to start orch", err)
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "started", "id": id})
}

// APIOrchStop stops a orch
func (h *Handler) APIOrchStop(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()
	force := c.QueryParam("force") == "true"

	if h.orchUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "orch service not available"})
	}

	devices, err := h.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return internalError(c, "failed to get devices", err)
	}

	err = h.orchUC.StopOrch(ctx, id, devices, force)
	if err != nil {
		return internalError(c, "failed to stop orch", err)
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "stopped", "id": id})
}

// APIOrchAddWorker adds a worker to a orch
func (h *Handler) APIOrchAddWorker(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if h.orchUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "orch service not available"})
	}

	devices, err := h.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return internalError(c, "failed to get devices", err)
	}

	// Get the orch to find head device
	orch, err := h.orchUC.GetOrch(ctx, id)
	if err != nil {
		log.Printf("internal error: get orch %s: %v", id, err)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "orch not found"})
	}

	device := devices[req.DeviceID]
	if device == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "device not found: " + req.DeviceID})
	}
	headDevice := devices[orch.CoordinatorID]
	if headDevice == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "head device not found: " + orch.CoordinatorID})
	}

	err = h.orchUC.AddWorker(ctx, id, req.DeviceID, device, headDevice)
	if err != nil {
		return internalError(c, "failed to add worker", err)
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "worker_added", "orch_id": id, "device_id": req.DeviceID})
}

// APIOrchRemoveWorker removes a worker from a orch
func (h *Handler) APIOrchRemoveWorker(c echo.Context) error {
	id := c.Param("id")
	deviceID := c.Param("deviceId")
	ctx := c.Request().Context()

	if h.orchUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "orch service not available"})
	}

	devices, err := h.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return internalError(c, "failed to get devices", err)
	}

	device := devices[deviceID]
	if device == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "device not found: " + deviceID})
	}

	err = h.orchUC.RemoveWorker(ctx, id, deviceID, device)
	if err != nil {
		return internalError(c, "failed to remove worker", err)
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "worker_removed", "orch_id": id, "device_id": deviceID})
}

// APIOrchChangeHead changes the head node of a orch.
// For running Ray orchs, performs a graceful head transfer via failoverUC.TransferCoordinator.
// For basic-mode or stopped orchs, delegates to orchUC.ChangeHead.
func (h *Handler) APIOrchChangeHead(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	var req struct {
		NewHeadID string `json:"new_coordinator_id"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.NewHeadID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "new_coordinator_id is required"})
	}

	if h.orchUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "orch service not available"})
	}

	devices, err := h.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return internalError(c, "failed to get devices", err)
	}

	// Use graceful TransferCoordinator for running Ray orchs when failoverUC is available
	if h.failoverUC != nil {
		orch, err := h.orchUC.GetOrch(ctx, id)
		if err == nil && orch.IsRunning() && orch.IsRayMode() {
			if err := h.failoverUC.TransferCoordinator(ctx, orch, req.NewHeadID, devices, ""); err != nil {
				return internalError(c, "failed to transfer head node", err)
			}
			return c.JSON(http.StatusOK, map[string]string{"status": "head_transferred", "orch_id": id, "new_coordinator_id": req.NewHeadID})
		}
	}

	// Fallback: stop/reconfigure/restart via orchUC (basic mode or stopped orchs)
	if err := h.orchUC.ChangeHead(ctx, id, req.NewHeadID, devices); err != nil {
		return internalError(c, "failed to change head node", err)
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "head_changed", "orch_id": id, "new_coordinator_id": req.NewHeadID})
}

// APIOrchHealth returns health status of all nodes in a orch
func (h *Handler) APIOrchHealth(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.orchUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "orch service not available"})
	}

	orch, err := h.orchUC.GetOrch(ctx, id)
	if err != nil {
		log.Printf("internal error: get orch %s: %v", id, err)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "orch not found"})
	}

	// Check each node's agent health endpoint
	type nodeStatus struct {
		NodeID  string `json:"nodeId"`
		Role    string `json:"role"`
		Healthy bool   `json:"healthy"`
		Error   string `json:"error,omitempty"`
	}

	var statuses []nodeStatus

	// Check node health based on device online status
	isNodeHealthy := func(deviceID string) bool {
		if h.deviceUC == nil {
			return false
		}
		device, err := h.deviceUC.GetDevice(ctx, deviceID)
		if err != nil {
			return false
		}
		return device.Status == "online"
	}

	// Head node
	statuses = append(statuses, nodeStatus{
		NodeID:  orch.CoordinatorID,
		Role:    "coordinator",
		Healthy: isNodeHealthy(orch.CoordinatorID),
	})

	// Workers
	for _, wid := range orch.WorkerIDs {
		statuses = append(statuses, nodeStatus{
			NodeID:  wid,
			Role:    "worker",
			Healthy: isNodeHealthy(wid),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"orchId": orch.ID,
		"name":      orch.Name,
		"status":    orch.Status,
		"nodes":     statuses,
	})
}

// APIOrchFailover manually triggers a failover
func (h *Handler) APIOrchFailover(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.orchUC == nil || h.failoverUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "service not available"})
	}

	orch, err := h.orchUC.GetOrch(ctx, id)
	if err != nil {
		log.Printf("internal error: get orch %s: %v", id, err)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "orch not found"})
	}

	var req struct {
		NewHeadID string `json:"new_coordinator_id"`
		Reason    string `json:"reason"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}

	if req.NewHeadID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "new_coordinator_id is required"})
	}

	devices, err := h.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return internalError(c, "failed to get devices", err)
	}

	election := &domain.ElectionResult{
		NewHeadID:  req.NewHeadID,
		Reason:     req.Reason,
		AIDecision: false,
	}

	if err := h.failoverUC.ExecuteFailover(ctx, orch, election, devices, ""); err != nil {
		return internalError(c, "failover failed", err)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":     "failover_complete",
		"orch_id": orch.ID,
		"new_head":   req.NewHeadID,
	})
}

// OrchExecutePage renders the distributed task execution page
func (h *Handler) OrchExecutePage(c echo.Context) error {
	id := url.PathEscape(c.Param("id"))
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>Execute Task - Naga</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<div class="flex justify-between items-center">
				<a href="/" class="text-xl font-bold text-gray-800">Naga</a>
				<div class="space-x-4">
					<a href="/orchs" class="text-gray-600 hover:text-gray-900">Orchs</a>
					<a href="/orchs/`+id+`" class="text-gray-600 hover:text-gray-900">Orch Detail</a>
				</div>
			</div>
		</div>
	</nav>
	<main class="max-w-7xl mx-auto px-4 py-8">
		<div class="bg-white rounded-lg shadow p-6">
			<h2 class="text-2xl font-bold text-gray-800 mb-2">Distributed Task Execution</h2>
			<p class="text-sm text-gray-500 mb-6">Run a command on all worker nodes in parallel and collect results.</p>

			<div class="space-y-4">
				<div>
					<label class="block text-sm font-medium text-gray-700">Command</label>
					<textarea id="cmd-input" name="command" rows="3"
						class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500 p-3 border font-mono text-sm"
						placeholder="e.g. nvidia-smi --query-gpu=name,memory.total --format=csv,noheader">nvidia-smi --query-gpu=name,memory.total,utilization.gpu --format=csv,noheader</textarea>
				</div>
				<div class="flex space-x-2">
					<button hx-post="/orchs/`+id+`/execute" hx-include="#cmd-input" hx-target="#results" hx-swap="innerHTML"
						hx-indicator="#spinner"
						class="bg-purple-600 text-white px-6 py-2 rounded hover:bg-purple-700 font-medium">
						Run on All Workers
					</button>
					<span id="spinner" class="htmx-indicator text-gray-400 text-sm self-center animate-pulse">Running on workers...</span>
				</div>
				<div class="mt-2 space-x-2">
					<span class="text-xs text-gray-500">Quick commands:</span>
					<button onclick="document.getElementById('cmd-input').value='nvidia-smi --query-gpu=name,memory.total,utilization.gpu --format=csv,noheader'"
						class="text-xs bg-gray-100 px-2 py-1 rounded hover:bg-gray-200">GPU Info</button>
					<button onclick="document.getElementById('cmd-input').value='hostname && uname -r && nproc && free -h | head -2'"
						class="text-xs bg-gray-100 px-2 py-1 rounded hover:bg-gray-200">System Info</button>
					<button onclick="document.getElementById('cmd-input').value='python3 -c \"import time; start=time.time(); sum(range(10**7)); print(f\\\"Computed in {time.time()-start:.3f}s\\\")\"'"
						class="text-xs bg-gray-100 px-2 py-1 rounded hover:bg-gray-200">CPU Benchmark</button>
				</div>
			</div>

			<div id="results" class="mt-6"></div>
		</div>
	</main>
</body>
</html>`)
}

// OrchExecuteTask runs a command on all orch workers in parallel
// dangerousPatterns are shell metacharacters and patterns that could enable command injection
var dangerousPatterns = []string{";", "&&", "||", "|", "`", "$(", "${", ">", "<", "\n", "\r", "\\"}

// validateCommand checks that a command is safe to execute remotely
func validateCommand(cmd string) error {
	if len(cmd) > 1024 {
		return fmt.Errorf("command too long (max 1024 chars)")
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(cmd, p) {
			return fmt.Errorf("command contains disallowed pattern: %q", p)
		}
	}
	return nil
}

func (h *Handler) OrchExecuteTask(c echo.Context) error {
	id := c.Param("id")
	command := c.FormValue("command")
	if command == "" {
		return c.HTML(http.StatusOK, `<p class="text-red-500">Command is required</p>`)
	}

	if err := validateCommand(command); err != nil {
		return c.HTML(http.StatusOK, fmt.Sprintf(`<p class="text-red-500">Invalid command: %s</p>`, err.Error()))
	}

	if h.orchUC == nil || h.executor == nil {
		return c.HTML(http.StatusOK, `<p class="text-red-500">Service not available</p>`)
	}

	orch, err := h.orchUC.GetOrch(c.Request().Context(), id)
	if err != nil {
		return c.HTML(http.StatusOK, `<p class="text-red-500">Orch not found</p>`)
	}

	// Get device map
	devices, err := h.deviceUC.GetDeviceMap(c.Request().Context())
	if err != nil {
		return c.HTML(http.StatusOK, `<p class="text-red-500">Failed to get devices</p>`)
	}

	// Collect worker devices
	var workers []*domain.Device
	for _, wid := range orch.WorkerIDs {
		if d, ok := devices[wid]; ok && d.IsOnline() {
			workers = append(workers, d)
		}
	}

	if len(workers) == 0 {
		return c.HTML(http.StatusOK, `<p class="text-yellow-600">No online workers found in this orch</p>`)
	}

	// Execute on all workers in parallel
	results := make([]TaskResult, len(workers))
	var wg sync.WaitGroup

	for i, w := range workers {
		wg.Add(1)
		go func(idx int, dev *domain.Device) {
			defer wg.Done()
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			output, err := h.executor.Execute(ctx, dev, command)
			r := TaskResult{
				DeviceID:   dev.ID,
				DeviceName: dev.GetDisplayName(),
				Duration:   float64(time.Since(start).Milliseconds()),
			}
			if dev.HasGPU {
				r.GPU = fmt.Sprintf("%dx %s", dev.GPUCount, dev.GPUModel)
			}
			if err != nil {
				r.Error = err.Error()
			} else {
				r.Output = strings.TrimSpace(output)
			}
			results[idx] = r
		}(i, w)
	}
	wg.Wait()

	// Render results as HTML
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<div class="border-t pt-4">
		<div class="flex justify-between items-center mb-3">
			<h3 class="text-lg font-semibold text-gray-800">Results from %d workers</h3>
			<span class="text-xs text-gray-400">Command: <code class="bg-gray-100 px-1 rounded">%s</code></span>
		</div>
		<div class="grid grid-cols-1 md:grid-cols-2 gap-3">`, len(workers), esc(command)))

	for _, r := range results {
		borderColor := "green"
		if r.Error != "" {
			borderColor = "red"
		}
		b.WriteString(fmt.Sprintf(`<div class="border-l-4 border-%s-400 bg-white rounded shadow p-3">
			<div class="flex justify-between items-start">
				<div>
					<span class="font-medium text-sm">%s</span>`, borderColor, esc(r.DeviceName)))
		if r.GPU != "" {
			b.WriteString(fmt.Sprintf(`<span class="ml-2 text-xs text-purple-600">%s</span>`, esc(r.GPU)))
		}
		b.WriteString(fmt.Sprintf(`</div>
				<span class="text-xs text-gray-400">%.0fms</span>
			</div>`, r.Duration))

		if r.Error != "" {
			b.WriteString(fmt.Sprintf(`<pre class="mt-2 text-xs text-red-600 bg-red-50 p-2 rounded overflow-x-auto">%s</pre>`, esc(r.Error)))
		} else {
			b.WriteString(fmt.Sprintf(`<pre class="mt-2 text-xs text-gray-700 bg-gray-50 p-2 rounded overflow-x-auto">%s</pre>`, esc(r.Output)))
		}
		b.WriteString(`</div>`)
	}

	b.WriteString(`</div></div>`)
	return c.HTML(http.StatusOK, b.String())
}

// APIOrchExecute runs a command on all orch workers and returns JSON results
func (h *Handler) APIOrchExecute(c echo.Context) error {
	id := c.Param("id")

	var req struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.Command == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "command is required"})
	}
	if err := validateCommand(req.Command); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 30
	}
	if req.TimeoutSeconds > 300 {
		req.TimeoutSeconds = 300
	}

	if h.orchUC == nil || h.executor == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "service not available"})
	}

	orch, err := h.orchUC.GetOrch(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "orch not found"})
	}

	devices, err := h.deviceUC.GetDeviceMap(c.Request().Context())
	if err != nil {
		return internalError(c, "failed to get devices", err)
	}

	// Resolve workers: devices execute directly, sub-orchs delegate
	var results []TaskResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	visited := map[string]bool{id: true} // track visited orchs to prevent cycles

	for _, ref := range orch.WorkerRefs() {
		wg.Add(1)
		if ref.IsOrch() {
			// Sub-orch: execute on its device workers (max 1 level deep)
			go func(orchID string) {
				defer wg.Done()
				start := time.Now()

				// Cycle detection
				if visited[orchID] {
					mu.Lock()
					results = append(results, TaskResult{
						DeviceID: orchID, DeviceName: "orch:" + orchID,
						Error: "circular reference detected", Duration: float64(time.Since(start).Milliseconds()),
					})
					mu.Unlock()
					return
				}

				subOrch, err := h.orchUC.GetOrch(c.Request().Context(), orchID)
				if err != nil {
					mu.Lock()
					results = append(results, TaskResult{
						DeviceID: orchID, DeviceName: "orch:" + orchID,
						Error: "sub-orch not found", Duration: float64(time.Since(start).Milliseconds()),
					})
					mu.Unlock()
					return
				}
				// Execute on sub-orch's device workers only (no deeper nesting)
				for _, subRef := range subOrch.WorkerRefs() {
					if !subRef.IsDevice() {
						continue
					}
					wg.Add(1)
					go func(devID string) {
						defer wg.Done()
						s := time.Now()
						r := h.executeOnDeviceByID(devID, req.Command, timeout, devices)
						r.DeviceName = subOrch.Name + "/" + r.DeviceName
						r.Duration = float64(time.Since(s).Milliseconds())
						mu.Lock()
						results = append(results, r)
						mu.Unlock()
					}(subRef.ID())
				}
			}(ref.ID())
		} else {
			// Direct device worker
			go func(devID string) {
				defer wg.Done()
				start := time.Now()
				r := h.executeOnDeviceByID(devID, req.Command, timeout, devices)
				r.Duration = float64(time.Since(start).Milliseconds())
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}(ref.ID())
		}
	}
	wg.Wait()

	return c.JSON(http.StatusOK, map[string]interface{}{
		"orch_id":   id,
		"command":      req.Command,
		"worker_count": len(results),
		"results":      results,
	})
}

// executeOnDeviceByID runs a command on a single device and returns a TaskResult
func (h *Handler) executeOnDeviceByID(deviceID, command string, timeout time.Duration, devices map[string]*domain.Device) TaskResult {
	dev, ok := devices[deviceID]
	if !ok || !dev.IsOnline() {
		return TaskResult{DeviceID: deviceID, DeviceName: deviceID, Error: "device offline or not found"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	output, err := h.executor.Execute(ctx, dev, command)
	r := TaskResult{DeviceID: dev.ID, DeviceName: dev.GetDisplayName()}
	if dev.HasGPU {
		r.GPU = fmt.Sprintf("%dx %s", dev.GPUCount, dev.GPUModel)
	}
	if err != nil {
		r.Error = err.Error()
	} else {
		r.Output = strings.TrimSpace(output)
	}
	return r
}

// APIExecuteOnDevice runs a command on a single device and returns JSON
func (h *Handler) APIExecuteOnDevice(c echo.Context) error {
	id := c.Param("id")

	var req struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.Command == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "command is required"})
	}
	if err := validateCommand(req.Command); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 30
	}
	if req.TimeoutSeconds > 300 {
		req.TimeoutSeconds = 300
	}

	if h.executor == nil || h.deviceUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "service not available"})
	}

	device, err := h.deviceUC.GetDevice(c.Request().Context(), id)
	if err != nil || device == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "device not found"})
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.TimeoutSeconds)*time.Second)
	defer cancel()

	output, execErr := h.executor.Execute(ctx, device, req.Command)
	r := TaskResult{
		DeviceID:   device.ID,
		DeviceName: device.GetDisplayName(),
		Duration:   float64(time.Since(start).Milliseconds()),
	}
	if device.HasGPU {
		r.GPU = fmt.Sprintf("%dx %s", device.GPUCount, device.GPUModel)
	}
	if execErr != nil {
		r.Error = execErr.Error()
	} else {
		r.Output = strings.TrimSpace(output)
	}

	return c.JSON(http.StatusOK, r)
}

// APIGPUMonitor returns GPU metrics from all GPU nodes.
// By default returns cached device info (no SSH). Use ?refresh=true for live SSH probe.
func (h *Handler) APIGPUMonitor(c echo.Context) error {
	ctx := c.Request().Context()

	if h.deviceUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "service not available"})
	}

	devices, err := h.deviceUC.ListDevices(ctx, false)
	if err != nil {
		return internalError(c, "failed to list devices", err)
	}

	var gpuDevices []*domain.Device
	for _, d := range devices {
		if d.HasGPU && d.IsOnline() {
			gpuDevices = append(gpuDevices, d)
		}
	}

	type GPUNodeStatus struct {
		DeviceID   string           `json:"deviceId"`
		DeviceName string           `json:"deviceName"`
		IP         string           `json:"ip"`
		GPUModel   string           `json:"gpuModel"`
		GPUCount   int              `json:"gpuCount"`
		GPUs       []domain.GPUInfo `json:"gpus"`
		Error      string           `json:"error,omitempty"`
	}

	results := make([]GPUNodeStatus, len(gpuDevices))

	if h.executor == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "executor not available"})
	}

	var wg sync.WaitGroup
	for i, d := range gpuDevices {
		wg.Add(1)
		go func(idx int, dev *domain.Device) {
			defer wg.Done()
			sshCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			output, err := h.executor.Execute(sshCtx, dev, "nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits")
			r := GPUNodeStatus{
				DeviceID:   dev.ID,
				DeviceName: dev.GetDisplayName(),
				IP:         dev.TailscaleIP,
				GPUModel:   dev.GPUModel,
				GPUCount:   dev.GPUCount,
			}
			if err != nil {
				r.Error = err.Error()
			} else {
				gpus, parseErr := domain.ParseNvidiaSmiOutput(output)
				if parseErr != nil {
					r.Error = parseErr.Error()
				} else {
					r.GPUs = gpus
				}
			}
			results[idx] = r
		}(i, d)
	}
	wg.Wait()

	return c.JSON(http.StatusOK, map[string]interface{}{
		"timestamp": time.Now(),
		"nodes":     results,
		"nodeCount": len(gpuDevices),
	})
}

// APIMetricsSnapshot returns system metrics for all devices
func (h *Handler) APIMetricsSnapshot(c echo.Context) error {
	if h.monitorUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "monitor not available"})
	}

	snapshot, err := h.monitorUC.GetAllMetrics(c.Request().Context())
	if err != nil {
		return internalError(c, "failed to get metrics", err)
	}

	return c.JSON(http.StatusOK, snapshot)
}

// MonitorPage renders the GPU monitoring page
func (h *Handler) MonitorPage(c echo.Context) error {
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>GPU Monitor - Naga</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<div class="flex justify-between items-center">
				<h1 class="text-xl font-bold text-gray-800"><a href="/">Naga</a></h1>
				<div class="space-x-4">
					<a href="/devices" class="text-gray-600 hover:text-gray-900">Devices</a>
					<a href="/orchs" class="text-gray-600 hover:text-gray-900">Orchs</a>
					<a href="/monitor" class="text-purple-600 font-semibold">GPU Monitor</a>
				</div>
			</div>
		</div>
	</nav>
	<main class="max-w-7xl mx-auto px-4 py-8">
		<div class="flex justify-between items-center mb-6">
			<h2 class="text-2xl font-bold text-gray-800">GPU Monitor</h2>
			<span class="text-sm text-gray-500">Auto-refresh every 5s</span>
		</div>
		<div id="gpu-status" hx-get="/htmx/gpu-monitor" hx-trigger="load, every 5s" hx-swap="innerHTML">
			<p class="text-gray-400 animate-pulse">Loading GPU status...</p>
		</div>
	</main>
</body>
</html>`)
}

// HTMXGPUMonitor returns GPU status as HTML fragment (auto-refreshed)
func (h *Handler) HTMXGPUMonitor(c echo.Context) error {
	ctx := c.Request().Context()

	if h.deviceUC == nil || h.executor == nil {
		return c.HTML(http.StatusOK, `<p class="text-red-500">Service not available</p>`)
	}

	devices, err := h.deviceUC.ListDevices(ctx, false)
	if err != nil {
		return c.HTML(http.StatusOK, `<p class="text-red-500">Failed to load devices</p>`)
	}

	var gpuDevices []*domain.Device
	for _, d := range devices {
		if d.HasGPU && d.IsOnline() {
			gpuDevices = append(gpuDevices, d)
		}
	}

	if len(gpuDevices) == 0 {
		return c.HTML(http.StatusOK, `<p class="text-gray-500">No GPU devices online</p>`)
	}

	// Collect GPU metrics in parallel
	type nodeResult struct {
		device *domain.Device
		gpus   []domain.GPUInfo
		err    string
	}
	results := make([]nodeResult, len(gpuDevices))
	var wg sync.WaitGroup

	for i, d := range gpuDevices {
		wg.Add(1)
		go func(idx int, dev *domain.Device) {
			defer wg.Done()
			sshCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			output, err := h.executor.Execute(sshCtx, dev, "nvidia-smi --query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits")
			r := nodeResult{device: dev}
			if err != nil {
				r.err = err.Error()
			} else {
				gpus, parseErr := domain.ParseNvidiaSmiOutput(output)
				if parseErr != nil {
					r.err = parseErr.Error()
				} else {
					r.gpus = gpus
				}
			}
			results[idx] = r
		}(i, d)
	}
	wg.Wait()

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<div class="text-xs text-gray-400 mb-4">Updated: %s</div>`, time.Now().Format("15:04:05")))
	b.WriteString(`<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">`)

	for _, r := range results {
		b.WriteString(`<div class="bg-white rounded-lg shadow p-4">`)
		b.WriteString(fmt.Sprintf(`<div class="flex justify-between items-center mb-3">
			<div>
				<span class="font-bold">%s</span>
				<span class="text-xs text-gray-400 ml-2">%s</span>
			</div>
			<span class="text-xs text-purple-600">%dx %s</span>
		</div>`, esc(r.device.Hostname), esc(r.device.TailscaleIP), r.device.GPUCount, esc(r.device.GPUModel)))

		if r.err != "" {
			b.WriteString(fmt.Sprintf(`<p class="text-red-500 text-sm">%s</p>`, esc(r.err)))
		} else {
			for _, gpu := range r.gpus {
				utilColor := "green"
				if gpu.UtilizationPercent > 80 {
					utilColor = "red"
				} else if gpu.UtilizationPercent > 50 {
					utilColor = "yellow"
				}
				memPercent := gpu.MemoryUsagePercent()
				memColor := "green"
				if memPercent > 80 {
					memColor = "red"
				} else if memPercent > 50 {
					memColor = "yellow"
				}
				tempColor := "green"
				if gpu.TemperatureC > 80 {
					tempColor = "red"
				} else if gpu.TemperatureC > 60 {
					tempColor = "yellow"
				}

				b.WriteString(fmt.Sprintf(`<div class="space-y-2 mb-3">
					<div class="flex justify-between text-sm">
						<span>GPU Util</span>
						<span class="font-mono text-%s-600">%.0f%%</span>
					</div>
					<div class="w-full bg-gray-200 rounded-full h-2">
						<div class="bg-%s-500 h-2 rounded-full" style="width: %.0f%%"></div>
					</div>
					<div class="flex justify-between text-sm">
						<span>Memory</span>
						<span class="font-mono text-%s-600">%d / %d MB</span>
					</div>
					<div class="w-full bg-gray-200 rounded-full h-2">
						<div class="bg-%s-500 h-2 rounded-full" style="width: %.0f%%"></div>
					</div>
					<div class="flex justify-between text-xs text-gray-500">
						<span>Temp: <span class="text-%s-600 font-mono">%d°C</span></span>
						<span>Power: <span class="font-mono">%.0fW / %.0fW</span></span>
					</div>
				</div>`,
					utilColor, gpu.UtilizationPercent,
					utilColor, gpu.UtilizationPercent,
					memColor, gpu.MemoryUsedMB, gpu.MemoryTotalMB,
					memColor, memPercent,
					tempColor, gpu.TemperatureC,
					gpu.PowerDrawW, gpu.PowerLimitW,
				))
			}
		}
		b.WriteString(`</div>`)
	}

	b.WriteString(`</div>`)
	return c.HTML(http.StatusOK, b.String())
}

// WorkerProcess represents a process running on a worker node
type WorkerProcess struct {
	PID         string  `json:"pid"`
	ProcessName string  `json:"processName"`
	CPUPercent  float64 `json:"cpuPercent"`
	MemPercent  float64 `json:"memPercent"`
	VRAM_MB     int     `json:"vramMB,omitempty"`
	Command     string  `json:"command"`
	IsGPU       bool    `json:"isGpu"`
}

// WorkerStatus represents the status of a single worker
type WorkerStatus struct {
	DeviceID   string          `json:"deviceId"`
	DeviceName string          `json:"deviceName"`
	GPU        string          `json:"gpu,omitempty"`
	Processes  []WorkerProcess `json:"processes"`
	Error      string          `json:"error,omitempty"`
}

// APIOrchProcesses returns running processes on all orch workers
func (h *Handler) APIOrchProcesses(c echo.Context) error {
	id := c.Param("id")

	if h.orchUC == nil || h.executor == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "service not available"})
	}

	orch, err := h.orchUC.GetOrch(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "orch not found"})
	}

	devices, err := h.deviceUC.GetDeviceMap(c.Request().Context())
	if err != nil {
		return internalError(c, "failed to get devices", err)
	}

	var workers []*domain.Device
	for _, wid := range orch.WorkerIDs {
		if d, ok := devices[wid]; ok && d.IsOnline() {
			workers = append(workers, d)
		}
	}

	statuses := make([]WorkerStatus, len(workers))
	var wg sync.WaitGroup

	script := `echo "==GPU=="; nvidia-smi --query-compute-apps=pid,process_name,used_memory --format=csv,noheader,nounits 2>/dev/null; echo "==CPU=="; ps aux --sort=-%cpu -ww | awk 'NR>1 && NR<=8 {printf "%s|%s|%s|", $2, $3, $4; for(i=11;i<=NF;i++) printf "%s ", $i; print ""}'`

	for i, w := range workers {
		wg.Add(1)
		go func(idx int, dev *domain.Device) {
			defer wg.Done()
			sshCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			ws := WorkerStatus{DeviceID: dev.ID, DeviceName: dev.GetDisplayName()}
			if dev.HasGPU {
				ws.GPU = fmt.Sprintf("%dx %s", dev.GPUCount, dev.GPUModel)
			}

			output, execErr := h.executor.Execute(sshCtx, dev, script)
			if execErr != nil {
				ws.Error = execErr.Error()
				statuses[idx] = ws
				return
			}

			lines := strings.Split(strings.TrimSpace(output), "\n")
			section := ""
			gpuPids := make(map[string]int)

			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "==GPU==" { section = "gpu"; continue }
				if line == "==CPU==" { section = "cpu"; continue }
				if line == "" { continue }

				if section == "gpu" {
					parts := strings.SplitN(line, ", ", 3)
					if len(parts) == 3 {
						pid := strings.TrimSpace(parts[0])
						vram := 0
						fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &vram)
						gpuPids[pid] = vram
						ws.Processes = append(ws.Processes, WorkerProcess{
							PID: pid, ProcessName: strings.TrimSpace(parts[1]),
							VRAM_MB: vram, Command: strings.TrimSpace(parts[1]), IsGPU: true,
						})
					}
				} else if section == "cpu" {
					parts := strings.SplitN(line, "|", 4)
					if len(parts) == 4 {
						pid := strings.TrimSpace(parts[0])
						if _, isGpu := gpuPids[pid]; isGpu { continue }
						var cpuPct, memPct float64
						fmt.Sscanf(strings.TrimSpace(parts[1]), "%f", &cpuPct)
						fmt.Sscanf(strings.TrimSpace(parts[2]), "%f", &memPct)
						cmd := strings.TrimSpace(parts[3])
						if len(cmd) > 80 { cmd = cmd[:80] + "..." }
						ws.Processes = append(ws.Processes, WorkerProcess{
							PID: pid, CPUPercent: cpuPct, MemPercent: memPct,
							Command: cmd, IsGPU: false,
						})
					}
				}
			}
			statuses[idx] = ws
		}(i, w)
	}
	wg.Wait()

	return c.JSON(http.StatusOK, map[string]interface{}{
		"orch_id": id, "timestamp": time.Now(),
		"worker_count": len(workers), "workers": statuses,
	})
}
