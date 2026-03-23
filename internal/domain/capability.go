package domain

// Standard capability constants
const (
	// Server capabilities
	CapCompute  = "compute"
	CapGPU      = "gpu"
	CapStorage  = "storage"
	CapSSH      = "ssh"
	CapRay      = "ray"

	// Mobile capabilities
	CapGPS       = "gps"
	CapCamera    = "camera"
	CapSMS       = "sms"
	CapPhone     = "phone"
	CapSensor    = "sensor"
	CapBluetooth = "bluetooth"
	CapNFC       = "nfc"
	CapPush      = "push"

	// Universal capabilities
	CapNetwork = "network"
	CapNotify  = "notify"
)

// DefaultServerCapabilities returns typical capabilities for a server node
func DefaultServerCapabilities(device *Device) []string {
	caps := []string{CapCompute, CapNetwork}
	if device.SSHEnabled {
		caps = append(caps, CapSSH)
	}
	if device.HasGPU {
		caps = append(caps, CapGPU)
	}
	if device.RayInstalled {
		caps = append(caps, CapRay)
	}
	return caps
}
