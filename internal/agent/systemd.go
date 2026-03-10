package agent

import (
	"fmt"
	"strings"
)

// SystemdConfig holds configuration for generating a systemd unit file.
type SystemdConfig struct {
	NodeID     string
	ClusterID  string
	Role       string
	Port       int
	BinaryPath string
	APIKey     string
}

// GenerateSystemdUnit returns the content of a systemd unit file for the agent.
func GenerateSystemdUnit(cfg SystemdConfig) string {
	envLine := ""
	if cfg.APIKey != "" {
		envLine = fmt.Sprintf("Environment=ANTHROPIC_API_KEY=%s", cfg.APIKey)
	}

	lines := []string{
		"[Unit]",
		fmt.Sprintf("Description=Cluster Agent for %s node %s", cfg.ClusterID, cfg.NodeID),
		"After=network-online.target",
		"Wants=network-online.target",
		"",
		"[Service]",
		"Type=simple",
		fmt.Sprintf("ExecStart=%s --node-id %s --cluster-id %s --role %s --port %d",
			cfg.BinaryPath, cfg.NodeID, cfg.ClusterID, cfg.Role, cfg.Port),
		"Restart=always",
		"RestartSec=5",
	}

	if envLine != "" {
		lines = append(lines, envLine)
	}

	lines = append(lines,
		"StandardOutput=journal",
		"StandardError=journal",
		fmt.Sprintf("SyslogIdentifier=%s", ServiceName(cfg.ClusterID, cfg.NodeID)),
		"",
		"[Install]",
		"WantedBy=multi-user.target",
		"",
	)

	return strings.Join(lines, "\n")
}

// ServiceName returns the systemd service name for a given cluster and node.
func ServiceName(clusterID, nodeID string) string {
	return fmt.Sprintf("cluster-agent-%s-%s", clusterID, nodeID)
}

// UnitFilePath returns the path to the systemd unit file.
func UnitFilePath(clusterID, nodeID string) string {
	return fmt.Sprintf("/etc/systemd/system/%s.service", ServiceName(clusterID, nodeID))
}

// InstallCommands returns the shell commands to install and start the systemd service.
func InstallCommands(cfg SystemdConfig) []string {
	name := ServiceName(cfg.ClusterID, cfg.NodeID)
	unitPath := UnitFilePath(cfg.ClusterID, cfg.NodeID)
	return []string{
		fmt.Sprintf("tee %s", unitPath),
		"systemctl daemon-reload",
		fmt.Sprintf("systemctl enable %s", name),
		fmt.Sprintf("systemctl start %s", name),
	}
}

// UninstallCommands returns the shell commands to stop, disable, and remove the systemd service.
func UninstallCommands(clusterID, nodeID string) []string {
	name := ServiceName(clusterID, nodeID)
	unitPath := UnitFilePath(clusterID, nodeID)
	return []string{
		fmt.Sprintf("systemctl stop %s", name),
		fmt.Sprintf("systemctl disable %s", name),
		fmt.Sprintf("rm %s", unitPath),
		"systemctl daemon-reload",
	}
}
