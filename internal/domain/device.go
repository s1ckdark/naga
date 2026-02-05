package domain

import "time"

// DeviceStatus represents the current status of a device
type DeviceStatus string

const (
	DeviceStatusOnline      DeviceStatus = "online"
	DeviceStatusOffline     DeviceStatus = "offline"
	DeviceStatusUnreachable DeviceStatus = "unreachable"
)

// Device represents a machine in the Tailscale network
type Device struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Hostname      string       `json:"hostname"`
	IPAddresses   []string     `json:"ipAddresses"`
	TailscaleIP   string       `json:"tailscaleIp"`
	OS            string       `json:"os"`
	Status        DeviceStatus `json:"status"`
	IsExternal    bool         `json:"isExternal"`
	Tags          []string     `json:"tags"`
	User          string       `json:"user"`
	LastSeen      time.Time    `json:"lastSeen"`
	CreatedAt     time.Time    `json:"createdAt"`
	SSHEnabled    bool         `json:"sshEnabled"`
	RayInstalled  bool         `json:"rayInstalled"`
	RayVersion    string       `json:"rayVersion"`
	PythonVersion string       `json:"pythonVersion"`
}

// IsOnline returns true if the device is currently online
func (d *Device) IsOnline() bool {
	return d.Status == DeviceStatusOnline
}

// CanSSH returns true if SSH connection is possible
func (d *Device) CanSSH() bool {
	return d.IsOnline() && d.SSHEnabled
}

// GetDisplayName returns the best name to display for this device
func (d *Device) GetDisplayName() string {
	if d.Name != "" {
		return d.Name
	}
	return d.Hostname
}

// DeviceFilter represents filters for querying devices
type DeviceFilter struct {
	Status      *DeviceStatus
	HasTag      string
	OS          string
	RayInstalled *bool
	SSHEnabled  *bool
}
