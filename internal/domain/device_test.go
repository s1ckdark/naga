package domain

import (
	"testing"
)

func TestDevice_IsOnline(t *testing.T) {
	tests := []struct {
		status DeviceStatus
		want   bool
	}{
		{DeviceStatusOnline, true},
		{DeviceStatusOffline, false},
		{DeviceStatusUnreachable, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			d := &Device{Status: tt.status}
			if got := d.IsOnline(); got != tt.want {
				t.Errorf("IsOnline() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDevice_CanSSH(t *testing.T) {
	tests := []struct {
		name       string
		status     DeviceStatus
		sshEnabled bool
		want       bool
	}{
		{"online + ssh", DeviceStatusOnline, true, true},
		{"online + no ssh", DeviceStatusOnline, false, false},
		{"offline + ssh", DeviceStatusOffline, true, false},
		{"offline + no ssh", DeviceStatusOffline, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Device{Status: tt.status, SSHEnabled: tt.sshEnabled}
			if got := d.CanSSH(); got != tt.want {
				t.Errorf("CanSSH() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDevice_GetDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		device   Device
		want     string
	}{
		{"has name", Device{Name: "my-server", Hostname: "host1"}, "my-server"},
		{"no name", Device{Name: "", Hostname: "host1"}, "host1"},
		{"both empty", Device{Name: "", Hostname: ""}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.device.GetDisplayName(); got != tt.want {
				t.Errorf("GetDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}
