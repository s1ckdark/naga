package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/s1ckdark/hydra/config"
	"github.com/s1ckdark/hydra/internal/domain"
	"github.com/s1ckdark/hydra/internal/infra/ssh"
	"github.com/s1ckdark/hydra/internal/infra/tailscale"
	"github.com/s1ckdark/hydra/internal/repository/sqlite"
	"github.com/s1ckdark/hydra/internal/usecase"
	"github.com/s1ckdark/hydra/internal/web/handler"
	"github.com/s1ckdark/hydra/internal/web/ws"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

// tailscaleCIDR is the Tailscale CGNAT range (100.64.0.0/10)
var tailscaleCIDR = func() *net.IPNet {
	_, cidr, _ := net.ParseCIDR("100.64.0.0/10")
	return cidr
}()

// isTailscaleOrLocal returns true if the IP is on the Tailscale network or localhost
func isTailscaleOrLocal(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	return tailscaleCIDR.Contains(ip)
}

// tailscaleAuthMiddleware allows requests only from Tailscale network or localhost
func tailscaleAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Use RemoteAddr directly — never trust X-Forwarded-For headers
		if !isTailscaleOrLocal(c.Request().RemoteAddr) {
			return c.JSON(http.StatusForbidden, map[string]string{
				"error": "access denied: must be on Tailscale network",
			})
		}
		return next(c)
	}
}

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Warning: failed to load config: %v", err)
		cfg = config.DefaultConfig()
	}

	// Initialize database
	db, err := sqlite.NewDB(cfg.Database.DSN)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	repos := db.Repositories()

	// Initialize infrastructure
	tsClient := tailscale.NewClient(cfg.Tailscale.APIKey, cfg.Tailscale.Tailnet)

	sshExecutor := ssh.NewExecutor(ssh.Config{
		User:            cfg.SSH.User,
		PrivateKeyPath:  cfg.SSH.PrivateKeyPath,
		Port:            cfg.SSH.Port,
		Timeout:         time.Duration(cfg.SSH.Timeout) * time.Second,
		UseTailscaleSSH: cfg.SSH.UseTailscaleSSH,
	})
	defer sshExecutor.Close()

	sshCollector := ssh.NewCollector(sshExecutor)
	gpuCollector := ssh.NewGPUCollector(sshExecutor)

	// Initialize use cases
	deviceUC := usecase.NewDeviceUseCase(repos, tsClient, sshCollector)
	deviceUC.SetGPUChecker(gpuCollector)
	orchUC := usecase.NewOrchUseCase(repos, nil) // ray manager set later when needed
	monitorUC := usecase.NewMonitorUseCase(repos, sshCollector, deviceUC)

	// Initialize Echo
	e := echo.New()
	e.HideBanner = true

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// CORS: restrict to configured origins
	corsOrigins := cfg.Server.CORSOrigins
	if len(corsOrigins) == 0 {
		corsOrigins = []string{"http://localhost:8080"}
	}
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: corsOrigins,
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
	}))

	// Static files
	e.Static("/static", "internal/web/static")

	// Initialize handlers
	h := handler.NewHandler(deviceUC, orchUC, monitorUC, nil, cfg)

	// Initialize WebSocket hub
	wsHub := ws.NewHub()
	go wsHub.Run()
	h.SetWebSocketHub(wsHub)

	// Initialize task queue
	taskQueue := domain.NewTaskQueue()
	h.SetTaskQueue(taskQueue)

	// Wire WebSocket disconnect handler for immediate task reassignment
	wsHub.SetDisconnectHandler(func(deviceID string) {
		assignedTasks := taskQueue.GetAssignedTasks(deviceID)
		if len(assignedTasks) > 0 {
			log.Printf("[ws] worker %s disconnected with %d tasks, triggering reassignment", deviceID, len(assignedTasks))
			reassigned := taskQueue.ReassignTasksFromDevice(deviceID)
			log.Printf("[ws] %d tasks reassigned from %s", len(reassigned), deviceID)
		}
	})

	// Start background metrics collection (every 30 seconds)
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	defer monitorCancel()
	go monitorUC.StartBackgroundCollection(monitorCtx, 30*time.Second)

	// Start task supervisor (periodic health check + timeout detection)
	taskSupervisor := usecase.NewTaskSupervisor(taskQueue, wsHub, deviceUC, monitorUC)
	aiRegistry := buildAIRegistry(cfg.Agent.AI)
	if arbiter := aiRegistry.TaskSchedulerProvider(); arbiter != nil {
		taskSupervisor.SetAIArbiter(arbiter, 0.10, 5, 3*time.Second)
		log.Printf("[supervisor] AI tiebreaker enabled (provider=%s, epsilon=0.10, budget=5/tick, timeout=3s)", cfg.Agent.AI.Resolve("schedule").Provider)
	}
	go taskSupervisor.Start(monitorCtx)
	h.SetExecutor(sshExecutor)

	// Routes
	e.GET("/", h.Dashboard)
	e.GET("/devices", h.DeviceList)
	e.GET("/devices/:id", h.DeviceDetail)
	e.GET("/htmx/devices", h.HTMXDeviceList)
	e.GET("/htmx/devices/:id", h.HTMXDeviceDetail)
	e.GET("/htmx/device-count", h.HTMXDeviceCount)
	e.GET("/htmx/device-options", h.HTMXDeviceOptions)
	e.GET("/htmx/device-checkboxes", h.HTMXDeviceCheckboxes)
	e.GET("/htmx/orchs", h.HTMXOrchList)
	e.GET("/htmx/orchs/:id", h.HTMXOrchDetail)
	e.GET("/monitor", h.MonitorPage)
	e.GET("/htmx/gpu-monitor", h.HTMXGPUMonitor)
	e.GET("/orchs", h.OrchList)
	e.GET("/orchs/new", h.OrchNew)
	e.POST("/orchs", h.OrchCreate)
	e.GET("/orchs/:id", h.OrchDetail)
	e.GET("/orchs/:id/execute", h.OrchExecutePage)
	e.POST("/orchs/:id/execute", h.OrchExecuteTask)
	e.DELETE("/orchs/:id", h.OrchDelete)

	// API routes — read-only (no auth required)
	api := e.Group("/api")
	api.GET("/devices", h.APIDeviceList)
	api.GET("/devices/:id", h.APIDeviceDetail)
	api.GET("/devices/:id/metrics", h.APIDeviceMetrics)
	api.GET("/orchs", h.APIOrchList)
	api.GET("/orchs/:id", h.APIOrchDetail)
	api.GET("/orchs/:id/health", h.APIOrchHealth)
	api.GET("/monitor/gpu", h.APIGPUMonitor)
	api.GET("/orchs/:id/processes", h.APIOrchProcesses)
	api.GET("/monitor/snapshot", h.APIMetricsSnapshot)

	// Auth info endpoint
	api.GET("/auth/me", func(c echo.Context) error {
		host, _, _ := net.SplitHostPort(c.Request().RemoteAddr)
		if host == "" {
			host = c.Request().RemoteAddr
		}

		if !isTailscaleOrLocal(c.Request().RemoteAddr) {
			return c.JSON(http.StatusForbidden, map[string]string{
				"error": "not on Tailscale network",
			})
		}

		// Find device matching this IP
		devices, _ := deviceUC.ListDevices(c.Request().Context(), false)
		var matchedDevice *domain.Device
		for _, d := range devices {
			for _, ip := range d.IPAddresses {
				if ip == host {
					matchedDevice = d
					break
				}
			}
			if d.TailscaleIP == host {
				matchedDevice = d
			}
			if matchedDevice != nil {
				break
			}
		}

		result := map[string]interface{}{
			"authenticated": true,
			"ip":            host,
			"network":       "tailscale",
		}

		if matchedDevice != nil {
			result["device"] = matchedDevice
			result["user"] = matchedDevice.User
		}

		return c.JSON(http.StatusOK, result)
	})

	// API routes — mutating (Tailscale network auth)
	apiWrite := api.Group("")
	apiWrite.Use(tailscaleAuthMiddleware)
	apiWrite.POST("/orchs", h.APIOrchCreate)
	apiWrite.DELETE("/orchs/:id", h.APIOrchDelete)
	apiWrite.POST("/orchs/:id/start", h.APIOrchStart)
	apiWrite.POST("/orchs/:id/stop", h.APIOrchStop)
	apiWrite.POST("/orchs/:id/workers", h.APIOrchAddWorker)
	apiWrite.DELETE("/orchs/:id/workers/:deviceId", h.APIOrchRemoveWorker)
	apiWrite.PUT("/orchs/:id/head", h.APIOrchChangeHead)
	apiWrite.POST("/orchs/:id/failover", h.APIOrchFailover)
	apiWrite.POST("/orchs/:id/execute", h.APIOrchExecute)
	apiWrite.POST("/devices/:id/execute", h.APIExecuteOnDevice)

	// WebSocket endpoint
	e.GET("/ws", h.HandleWebSocket)

	// Task API routes
	api.GET("/tasks", h.APITaskList)
	api.GET("/tasks/:id", h.APITaskDetail)
	api.POST("/tasks", h.APITaskCreate)
	api.PUT("/tasks/:id/status", h.APITaskUpdateStatus)
	api.PUT("/tasks/:id/result", h.APITaskSetResult)

	// Capability routes
	api.POST("/devices/:id/capabilities", h.APIRegisterCapabilities)
	api.GET("/devices/:id/capabilities", h.APIGetCapabilities)

	// Config routes (Tailscale network auth required)
	apiWrite.GET("/config/tailscale", h.APIGetTailscaleConfig)
	apiWrite.PUT("/config/tailscale", h.APIPutTailscaleConfig)

	// Health check
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"status":  "healthy",
			"version": Version,
		})
	})

	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Starting server at http://%s", addr)
	log.Printf("Version: %s (built: %s)", Version, BuildTime)

	// Graceful shutdown
	go func() {
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Register mDNS/Bonjour service for auto-discovery
	mdnsServer, err := zeroconf.Register(
		"hydra-server",      // service instance name
		"_hydra._tcp",       // service type
		"local.",           // domain
		cfg.Server.Port,    // port
		[]string{"version=" + Version}, // TXT records
		nil,                // interfaces (nil = all)
	)
	if err != nil {
		log.Printf("Warning: failed to register mDNS service: %v", err)
	} else {
		log.Printf("mDNS: registered _hydra._tcp on port %d", cfg.Server.Port)
		defer mdnsServer.Shutdown()
	}

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
}
