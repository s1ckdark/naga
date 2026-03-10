package agent

import (
	"strings"
	"testing"
)

func TestGenerateSystemdUnit(t *testing.T) {
	cfg := SystemdConfig{
		NodeID:     "worker1",
		ClusterID:  "mycluster",
		Role:       "worker",
		Port:       9090,
		BinaryPath: "/usr/local/bin/cluster-agent",
		APIKey:     "sk-test-key",
	}

	unit := GenerateSystemdUnit(cfg)

	checks := []struct {
		name     string
		contains string
	}{
		{"has Unit section", "[Unit]"},
		{"has Service section", "[Service]"},
		{"has Install section", "[Install]"},
		{"has ExecStart", "ExecStart=/usr/local/bin/cluster-agent"},
		{"has Restart=always", "Restart=always"},
		{"has node-id flag", "--node-id worker1"},
		{"has cluster-id flag", "--cluster-id mycluster"},
		{"has role flag", "--role worker"},
		{"has port flag", "--port 9090"},
		{"has API key env", "Environment=ANTHROPIC_API_KEY=sk-test-key"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(unit, c.contains) {
				t.Errorf("unit file does not contain %q\n\nGot:\n%s", c.contains, unit)
			}
		})
	}
}

func TestGenerateSystemdUnitNoAPIKey(t *testing.T) {
	cfg := SystemdConfig{
		NodeID:     "head1",
		ClusterID:  "testcluster",
		Role:       "head",
		Port:       8080,
		BinaryPath: "/usr/bin/cluster-agent",
	}

	unit := GenerateSystemdUnit(cfg)

	if strings.Contains(unit, "ANTHROPIC_API_KEY") {
		t.Error("unit file should not contain ANTHROPIC_API_KEY when APIKey is empty")
	}
}

func TestServiceName(t *testing.T) {
	name := ServiceName("mycluster", "worker1")
	if name != "cluster-agent-mycluster-worker1" {
		t.Errorf("expected cluster-agent-mycluster-worker1, got %s", name)
	}
}

func TestUnitFilePath(t *testing.T) {
	path := UnitFilePath("mycluster", "worker1")
	expected := "/etc/systemd/system/cluster-agent-mycluster-worker1.service"
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestInstallCommands(t *testing.T) {
	cfg := SystemdConfig{
		NodeID:     "worker1",
		ClusterID:  "mycluster",
		Role:       "worker",
		Port:       9090,
		BinaryPath: "/usr/local/bin/cluster-agent",
	}

	cmds := InstallCommands(cfg)

	hasReload := false
	hasEnable := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "daemon-reload") {
			hasReload = true
		}
		if strings.Contains(cmd, "enable") {
			hasEnable = true
		}
	}

	if !hasReload {
		t.Error("install commands should contain daemon-reload")
	}
	if !hasEnable {
		t.Error("install commands should contain enable")
	}
}

func TestUninstallCommands(t *testing.T) {
	cmds := UninstallCommands("mycluster", "worker1")

	hasStop := false
	hasDisable := false
	hasReload := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "stop") {
			hasStop = true
		}
		if strings.Contains(cmd, "disable") {
			hasDisable = true
		}
		if strings.Contains(cmd, "daemon-reload") {
			hasReload = true
		}
	}

	if !hasStop {
		t.Error("uninstall commands should contain stop")
	}
	if !hasDisable {
		t.Error("uninstall commands should contain disable")
	}
	if !hasReload {
		t.Error("uninstall commands should contain daemon-reload")
	}
}
