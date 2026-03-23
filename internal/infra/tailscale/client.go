package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dave/naga/internal/domain"
)

// Client is a Tailscale API client
type Client struct {
	apiKey  string
	tailnet string
	baseURL string
	http    *http.Client
}

// NewClient creates a new Tailscale API client
func NewClient(apiKey, tailnet string) *Client {
	return &Client{
		apiKey:  apiKey,
		tailnet: tailnet,
		baseURL: "https://api.tailscale.com",
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientWithBaseURL creates a new Tailscale API client with custom base URL
func NewClientWithBaseURL(apiKey, tailnet, baseURL string) *Client {
	c := NewClient(apiKey, tailnet)
	c.baseURL = baseURL
	return c
}

// API response types

type apiDevice struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Hostname        string    `json:"hostname"`
	User            string    `json:"user"`
	OS              string    `json:"os"`
	Addresses       []string  `json:"addresses"`
	ClientVersion   string    `json:"clientVersion"`
	Authorized      bool      `json:"authorized"`
	KeyExpiryDisabled bool    `json:"keyExpiryDisabled"`
	BlocksIncomingConnections bool `json:"blocksIncomingConnections"`
	Tags            []string  `json:"tags"`
	Created         time.Time `json:"created"`
	LastSeen        time.Time `json:"lastSeen"`
	IsExternal      bool      `json:"isExternal"`
}

type devicesResponse struct {
	Devices []apiDevice `json:"devices"`
}

// ListDevices returns all devices in the tailnet
func (c *Client) ListDevices(ctx context.Context) ([]*domain.Device, error) {
	// If tailnet is not set, try to use "-" for the default tailnet
	tailnet := c.tailnet
	if tailnet == "" {
		tailnet = "-"
	}

	url := fmt.Sprintf("%s/api/v2/tailnet/%s/devices", c.baseURL, tailnet)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result devicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	devices := make([]*domain.Device, 0, len(result.Devices))
	for _, d := range result.Devices {
		devices = append(devices, convertDevice(&d))
	}

	return devices, nil
}

// GetDevice returns a specific device by name or ID
func (c *Client) GetDevice(ctx context.Context, nameOrID string) (*domain.Device, error) {
	devices, err := c.ListDevices(ctx)
	if err != nil {
		return nil, err
	}

	// Try to find by ID, name, or hostname
	for _, d := range devices {
		if d.ID == nameOrID || d.Name == nameOrID || d.Hostname == nameOrID {
			return d, nil
		}
		// Also try partial match on name
		if strings.Contains(strings.ToLower(d.Name), strings.ToLower(nameOrID)) {
			return d, nil
		}
	}

	return nil, fmt.Errorf("device not found: %s", nameOrID)
}

// GetDeviceByID returns a specific device by its ID
func (c *Client) GetDeviceByID(ctx context.Context, id string) (*domain.Device, error) {
	url := fmt.Sprintf("%s/api/v2/device/%s", c.baseURL, id)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result apiDevice
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return convertDevice(&result), nil
}

// convertDevice converts an API device to a domain device
func convertDevice(d *apiDevice) *domain.Device {
	status := domain.DeviceStatusOffline

	// Check if device is online (last seen within 5 minutes)
	if time.Since(d.LastSeen) < 5*time.Minute {
		status = domain.DeviceStatusOnline
	}

	// Extract Tailscale IP (first IPv4 in the 100.x.x.x range)
	var tailscaleIP string
	for _, ip := range d.Addresses {
		if strings.HasPrefix(ip, "100.") {
			tailscaleIP = ip
			break
		}
	}

	// Clean up hostname (remove tailnet suffix)
	hostname := d.Hostname
	if idx := strings.Index(hostname, "."); idx > 0 {
		// Keep the full hostname for clarity
	}

	return &domain.Device{
		ID:          d.ID,
		Name:        d.Name,
		Hostname:    hostname,
		IPAddresses: d.Addresses,
		TailscaleIP: tailscaleIP,
		OS:          normalizeOS(d.OS),
		Status:      status,
		IsExternal:  d.IsExternal,
		Tags:        d.Tags,
		User:        d.User,
		LastSeen:    d.LastSeen,
		CreatedAt:   d.Created,
		SSHEnabled:  !d.BlocksIncomingConnections,
	}
}

// normalizeOS normalizes the OS string for display
func normalizeOS(os string) string {
	os = strings.ToLower(os)

	switch {
	case strings.Contains(os, "macos") || strings.Contains(os, "darwin"):
		return "macOS"
	case strings.Contains(os, "linux"):
		return "Linux"
	case strings.Contains(os, "windows"):
		return "Windows"
	case strings.Contains(os, "ios"):
		return "iOS"
	case strings.Contains(os, "android"):
		return "Android"
	default:
		return os
	}
}

// Tailnet returns the current tailnet
func (c *Client) Tailnet() string {
	return c.tailnet
}

// SetTailnet sets the tailnet
func (c *Client) SetTailnet(tailnet string) {
	c.tailnet = tailnet
}
