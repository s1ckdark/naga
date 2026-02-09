package handler

import (
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"

	"github.com/dave/clusterctl/config"
	"github.com/dave/clusterctl/internal/usecase"
)

// Handler handles HTTP requests
type Handler struct {
	deviceUC  *usecase.DeviceUseCase
	clusterUC *usecase.ClusterUseCase
	monitorUC *usecase.MonitorUseCase
	cfg       *config.Config
}

// NewHandler creates a new Handler
func NewHandler(
	deviceUC *usecase.DeviceUseCase,
	clusterUC *usecase.ClusterUseCase,
	monitorUC *usecase.MonitorUseCase,
	cfg *config.Config,
) *Handler {
	return &Handler{
		deviceUC:  deviceUC,
		clusterUC: clusterUC,
		monitorUC: monitorUC,
		cfg:       cfg,
	}
}

// Dashboard renders the main dashboard
func (h *Handler) Dashboard(c echo.Context) error {
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>Cluster Manager</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<div class="flex justify-between items-center">
				<h1 class="text-xl font-bold text-gray-800">Cluster Manager</h1>
				<div class="space-x-4">
					<a href="/devices" class="text-gray-600 hover:text-gray-900">Devices</a>
					<a href="/clusters" class="text-gray-600 hover:text-gray-900">Clusters</a>
				</div>
			</div>
		</div>
	</nav>
	<main class="max-w-7xl mx-auto px-4 py-8">
		<div class="grid grid-cols-1 md:grid-cols-3 gap-6">
			<div class="bg-white rounded-lg shadow p-6">
				<h2 class="text-lg font-semibold text-gray-700">Devices</h2>
				<p class="text-3xl font-bold text-blue-600 mt-2" hx-get="/api/devices" hx-trigger="load" hx-swap="innerHTML" hx-select=".count">Loading...</p>
				<a href="/devices" class="text-blue-500 text-sm">View all →</a>
			</div>
			<div class="bg-white rounded-lg shadow p-6">
				<h2 class="text-lg font-semibold text-gray-700">Clusters</h2>
				<p class="text-3xl font-bold text-green-600 mt-2">0</p>
				<a href="/clusters" class="text-blue-500 text-sm">View all →</a>
			</div>
			<div class="bg-white rounded-lg shadow p-6">
				<h2 class="text-lg font-semibold text-gray-700">Status</h2>
				<p class="text-lg text-green-600 mt-2">All systems operational</p>
			</div>
		</div>

		<div class="mt-8 bg-white rounded-lg shadow">
			<div class="px-6 py-4 border-b">
				<h2 class="text-lg font-semibold text-gray-700">Quick Actions</h2>
			</div>
			<div class="p-6 grid grid-cols-1 md:grid-cols-2 gap-4">
				<a href="/clusters/new" class="block p-4 bg-blue-50 rounded-lg hover:bg-blue-100">
					<h3 class="font-semibold text-blue-700">Create Cluster</h3>
					<p class="text-sm text-gray-600">Set up a new Ray cluster</p>
				</a>
				<a href="/devices" class="block p-4 bg-green-50 rounded-lg hover:bg-green-100">
					<h3 class="font-semibold text-green-700">Monitor Devices</h3>
					<p class="text-sm text-gray-600">View resource usage</p>
				</a>
			</div>
		</div>
	</main>
</body>
</html>`)
}

// DeviceList renders the device list page
func (h *Handler) DeviceList(c echo.Context) error {
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>Devices - Cluster Manager</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<div class="flex justify-between items-center">
				<h1 class="text-xl font-bold text-gray-800"><a href="/">Cluster Manager</a></h1>
				<div class="space-x-4">
					<a href="/devices" class="text-blue-600 font-semibold">Devices</a>
					<a href="/clusters" class="text-gray-600 hover:text-gray-900">Clusters</a>
				</div>
			</div>
		</div>
	</nav>
	<main class="max-w-7xl mx-auto px-4 py-8">
		<div class="flex justify-between items-center mb-6">
			<h2 class="text-2xl font-bold text-gray-800">Devices</h2>
			<button hx-get="/api/devices" hx-target="#device-list" hx-swap="innerHTML" class="bg-blue-500 text-white px-4 py-2 rounded hover:bg-blue-600">
				Refresh
			</button>
		</div>
		<div id="device-list" hx-get="/api/devices" hx-trigger="load" hx-swap="innerHTML">
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
	<title>Device Details - Cluster Manager</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<a href="/" class="text-xl font-bold text-gray-800">Cluster Manager</a>
		</div>
	</nav>
		<main class="max-w-7xl mx-auto px-4 py-8">
			<div hx-get="/api/devices/`+escapedID+`" hx-trigger="load" hx-swap="innerHTML">
				Loading device details...
			</div>
		</main>
</body>
</html>`)
}

// ClusterList renders the cluster list page
func (h *Handler) ClusterList(c echo.Context) error {
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>Clusters - Cluster Manager</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<div class="flex justify-between items-center">
				<h1 class="text-xl font-bold text-gray-800"><a href="/">Cluster Manager</a></h1>
				<div class="space-x-4">
					<a href="/devices" class="text-gray-600 hover:text-gray-900">Devices</a>
					<a href="/clusters" class="text-blue-600 font-semibold">Clusters</a>
				</div>
			</div>
		</div>
	</nav>
	<main class="max-w-7xl mx-auto px-4 py-8">
		<div class="flex justify-between items-center mb-6">
			<h2 class="text-2xl font-bold text-gray-800">Clusters</h2>
			<a href="/clusters/new" class="bg-blue-500 text-white px-4 py-2 rounded hover:bg-blue-600">
				Create Cluster
			</a>
		</div>
		<div id="cluster-list" hx-get="/api/clusters" hx-trigger="load" hx-swap="innerHTML">
			<p class="text-gray-500">Loading clusters...</p>
		</div>
	</main>
</body>
</html>`)
}

// ClusterNew renders the new cluster form
func (h *Handler) ClusterNew(c echo.Context) error {
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>Create Cluster - Cluster Manager</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<a href="/" class="text-xl font-bold text-gray-800">Cluster Manager</a>
		</div>
	</nav>
	<main class="max-w-7xl mx-auto px-4 py-8">
		<div class="bg-white rounded-lg shadow p-6 max-w-2xl">
			<h2 class="text-2xl font-bold text-gray-800 mb-6">Create New Cluster</h2>
			<form hx-post="/api/clusters" hx-target="#result" class="space-y-4">
				<div>
					<label class="block text-sm font-medium text-gray-700">Cluster Name</label>
					<input type="text" name="name" required class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500 p-2 border">
				</div>
				<div>
					<label class="block text-sm font-medium text-gray-700">Description</label>
					<textarea name="description" rows="2" class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500 p-2 border"></textarea>
				</div>
				<div>
					<label class="block text-sm font-medium text-gray-700">Head Node</label>
					<select name="head" required class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500 p-2 border" hx-get="/api/devices" hx-trigger="load" hx-swap="innerHTML">
						<option>Loading devices...</option>
					</select>
				</div>
				<div>
					<label class="block text-sm font-medium text-gray-700">Worker Nodes</label>
					<div id="worker-select" hx-get="/api/devices" hx-trigger="load" hx-swap="innerHTML">
						Loading devices...
					</div>
				</div>
				<div class="pt-4">
					<button type="submit" class="bg-blue-500 text-white px-4 py-2 rounded hover:bg-blue-600">
						Create Cluster
					</button>
					<a href="/clusters" class="ml-4 text-gray-600 hover:text-gray-800">Cancel</a>
				</div>
			</form>
			<div id="result" class="mt-4"></div>
		</div>
	</main>
</body>
</html>`)
}

// ClusterCreate handles cluster creation (form submission)
func (h *Handler) ClusterCreate(c echo.Context) error {
	return h.APIClusterCreate(c)
}

// ClusterDetail renders the cluster detail page
func (h *Handler) ClusterDetail(c echo.Context) error {
	id := c.Param("id")
	escapedID := url.PathEscape(id)
	return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html>
<head>
	<title>Cluster Details - Cluster Manager</title>
	<script src="https://unpkg.com/htmx.org@1.9.10"></script>
	<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-100">
	<nav class="bg-white shadow">
		<div class="max-w-7xl mx-auto px-4 py-4">
			<a href="/" class="text-xl font-bold text-gray-800">Cluster Manager</a>
		</div>
	</nav>
		<main class="max-w-7xl mx-auto px-4 py-8">
			<div hx-get="/api/clusters/`+escapedID+`" hx-trigger="load" hx-swap="innerHTML">
				Loading cluster details...
			</div>
		</main>
</body>
</html>`)
}

// ClusterDelete handles cluster deletion (form submission)
func (h *Handler) ClusterDelete(c echo.Context) error {
	return h.APIClusterDelete(c)
}

// APIClusterCreate handles cluster creation API
func (h *Handler) APIClusterCreate(c echo.Context) error {
	name := c.FormValue("name")
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}

	headID := c.FormValue("head")
	if headID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "head node is required"})
	}

	// TODO: Create cluster using usecase
	return c.JSON(http.StatusOK, map[string]string{
		"status":  "created",
		"name":    name,
		"message": "Cluster created successfully",
	})
}

// APIClusterDelete handles cluster deletion API
func (h *Handler) APIClusterDelete(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "cluster id is required"})
	}

	// TODO: Delete cluster using usecase
	return c.JSON(http.StatusOK, map[string]string{
		"status":  "deleted",
		"id":      id,
		"message": "Cluster deleted successfully",
	})
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
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
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
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, metrics)
}

// APIClusterList returns list of clusters as JSON
func (h *Handler) APIClusterList(c echo.Context) error {
	ctx := c.Request().Context()

	if h.clusterUC == nil {
		return c.JSON(http.StatusOK, []interface{}{})
	}

	clusters, err := h.clusterUC.ListClusters(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, clusters)
}

// APIClusterDetail returns cluster details as JSON
func (h *Handler) APIClusterDetail(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.clusterUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "cluster service not available"})
	}

	cluster, err := h.clusterUC.GetCluster(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, cluster)
}

// APIClusterStart starts a cluster
func (h *Handler) APIClusterStart(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	if h.clusterUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "cluster service not available"})
	}

	// Get device map for cluster operations
	devices, err := h.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get devices: " + err.Error()})
	}

	err = h.clusterUC.StartCluster(ctx, id, devices)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "started", "id": id})
}

// APIClusterStop stops a cluster
func (h *Handler) APIClusterStop(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()
	force := c.QueryParam("force") == "true"

	if h.clusterUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "cluster service not available"})
	}

	devices, err := h.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get devices: " + err.Error()})
	}

	err = h.clusterUC.StopCluster(ctx, id, devices, force)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "stopped", "id": id})
}

// APIClusterAddWorker adds a worker to a cluster
func (h *Handler) APIClusterAddWorker(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if h.clusterUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "cluster service not available"})
	}

	devices, err := h.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get devices: " + err.Error()})
	}

	// Get the cluster to find head device
	cluster, err := h.clusterUC.GetCluster(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}

	device := devices[req.DeviceID]
	headDevice := devices[cluster.HeadNodeID]

	err = h.clusterUC.AddWorker(ctx, id, req.DeviceID, device, headDevice)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "worker_added", "cluster_id": id, "device_id": req.DeviceID})
}

// APIClusterRemoveWorker removes a worker from a cluster
func (h *Handler) APIClusterRemoveWorker(c echo.Context) error {
	id := c.Param("id")
	deviceID := c.Param("deviceId")
	ctx := c.Request().Context()

	if h.clusterUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "cluster service not available"})
	}

	devices, err := h.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get devices: " + err.Error()})
	}

	device := devices[deviceID]

	err = h.clusterUC.RemoveWorker(ctx, id, deviceID, device)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "worker_removed", "cluster_id": id, "device_id": deviceID})
}

// APIClusterChangeHead changes the head node of a cluster
func (h *Handler) APIClusterChangeHead(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	var req struct {
		NewHeadID string `json:"new_head_id"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if h.clusterUC == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "cluster service not available"})
	}

	devices, err := h.deviceUC.GetDeviceMap(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get devices: " + err.Error()})
	}

	err = h.clusterUC.ChangeHead(ctx, id, req.NewHeadID, devices)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "head_changed", "cluster_id": id, "new_head_id": req.NewHeadID})
}
