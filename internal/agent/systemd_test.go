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

	unit, err := GenerateSystemdUnit(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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
		{"has EnvironmentFile", "EnvironmentFile=/etc/cluster-agent/cluster-agent-mycluster-worker1.env"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(unit, c.contains) {
				t.Errorf("unit file does not contain %q\n\nGot:\n%s", c.contains, unit)
			}
		})
	}

	// Must NOT contain inline API key
	if strings.Contains(unit, "sk-test-key") {
		t.Error("unit file must not contain the API key inline")
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

	unit, err := GenerateSystemdUnit(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(unit, "ANTHROPIC_API_KEY") {
		t.Error("unit file should not contain ANTHROPIC_API_KEY when APIKey is empty")
	}
	if strings.Contains(unit, "EnvironmentFile") {
		t.Error("unit file should not contain EnvironmentFile when APIKey is empty")
	}
}

func TestGenerateEnvFile(t *testing.T) {
	cfg := SystemdConfig{
		APIKey: "sk-test-key-123",
	}
	content := GenerateEnvFile(cfg)
	if content != "ANTHROPIC_API_KEY=sk-test-key-123\n" {
		t.Errorf("unexpected env file content: %q", content)
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
		APIKey:     "sk-test",
	}

	cmds, err := InstallCommands(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasReload := false
	hasEnable := false
	hasMkdir := false
	hasInstallEnv := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, "daemon-reload") {
			hasReload = true
		}
		if strings.Contains(cmd, "enable") {
			hasEnable = true
		}
		if strings.Contains(cmd, "mkdir -p /etc/cluster-agent") {
			hasMkdir = true
		}
		if strings.Contains(cmd, "install -m 0600") {
			hasInstallEnv = true
		}
	}

	if !hasReload {
		t.Error("install commands should contain daemon-reload")
	}
	if !hasEnable {
		t.Error("install commands should contain enable")
	}
	if !hasMkdir {
		t.Error("install commands should create env file directory")
	}
	if !hasInstallEnv {
		t.Error("install commands should install env file with mode 0600")
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

func TestValidateIdentifier(t *testing.T) {
	valid := []string{"worker1", "my-cluster", "node.01", "head_2", "a"}
	for _, s := range valid {
		if err := ValidateIdentifier(s); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", s, err)
		}
	}

	invalid := []string{"", "no spaces", "semi;colon", "back`tick", "$(cmd)", "../path", "a/b", "key=val"}
	for _, s := range invalid {
		if err := ValidateIdentifier(s); err == nil {
			t.Errorf("expected %q to be invalid, got nil error", s)
		}
	}
}

func TestGenerateSystemdUnitValidation(t *testing.T) {
	cfg := SystemdConfig{
		NodeID:     "worker1; rm -rf /",
		ClusterID:  "mycluster",
		Role:       "worker",
		Port:       9090,
		BinaryPath: "/usr/local/bin/cluster-agent",
	}

	_, err := GenerateSystemdUnit(cfg)
	if err == nil {
		t.Error("expected error for invalid NodeID with shell metacharacters")
	}
}

func TestInstallCommandsValidation(t *testing.T) {
	cfg := SystemdConfig{
		NodeID:     "$(evil)",
		ClusterID:  "mycluster",
		Role:       "worker",
		Port:       9090,
		BinaryPath: "/usr/local/bin/cluster-agent",
	}

	_, err := InstallCommands(cfg)
	if err == nil {
		t.Error("expected error for invalid NodeID with command injection")
	}
}

func TestValidatedServiceName(t *testing.T) {
	_, err := ValidatedServiceName("good-cluster", "good-node")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	_, err = ValidatedServiceName("bad cluster", "node")
	if err == nil {
		t.Error("expected error for invalid clusterID")
	}
}
