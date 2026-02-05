package ssh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/dave/clusterctl/internal/domain"
)

// Executor executes commands on remote machines via SSH
type Executor struct {
	user           string
	privateKeyPath string
	port           int
	timeout        time.Duration
	useTailscaleSSH bool

	// Connection pool
	connPool   map[string]*ssh.Client
	connPoolMu sync.RWMutex
}

// Config holds SSH executor configuration
type Config struct {
	User            string
	PrivateKeyPath  string
	Port            int
	Timeout         time.Duration
	UseTailscaleSSH bool
}

// NewExecutor creates a new SSH executor
func NewExecutor(cfg Config) *Executor {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &Executor{
		user:            cfg.User,
		privateKeyPath:  cfg.PrivateKeyPath,
		port:            cfg.Port,
		timeout:         cfg.Timeout,
		useTailscaleSSH: cfg.UseTailscaleSSH,
		connPool:        make(map[string]*ssh.Client),
	}
}

// Execute runs a command on a remote device
func (e *Executor) Execute(ctx context.Context, device *domain.Device, command string) (string, error) {
	if e.useTailscaleSSH {
		return e.executeTailscaleSSH(ctx, device, command)
	}
	return e.executeRegularSSH(ctx, device, command)
}

// executeTailscaleSSH uses the tailscale ssh command
func (e *Executor) executeTailscaleSSH(ctx context.Context, device *domain.Device, command string) (string, error) {
	// Build target: user@device or just device
	target := device.TailscaleIP
	if device.Name != "" {
		target = device.Name
	}
	if e.user != "" {
		target = e.user + "@" + target
	}

	// Use tailscale ssh command
	cmd := exec.CommandContext(ctx, "tailscale", "ssh", target, command)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("ssh error: %s", stderr.String())
		}
		return "", fmt.Errorf("ssh failed: %w", err)
	}

	return stdout.String(), nil
}

// executeRegularSSH uses standard SSH with key authentication
func (e *Executor) executeRegularSSH(ctx context.Context, device *domain.Device, command string) (string, error) {
	client, err := e.getClient(device)
	if err != nil {
		return "", err
	}

	session, err := client.NewSession()
	if err != nil {
		// Connection might be stale, try to reconnect
		e.closeClient(device.ID)
		client, err = e.getClient(device)
		if err != nil {
			return "", fmt.Errorf("failed to reconnect: %w", err)
		}
		session, err = client.NewSession()
		if err != nil {
			return "", fmt.Errorf("failed to create session: %w", err)
		}
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Run with context deadline
	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		session.Signal(ssh.SIGTERM)
		return "", ctx.Err()
	case err := <-done:
		if err != nil {
			if stderr.Len() > 0 {
				return "", fmt.Errorf("command error: %s", strings.TrimSpace(stderr.String()))
			}
			return "", err
		}
	}

	return stdout.String(), nil
}

// getClient gets or creates an SSH client for a device
func (e *Executor) getClient(device *domain.Device) (*ssh.Client, error) {
	e.connPoolMu.RLock()
	client, exists := e.connPool[device.ID]
	e.connPoolMu.RUnlock()

	if exists {
		return client, nil
	}

	// Create new connection
	config, err := e.getSSHConfig()
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("%s:%d", device.TailscaleIP, e.port)
	client, err = ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	e.connPoolMu.Lock()
	e.connPool[device.ID] = client
	e.connPoolMu.Unlock()

	return client, nil
}

// closeClient closes and removes a client from the pool
func (e *Executor) closeClient(deviceID string) {
	e.connPoolMu.Lock()
	defer e.connPoolMu.Unlock()

	if client, exists := e.connPool[deviceID]; exists {
		client.Close()
		delete(e.connPool, deviceID)
	}
}

// Close closes all SSH connections
func (e *Executor) Close() {
	e.connPoolMu.Lock()
	defer e.connPoolMu.Unlock()

	for _, client := range e.connPool {
		client.Close()
	}
	e.connPool = make(map[string]*ssh.Client)
}

// getSSHConfig creates SSH client configuration
func (e *Executor) getSSHConfig() (*ssh.ClientConfig, error) {
	keyPath := e.privateKeyPath
	if strings.HasPrefix(keyPath, "~") {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, keyPath[1:])
	}

	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return &ssh.ClientConfig{
		User: e.user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: proper host key verification
		Timeout:         e.timeout,
	}, nil
}

// CopyFile copies a file to a remote device
func (e *Executor) CopyFile(ctx context.Context, device *domain.Device, localPath, remotePath string) error {
	if e.useTailscaleSSH {
		return e.copyFileTailscaleSSH(ctx, device, localPath, remotePath)
	}
	return e.copyFileRegularSSH(ctx, device, localPath, remotePath)
}

func (e *Executor) copyFileTailscaleSSH(ctx context.Context, device *domain.Device, localPath, remotePath string) error {
	target := device.TailscaleIP
	if device.Name != "" {
		target = device.Name
	}
	if e.user != "" {
		target = e.user + "@" + target
	}

	// Use scp through tailscale
	cmd := exec.CommandContext(ctx, "tailscale", "scp", localPath, target+":"+remotePath)
	return cmd.Run()
}

func (e *Executor) copyFileRegularSSH(ctx context.Context, device *domain.Device, localPath, remotePath string) error {
	client, err := e.getClient(device)
	if err != nil {
		return err
	}

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	// Use SCP protocol
	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()

		fmt.Fprintf(w, "C0644 %d %s\n", stat.Size(), filepath.Base(remotePath))
		io.Copy(w, file)
		fmt.Fprint(w, "\x00")
	}()

	return session.Run(fmt.Sprintf("scp -t %s", remotePath))
}

// CheckConnectivity tests if SSH connection is possible
func (e *Executor) CheckConnectivity(ctx context.Context, device *domain.Device) error {
	_, err := e.Execute(ctx, device, "echo ok")
	return err
}

// ExecuteParallel executes a command on multiple devices in parallel
func (e *Executor) ExecuteParallel(ctx context.Context, devices []*domain.Device, command string, maxConcurrent int) map[string]ExecuteResult {
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}

	results := make(map[string]ExecuteResult)
	var resultsMu sync.Mutex

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for _, device := range devices {
		wg.Add(1)
		go func(d *domain.Device) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			output, err := e.Execute(ctx, d, command)

			resultsMu.Lock()
			results[d.ID] = ExecuteResult{
				Output: output,
				Error:  err,
			}
			resultsMu.Unlock()
		}(device)
	}

	wg.Wait()
	return results
}

// ExecuteResult holds the result of a command execution
type ExecuteResult struct {
	Output string
	Error  error
}
