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

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/dave/clusterctl/config"
	"github.com/dave/clusterctl/internal/infra/ssh"
	"github.com/dave/clusterctl/internal/infra/tailscale"
	"github.com/dave/clusterctl/internal/repository/sqlite"
	"github.com/dave/clusterctl/internal/usecase"
	"github.com/dave/clusterctl/internal/web/handler"
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
	clusterUC := usecase.NewClusterUseCase(repos, nil) // ray manager set later when needed
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
	h := handler.NewHandler(deviceUC, clusterUC, monitorUC, nil, cfg)

	// Start background metrics collection (every 30 seconds)
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	defer monitorCancel()
	go monitorUC.StartBackgroundCollection(monitorCtx, 30*time.Second)
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
	e.GET("/htmx/clusters", h.HTMXClusterList)
	e.GET("/htmx/clusters/:id", h.HTMXClusterDetail)
	e.GET("/monitor", h.MonitorPage)
	e.GET("/htmx/gpu-monitor", h.HTMXGPUMonitor)
	e.GET("/clusters", h.ClusterList)
	e.GET("/clusters/new", h.ClusterNew)
	e.POST("/clusters", h.ClusterCreate)
	e.GET("/clusters/:id", h.ClusterDetail)
	e.GET("/clusters/:id/execute", h.ClusterExecutePage)
	e.POST("/clusters/:id/execute", h.ClusterExecuteTask)
	e.DELETE("/clusters/:id", h.ClusterDelete)

	// API routes — read-only (no auth required)
	api := e.Group("/api")
	api.GET("/devices", h.APIDeviceList)
	api.GET("/devices/:id", h.APIDeviceDetail)
	api.GET("/devices/:id/metrics", h.APIDeviceMetrics)
	api.GET("/clusters", h.APIClusterList)
	api.GET("/clusters/:id", h.APIClusterDetail)
	api.GET("/clusters/:id/health", h.APIClusterHealth)
	api.GET("/monitor/gpu", h.APIGPUMonitor)
	api.GET("/monitor/snapshot", h.APIMetricsSnapshot)

	// API routes — mutating (Tailscale network auth)
	apiWrite := api.Group("")
	apiWrite.Use(tailscaleAuthMiddleware)
	apiWrite.POST("/clusters", h.APIClusterCreate)
	apiWrite.DELETE("/clusters/:id", h.APIClusterDelete)
	apiWrite.POST("/clusters/:id/start", h.APIClusterStart)
	apiWrite.POST("/clusters/:id/stop", h.APIClusterStop)
	apiWrite.POST("/clusters/:id/workers", h.APIClusterAddWorker)
	apiWrite.DELETE("/clusters/:id/workers/:deviceId", h.APIClusterRemoveWorker)
	apiWrite.PUT("/clusters/:id/head", h.APIClusterChangeHead)
	apiWrite.POST("/clusters/:id/failover", h.APIClusterFailover)
	apiWrite.POST("/clusters/:id/execute", h.APIClusterExecute)
	apiWrite.POST("/devices/:id/execute", h.APIExecuteOnDevice)

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
