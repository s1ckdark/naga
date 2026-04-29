package ssh

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/s1ckdark/hydra/internal/domain"
)

// knownHostsMu serializes every read-modify-write to the known_hosts file so
// that the executor's auto-accept callback and the recovery package's
// Replace/Remove helpers can not interleave and produce a torn or partially
// rewritten file.
var knownHostsMu sync.Mutex

type DiagnosisCategory string

const (
	DiagOK                 DiagnosisCategory = "ok"
	DiagNetworkUnreachable DiagnosisCategory = "network_unreachable"
	DiagHostKeyMismatch    DiagnosisCategory = "host_key_mismatch"
	DiagAuthFailed         DiagnosisCategory = "auth_failed"
	DiagKeyFileMissing     DiagnosisCategory = "key_file_missing"
	DiagTailscale          DiagnosisCategory = "tailscale"
	DiagUnknown            DiagnosisCategory = "unknown"
)

type SSHDiagnosis struct {
	Category           DiagnosisCategory `json:"category"`
	Message            string            `json:"message"`
	Hostname           string            `json:"hostname,omitempty"`
	HostKeyFingerprint string            `json:"hostKeyFingerprint,omitempty"`
}

func knownHostsPath() string {
	if p := os.Getenv("CLUSTERCTL_SSH_KNOWN_HOSTS"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "known_hosts")
}

func fingerprintSHA256(k gossh.PublicKey) string {
	sum := sha256.Sum256(k.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// RemoveKnownHost strips every known_hosts line whose first field matches
// hostname. The rewrite is serialized via knownHostsMu and goes through a
// temp file + rename so concurrent dials and acceptances cannot observe a
// partially-written file.
func RemoveKnownHost(hostname string) error {
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()
	return rewriteKnownHostsLocked(hostname, nil)
}

// ReplaceKnownHost atomically replaces every entry for hostname in the
// known_hosts file with a single new entry for key. Used after the user has
// confirmed a host-key change so removal+append happen as one operation.
func ReplaceKnownHost(hostname string, key gossh.PublicKey) error {
	if key == nil {
		return errors.New("ReplaceKnownHost: key is required")
	}
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()
	return rewriteKnownHostsLocked(hostname, key)
}

// AppendKnownHostLine adds a new known_hosts entry under knownHostsMu. The
// executor's auto-accept callback uses this so its append cannot race with a
// ReplaceKnownHost rewrite.
func AppendKnownHostLine(hostname string, key gossh.PublicKey) error {
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()
	path := knownHostsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	_, err = fmt.Fprintln(f, line)
	return err
}

// rewriteKnownHostsLocked must be called with knownHostsMu held. It reads the
// current known_hosts file, drops every entry matching hostname, optionally
// appends a replacement line, and atomically renames the result over the
// original file.
func rewriteKnownHostsLocked(hostname string, replacement gossh.PublicKey) error {
	path := knownHostsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// File doesn't exist. A pure removal has nothing to do; a replacement
		// still needs to create the file with the new entry.
		if replacement == nil {
			return nil
		}
	}

	target := knownhosts.Normalize(hostname)
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			kept = append(kept, line)
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			kept = append(kept, line)
			continue
		}
		match := false
		for _, h := range strings.Split(fields[0], ",") {
			if knownhosts.Normalize(h) == target {
				match = true
				break
			}
		}
		if !match {
			kept = append(kept, line)
		}
	}
	if replacement != nil {
		kept = append(kept, knownhosts.Line([]string{target}, replacement))
	}

	return atomicWriteKnownHosts(path, []byte(strings.Join(kept, "\n")))
}

// atomicWriteKnownHosts writes contents to a temp file in the same directory,
// fsyncs it, chmods it 0600, then renames it over path. On any error the temp
// file is removed.
func atomicWriteKnownHosts(path string, contents []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".known_hosts.tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(contents); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpPath, 0600); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	// fsync the parent directory so the rename itself is durable across a
	// power loss. Without this the rename could be replayed in a way that
	// leaves the new file but loses the directory entry.
	if dirF, err := os.Open(dir); err == nil {
		_ = dirF.Sync()
		dirF.Close()
	}
	return nil
}

// errProbeAbort is the sentinel returned from probeHostKey's host-key
// callback to short-circuit the handshake after capturing the server's key.
// It is package-level so tests and the verification path below can reference
// the exact string.
var errProbeAbort = errors.New("probe-abort")

// probeHostKey opens a TCP connection and captures the server's host key
// without verifying it against known_hosts. The handshake is intentionally
// aborted via errProbeAbort; we require the resulting NewClientConn error to
// contain that sentinel before trusting the captured key, so an unrelated
// handshake failure can not be misread as a successful capture.
func (e *Executor) probeHostKey(ctx context.Context, addr string) (gossh.PublicKey, error) {
	var captured gossh.PublicKey
	cfg := &gossh.ClientConfig{
		User: e.user,
		HostKeyCallback: func(_ string, _ net.Addr, key gossh.PublicKey) error {
			captured = key
			return errProbeAbort
		},
		Timeout: 5 * time.Second,
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	// x/crypto/ssh wraps callback errors with %v (not %w), so errors.Is/As
	// won't unwrap through the handshake error. Substring match on the
	// sentinel text is the documented workaround.
	_, _, _, hsErr := gossh.NewClientConn(conn, addr, cfg)
	if captured == nil {
		if hsErr != nil {
			return nil, fmt.Errorf("capture host key: %w", hsErr)
		}
		return nil, errors.New("failed to capture host key")
	}
	if hsErr == nil || !strings.Contains(hsErr.Error(), errProbeAbort.Error()) {
		return nil, fmt.Errorf("captured key but handshake did not abort via sentinel: %v", hsErr)
	}
	return captured, nil
}

// Diagnose returns a structured explanation of why SSH fails for device.
func (e *Executor) Diagnose(ctx context.Context, device *domain.Device) (*SSHDiagnosis, error) {
	if e.useTailscaleSSH {
		msg := "regular SSH recovery does not apply to tailscale SSH"
		if !e.checkTailscaleAuth() {
			msg = "tailscale is not authenticated — run 'tailscale login'"
		}
		return &SSHDiagnosis{Category: DiagTailscale, Message: msg, Hostname: device.TailscaleIP}, nil
	}

	addr := fmt.Sprintf("%s:%d", device.TailscaleIP, e.port)

	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	cancel()
	if err != nil {
		return &SSHDiagnosis{
			Category: DiagNetworkUnreachable,
			Message:  err.Error(),
			Hostname: device.TailscaleIP,
		}, nil
	}
	conn.Close()

	// Probe the SSH handshake using a throwaway client so we don't disturb
	// in-flight executes, metric polls, or task supervisor sessions that may
	// be holding the pooled client for this device.
	connErr := e.probeSSHHandshake(ctx, device)
	if connErr == nil {
		return &SSHDiagnosis{Category: DiagOK, Hostname: device.TailscaleIP}, nil
	}

	cat, msg := categorizeSSHError(connErr)
	diag := &SSHDiagnosis{Category: cat, Message: msg, Hostname: device.TailscaleIP}

	if cat == DiagHostKeyMismatch {
		if key, perr := e.probeHostKey(ctx, addr); perr == nil {
			diag.HostKeyFingerprint = fingerprintSHA256(key)
		}
	}
	return diag, nil
}

// probeSSHHandshake performs a one-shot SSH handshake using the executor's
// regular auth + host-key callback, then closes the result. It does not
// touch the connection pool; this is what makes Diagnose safe to call while
// other goroutines are using the pooled client.
func (e *Executor) probeSSHHandshake(ctx context.Context, device *domain.Device) error {
	cfg, err := e.getSSHConfig()
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", device.TailscaleIP, e.port)
	dialCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return err
	}
	sshConn, chans, reqs, err := gossh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return err
	}
	client := gossh.NewClient(sshConn, chans, reqs)
	client.Close()
	return nil
}

// AcceptHostKey re-probes the server, verifies its key matches expectedFingerprint,
// and then replaces the known_hosts entry for device.
func (e *Executor) AcceptHostKey(ctx context.Context, device *domain.Device, expectedFingerprint string) error {
	if e.useTailscaleSSH {
		return errors.New("host key acceptance is not supported on tailscale SSH")
	}
	addr := fmt.Sprintf("%s:%d", device.TailscaleIP, e.port)
	hostname := device.TailscaleIP

	key, err := e.probeHostKey(ctx, addr)
	if err != nil {
		return fmt.Errorf("probe server key: %w", err)
	}
	actual := fingerprintSHA256(key)
	if actual != expectedFingerprint {
		return fmt.Errorf("host key changed mid-confirmation: got %s, expected %s", actual, expectedFingerprint)
	}

	if err := ReplaceKnownHost(hostname, key); err != nil {
		return fmt.Errorf("update known_hosts: %w", err)
	}

	// Force the next getClient to re-handshake against the new host key.
	e.closeClient(device.ID)
	return nil
}

// categorizeSSHError maps a raw SSH dial/handshake error to a DiagnosisCategory
// the iOS client branches on. The mapping uses strings.Contains because the
// x/crypto/ssh handshake error wraps with %v (not %w), so errors.Is/As cannot
// unwrap through it.
func categorizeSSHError(err error) (DiagnosisCategory, string) {
	msg := err.Error()
	s := strings.ToLower(msg)
	switch {
	case strings.Contains(s, "key mismatch"):
		return DiagHostKeyMismatch, msg
	case strings.Contains(s, "unable to authenticate"):
		return DiagAuthFailed, msg
	case strings.Contains(s, "connection refused"),
		strings.Contains(s, "i/o timeout"),
		strings.Contains(s, "no route to host"):
		return DiagNetworkUnreachable, msg
	case strings.Contains(s, "failed to read private key"),
		strings.Contains(s, "failed to parse private key"),
		strings.Contains(s, "no such file or directory"):
		return DiagKeyFileMissing, msg
	default:
		return DiagUnknown, msg
	}
}
