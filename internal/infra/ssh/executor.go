package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/s1ckdark/hydra/internal/domain"
)

// Executor executes commands on remote machines via SSH
type Executor struct {
	user            string
	privateKeyPath  string
	port            int
	timeout         time.Duration
	useTailscaleSSH bool

	// Connection pool
	connPool   map[string]*ssh.Client
	connPoolMu sync.RWMutex

	// Tailscale auth state (checked once at startup)
	tailscaleAuthed   bool
	tailscaleCheckMu  sync.Once
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

// checkTailscaleAuth checks if Tailscale is authenticated (runs once).
func (e *Executor) checkTailscaleAuth() bool {
	e.tailscaleCheckMu.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, findTailscaleBinary(), "status", "--json")
		out, err := cmd.Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[hydra] tailscale status check failed: %v\n", err)
			return
		}
		// If BackendState is "Running", we're authenticated
		if bytes.Contains(out, []byte(`"BackendState":"Running"`)) {
			e.tailscaleAuthed = true
		} else {
			fmt.Fprintln(os.Stderr, "[hydra] tailscale is not authenticated — skipping tailscale ssh. Run 'tailscale login' to authenticate.")
		}
	})
	return e.tailscaleAuthed
}

// findTailscaleBinary returns the path to the tailscale binary
func findTailscaleBinary() string {
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path
	}
	// macOS app bundle location
	macOSPath := "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
	if _, err := os.Stat(macOSPath); err == nil {
		return macOSPath
	}
	return "tailscale"
}

// executeTailscaleSSH uses the tailscale ssh command
func (e *Executor) executeTailscaleSSH(ctx context.Context, device *domain.Device, command string) (string, error) {
	if !e.checkTailscaleAuth() {
		return "", fmt.Errorf("tailscale not authenticated — run 'tailscale login' first")
	}

	// Build target: user@device or just device
	target := device.TailscaleIP
	if device.Name != "" {
		target = device.Name
	}
	if e.user != "" {
		target = e.user + "@" + target
	}

	// Use tailscale ssh command
	cmd := exec.CommandContext(ctx, findTailscaleBinary(), "ssh", target, command)

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

	hostKeyCallback, err := e.getHostKeyCallback()
	if err != nil {
		return nil, err
	}

	return &ssh.ClientConfig{
		User: e.user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         e.timeout,
	}, nil
}

func (e *Executor) getHostKeyCallback() (ssh.HostKeyCallback, error) {
	home, _ := os.UserHomeDir()
	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")

	if file := os.Getenv("CLUSTERCTL_SSH_KNOWN_HOSTS"); file != "" {
		knownHostsPath = file
	}

	// Ensure known_hosts file exists
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		dir := filepath.Dir(knownHostsPath)
		os.MkdirAll(dir, 0700)
		os.WriteFile(knownHostsPath, nil, 0600)
	}

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize SSH host key verification: %w", err)
	}

	// Wrap callback to auto-accept and save unknown host keys (like StrictHostKeyChecking=accept-new)
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := callback(hostname, remote, key)
		if err == nil {
			return nil
		}

		// If it's a key-not-found error, auto-accept and save. We route the
		// append through AppendKnownHostLine so it shares knownHostsMu with
		// recovery's ReplaceKnownHost/RemoveKnownHost and can not interleave
		// with their read-modify-write rewrites.
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			if appendErr := AppendKnownHostLine(hostname, key); appendErr != nil {
				return fmt.Errorf("unknown host key and failed to save: %w", appendErr)
			}
			return nil
		}

		// Key mismatch (possible MITM) — reject
		return err
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
	if !e.checkTailscaleAuth() {
		return fmt.Errorf("tailscale not authenticated — run 'tailscale login' first")
	}

	target := device.TailscaleIP
	if device.Name != "" {
		target = device.Name
	}
	if e.user != "" {
		target = e.user + "@" + target
	}

	// Use scp through tailscale
	cmd := exec.CommandContext(ctx, findTailscaleBinary(), "scp", localPath, target+":"+remotePath)
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
