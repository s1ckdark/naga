package main

import (
	"context"
	"fmt"
	"log"
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

	// Initialize use cases
	deviceUC := usecase.NewDeviceUseCase(repos, tsClient, sshCollector)
	// monitorUC := usecase.NewMonitorUseCase(repos, sshCollector, deviceUC)
	// clusterUC would need ray manager

	// Initialize Echo
	e := echo.New()
	e.HideBanner = true

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// Static files
	e.Static("/static", "internal/web/static")

	// Initialize handlers
	h := handler.NewHandler(deviceUC, nil, nil, cfg)

	// Routes
	e.GET("/", h.Dashboard)
	e.GET("/devices", h.DeviceList)
	e.GET("/devices/:id", h.DeviceDetail)
	e.GET("/clusters", h.ClusterList)
	e.GET("/clusters/new", h.ClusterNew)
	e.POST("/clusters", h.ClusterCreate)
	e.GET("/clusters/:id", h.ClusterDetail)
	e.DELETE("/clusters/:id", h.ClusterDelete)

	// API routes
	api := e.Group("/api")
	api.GET("/devices", h.APIDeviceList)
	api.GET("/devices/:id", h.APIDeviceDetail)
	api.GET("/devices/:id/metrics", h.APIDeviceMetrics)
	api.GET("/clusters", h.APIClusterList)
	api.POST("/clusters", h.APIClusterCreate)
	api.GET("/clusters/:id", h.APIClusterDetail)
	api.DELETE("/clusters/:id", h.APIClusterDelete)
	api.POST("/clusters/:id/start", h.APIClusterStart)
	api.POST("/clusters/:id/stop", h.APIClusterStop)
	api.POST("/clusters/:id/workers", h.APIClusterAddWorker)
	api.DELETE("/clusters/:id/workers/:deviceId", h.APIClusterRemoveWorker)
	api.PUT("/clusters/:id/head", h.APIClusterChangeHead)

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
