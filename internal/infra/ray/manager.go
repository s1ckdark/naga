package ray

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dave/naga/internal/domain"
	"github.com/dave/naga/internal/infra/ssh"
)

// Manager manages Ray cluster operations
type Manager struct {
	executor    *ssh.Executor
	pythonPath  string
	autoInstall bool
	rayVersion  string
}

// Config holds Ray manager configuration
type Config struct {
	PythonPath  string
	AutoInstall bool
	RayVersion  string
}

// NewManager creates a new Ray manager
func NewManager(executor *ssh.Executor, cfg Config) *Manager {
	if cfg.PythonPath == "" {
		cfg.PythonPath = "python3"
	}
	if cfg.RayVersion == "" {
		cfg.RayVersion = "2.9.0"
	}

	return &Manager{
		executor:    executor,
		pythonPath:  cfg.PythonPath,
		autoInstall: cfg.AutoInstall,
		rayVersion:  cfg.RayVersion,
	}
}

// StartHead starts Ray as head node
func (m *Manager) StartHead(ctx context.Context, device *domain.Device, port, dashboardPort int) error {
	// Check if Ray is installed
	installed, _, err := m.CheckRayInstalled(ctx, device)
	if err != nil {
		return fmt.Errorf("failed to check Ray installation: %w", err)
	}

	if !installed {
		if m.autoInstall {
			if err := m.InstallRay(ctx, device, m.rayVersion); err != nil {
				return fmt.Errorf("failed to install Ray: %w", err)
			}
		} else {
			return fmt.Errorf("Ray is not installed on %s. Install it or enable auto_install", device.Name)
		}
	}

	// Stop any existing Ray process
	m.StopRay(ctx, device)

	// Start head node
	cmd := fmt.Sprintf(
		"%s -m ray start --head --port=%d --dashboard-port=%d --dashboard-host=0.0.0.0",
		m.pythonPath, port, dashboardPort,
	)

	output, err := m.executor.Execute(ctx, device, cmd)
	if err != nil {
		return fmt.Errorf("failed to start head: %w (output: %s)", err, output)
	}

	// Wait a bit for Ray to start
	time.Sleep(3 * time.Second)

	// Verify Ray started
	if err := m.checkRayRunning(ctx, device); err != nil {
		return fmt.Errorf("Ray failed to start: %w", err)
	}

	return nil
}

// StartWorker starts Ray as worker node
func (m *Manager) StartWorker(ctx context.Context, device *domain.Device, headAddress string) error {
	// Check if Ray is installed
	installed, _, err := m.CheckRayInstalled(ctx, device)
	if err != nil {
		return fmt.Errorf("failed to check Ray installation: %w", err)
	}

	if !installed {
		if m.autoInstall {
			if err := m.InstallRay(ctx, device, m.rayVersion); err != nil {
				return fmt.Errorf("failed to install Ray: %w", err)
			}
		} else {
			return fmt.Errorf("Ray is not installed on %s. Install it or enable auto_install", device.Name)
		}
	}

	// Stop any existing Ray process
	m.StopRay(ctx, device)

	// Start worker node
	cmd := fmt.Sprintf(
		"%s -m ray start --address=%s",
		m.pythonPath, headAddress,
	)

	output, err := m.executor.Execute(ctx, device, cmd)
	if err != nil {
		return fmt.Errorf("failed to start worker: %w (output: %s)", err, output)
	}

	// Wait a bit for Ray to connect
	time.Sleep(2 * time.Second)

	return nil
}

// StopRay stops Ray on a device
func (m *Manager) StopRay(ctx context.Context, device *domain.Device) error {
	cmd := fmt.Sprintf("%s -m ray stop --force", m.pythonPath)
	_, err := m.executor.Execute(ctx, device, cmd)
	return err
}

// GetClusterInfo gets Ray cluster information from head node
func (m *Manager) GetClusterInfo(ctx context.Context, headDevice *domain.Device) (*domain.RayClusterInfo, error) {
	// Use ray status to get cluster info
	cmd := fmt.Sprintf(`%s -c "
import ray
import json

ray.init(address='auto')
nodes = ray.nodes()
resources = ray.cluster_resources()
available = ray.available_resources()

info = {
    'nodes': [],
    'totalCpus': resources.get('CPU', 0),
    'availCpus': available.get('CPU', 0),
    'totalMemory': int(resources.get('memory', 0)),
    'availMemory': int(available.get('memory', 0)),
    'totalGpus': resources.get('GPU', 0),
    'availGpus': available.get('GPU', 0),
}

for node in nodes:
    info['nodes'].append({
        'nodeId': node['NodeID'],
        'nodeIp': node['NodeManagerAddress'].split(':')[0] if 'NodeManagerAddress' in node else '',
        'isHeadNode': node.get('MetricsExportPort', 0) > 0,
        'state': node['State'],
        'nodeName': node.get('NodeName', ''),
        'resourcesTotal': node.get('Resources', {}),
        'resourcesAvailable': node.get('ResourcesAvailable', {}),
    })

print(json.dumps(info))
ray.shutdown()
"`, m.pythonPath)

	output, err := m.executor.Execute(ctx, headDevice, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster info: %w", err)
	}

	var info domain.RayClusterInfo
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &info); err != nil {
		return nil, fmt.Errorf("failed to parse cluster info: %w", err)
	}

	info.DashboardURL = fmt.Sprintf("http://%s:8265", headDevice.TailscaleIP)

	return &info, nil
}

// CheckRayInstalled checks if Ray is installed on a device
func (m *Manager) CheckRayInstalled(ctx context.Context, device *domain.Device) (bool, string, error) {
	cmd := fmt.Sprintf("%s -c 'import ray; print(ray.__version__)'", m.pythonPath)
	output, err := m.executor.Execute(ctx, device, cmd)
	if err != nil {
		return false, "", nil
	}

	version := strings.TrimSpace(output)
	return version != "", version, nil
}

// InstallRay installs Ray on a device
func (m *Manager) InstallRay(ctx context.Context, device *domain.Device, version string) error {
	if version == "" {
		version = m.rayVersion
	}

	// Install Ray using pip
	cmd := fmt.Sprintf("%s -m pip install 'ray[default]==%s'", m.pythonPath, version)
	output, err := m.executor.Execute(ctx, device, cmd)
	if err != nil {
		return fmt.Errorf("pip install failed: %w (output: %s)", err, output)
	}

	// Verify installation
	installed, installedVersion, err := m.CheckRayInstalled(ctx, device)
	if err != nil {
		return err
	}

	if !installed {
		return fmt.Errorf("Ray installation verification failed")
	}

	fmt.Printf("Ray %s installed on %s\n", installedVersion, device.Name)
	return nil
}

// HasRunningJobs checks if there are running jobs on the cluster
func (m *Manager) HasRunningJobs(ctx context.Context, headDevice *domain.Device) (bool, error) {
	cmd := fmt.Sprintf(`%s -c "
import ray
ray.init(address='auto')
from ray.job_submission import JobSubmissionClient
client = JobSubmissionClient('http://127.0.0.1:8265')
jobs = client.list_jobs()
running = [j for j in jobs if j.status in ['PENDING', 'RUNNING']]
print(len(running))
ray.shutdown()
"`, m.pythonPath)

	output, err := m.executor.Execute(ctx, headDevice, cmd)
	if err != nil {
		// If we can't check, assume no jobs
		return false, nil
	}

	count := strings.TrimSpace(output)
	return count != "0" && count != "", nil
}

// checkRayRunning checks if Ray is running on a device
func (m *Manager) checkRayRunning(ctx context.Context, device *domain.Device) error {
	cmd := fmt.Sprintf(`%s -c "
import ray
ray.init(address='auto')
print('connected')
ray.shutdown()
"`, m.pythonPath)

	output, err := m.executor.Execute(ctx, device, cmd)
	if err != nil {
		return fmt.Errorf("Ray not running: %w", err)
	}

	if !strings.Contains(output, "connected") {
		return fmt.Errorf("Ray not responding correctly")
	}

	return nil
}

// GetRayStatus gets the status of Ray on a device
func (m *Manager) GetRayStatus(ctx context.Context, device *domain.Device) (string, error) {
	cmd := fmt.Sprintf("%s -m ray status", m.pythonPath)
	return m.executor.Execute(ctx, device, cmd)
}
