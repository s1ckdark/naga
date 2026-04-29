package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func TestCategorizeSSHError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want DiagnosisCategory
	}{
		{"host key mismatch", errors.New("ssh: handshake failed: knownhosts: key mismatch"), DiagHostKeyMismatch},
		{"auth failed", errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey]"), DiagAuthFailed},
		{"connection refused", errors.New("dial tcp 100.1.2.3:22: connect: connection refused"), DiagNetworkUnreachable},
		{"i/o timeout", errors.New("dial tcp 100.1.2.3:22: i/o timeout"), DiagNetworkUnreachable},
		{"no route", errors.New("dial tcp 100.1.2.3:22: connect: no route to host"), DiagNetworkUnreachable},
		{"key file missing", errors.New("failed to read private key: open /tmp/x: no such file or directory"), DiagKeyFileMissing},
		{"key parse fail", errors.New("failed to parse private key: ssh: no key found"), DiagKeyFileMissing},
		{"unknown", errors.New("something went sideways"), DiagUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat, msg := categorizeSSHError(tc.err)
			if cat != tc.want {
				t.Fatalf("category: got %q want %q", cat, tc.want)
			}
			if msg != tc.err.Error() {
				t.Fatalf("message must echo err.Error(); got %q", msg)
			}
		})
	}
}

func newTestKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	pk, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("wrap pubkey: %v", err)
	}
	return pk
}

func setupKnownHosts(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	t.Setenv("CLUSTERCTL_SSH_KNOWN_HOSTS", path)
	return path
}

func TestRemoveKnownHostAtomic(t *testing.T) {
	const seed = "# header line\n" +
		"100.1.2.3 ssh-ed25519 AAAA\n" +
		"100.4.5.6 ssh-ed25519 BBBB\n"
	path := setupKnownHosts(t, seed)

	if err := RemoveKnownHost("100.1.2.3"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(got), "100.1.2.3") {
		t.Fatalf("removed host still present:\n%s", got)
	}
	if !strings.Contains(string(got), "100.4.5.6") {
		t.Fatalf("unrelated host was dropped:\n%s", got)
	}
	if !strings.Contains(string(got), "# header line") {
		t.Fatalf("comment header was dropped:\n%s", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("perm after rewrite: got %o want 0600", info.Mode().Perm())
	}
}

func TestRemoveKnownHostMissingFileNoError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLUSTERCTL_SSH_KNOWN_HOSTS", filepath.Join(dir, "known_hosts"))
	if err := RemoveKnownHost("100.1.2.3"); err != nil {
		t.Fatalf("remove on missing file should be ok: %v", err)
	}
}

func TestReplaceKnownHostReplacesAndAppendsAtomically(t *testing.T) {
	seed := "# header\n100.1.2.3 ssh-ed25519 OLDKEY\n100.4.5.6 ssh-ed25519 KEEP\n"
	path := setupKnownHosts(t, seed)
	key := newTestKey(t)

	if err := ReplaceKnownHost("100.1.2.3", key); err != nil {
		t.Fatalf("replace: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	out := string(got)
	if strings.Contains(out, "OLDKEY") {
		t.Fatalf("old key still in file:\n%s", out)
	}
	if !strings.Contains(out, "100.4.5.6") {
		t.Fatalf("unrelated host dropped:\n%s", out)
	}
	if !strings.Contains(out, "100.1.2.3") {
		t.Fatalf("replacement host missing:\n%s", out)
	}
	if !strings.Contains(out, "# header") {
		t.Fatalf("comment dropped:\n%s", out)
	}
}

func TestReplaceKnownHostRequiresKey(t *testing.T) {
	setupKnownHosts(t, "")
	if err := ReplaceKnownHost("100.1.2.3", nil); err == nil {
		t.Fatalf("expected error for nil key")
	}
}

func TestAppendKnownHostLineCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "known_hosts")
	t.Setenv("CLUSTERCTL_SSH_KNOWN_HOSTS", path)
	key := newTestKey(t)
	if err := AppendKnownHostLine("100.7.8.9", key); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "100.7.8.9") {
		t.Fatalf("appended host missing:\n%s", got)
	}
}

// TestKnownHostsConcurrent exercises the package mutex by interleaving
// Replace/Remove/Append from multiple goroutines. Run with -race.
func TestKnownHostsConcurrent(t *testing.T) {
	setupKnownHosts(t, "")
	key := newTestKey(t)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(3)
		go func(n int) {
			defer wg.Done()
			_ = AppendKnownHostLine("100.0.0."+itoa(n), key)
		}(i)
		go func(n int) {
			defer wg.Done()
			_ = ReplaceKnownHost("100.0.0."+itoa(n), key)
		}(i)
		go func(n int) {
			defer wg.Done()
			_ = RemoveKnownHost("100.0.0." + itoa(n))
		}(i)
	}
	wg.Wait()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
