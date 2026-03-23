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
	HasGPU        bool         `json:"hasGpu"`
	GPUModel      string       `json:"gpuModel,omitempty"`
	GPUCount      int          `json:"gpuCount"`
	Capabilities   []string    `json:"capabilities,omitempty"`   // e.g., ["compute","gpu","gps","camera","sms"]
	DeviceToken    string      `json:"deviceToken,omitempty"`    // APNS token for push notifications
	ConnectionType string      `json:"connectionType,omitempty"` // "ssh", "websocket", "offline"
	APIKeyHash     string      `json:"-"`                        // hashed API key for external auth
}

// IsGPUCandidate returns true if the device could potentially have a GPU (Linux + SSH)
func (d *Device) IsGPUCandidate() bool {
	return d.OS == "Linux" && d.CanSSH()
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

// HasCapability returns true if the device has the given capability
func (d *Device) HasCapability(cap string) bool {
	for _, c := range d.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// IsMobile returns true if the device OS is iOS or Android
func (d *Device) IsMobile() bool {
	return d.OS == "iOS" || d.OS == "Android"
}

// DeviceFilter represents filters for querying devices
type DeviceFilter struct {
	Status      *DeviceStatus
	HasTag      string
	OS          string
	RayInstalled *bool
	SSHEnabled  *bool
}
