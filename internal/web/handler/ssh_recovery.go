package handler

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/s1ckdark/hydra/internal/domain"
	sshpkg "github.com/s1ckdark/hydra/internal/infra/ssh"
)

// SSHRecoverer performs SSH-level diagnosis and host-key acceptance.
type SSHRecoverer interface {
	Diagnose(ctx context.Context, device *domain.Device) (*sshpkg.SSHDiagnosis, error)
	AcceptHostKey(ctx context.Context, device *domain.Device, expectedFingerprint string) error
}

// SetSSHRecoverer wires an SSH recovery-capable executor into the handler.
func (h *Handler) SetSSHRecoverer(r SSHRecoverer) {
	h.sshRecoverer = r
}

// APISSHDiagnose returns a structured diagnosis for a device's SSH connectivity.
func (h *Handler) APISSHDiagnose(c echo.Context) error {
	id := c.Param("id")
	if h.deviceUC == nil || h.sshRecoverer == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "service not available"})
	}
	device, err := h.deviceUC.GetDevice(c.Request().Context(), id)
	if err != nil || device == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "device not found"})
	}
	diag, err := h.sshRecoverer.Diagnose(c.Request().Context(), device)
	if err != nil {
		return internalError(c, "ssh diagnose failed", err)
	}
	return c.JSON(http.StatusOK, diag)
}

// APISSHAcceptHostKey replaces the known_hosts entry for a device after the
// user has confirmed the presented fingerprint.
func (h *Handler) APISSHAcceptHostKey(c echo.Context) error {
	id := c.Param("id")
	var req struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if !validSHA256Fingerprint(req.Fingerprint) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "fingerprint must be SHA256:<base64>"})
	}
	if h.deviceUC == nil || h.sshRecoverer == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "service not available"})
	}
	device, err := h.deviceUC.GetDevice(c.Request().Context(), id)
	if err != nil || device == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "device not found"})
	}
	if err := h.sshRecoverer.AcceptHostKey(c.Request().Context(), device, req.Fingerprint); err != nil {
		log.Printf("ssh accept host key failed for %s: %v", id, err)
		status, msg := classifyAcceptHostKeyError(err)
		return c.JSON(status, map[string]string{"error": msg})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// validSHA256Fingerprint enforces the SHA256:<base64> shape produced by
// fingerprintSHA256 in the ssh package. The base64 of a 32-byte digest is
// exactly 43 chars unpadded or 44 with `=` padding, so anything else is
// either malformed input or a different hash family. The backend still
// re-probes the server before accepting, so this is a hygiene check, not a
// security boundary.
func validSHA256Fingerprint(fp string) bool {
	const prefix = "SHA256:"
	if !strings.HasPrefix(fp, prefix) {
		return false
	}
	payload := fp[len(prefix):]
	if len(payload) != 43 && len(payload) != 44 {
		return false
	}
	for _, r := range payload {
		isAlnum := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !isAlnum && r != '+' && r != '/' && r != '=' {
			return false
		}
	}
	return true
}

// classifyAcceptHostKeyError maps backend errors from AcceptHostKey to an
// HTTP status the client can act on, plus a sanitized message that does not
// leak filesystem paths or other server internals.
func classifyAcceptHostKeyError(err error) (int, string) {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "host key changed mid-confirmation"):
		return http.StatusConflict, "host key changed during confirmation; restart and verify the new fingerprint"
	case strings.Contains(s, "probe server key"),
		strings.Contains(s, "connection refused"),
		strings.Contains(s, "i/o timeout"),
		strings.Contains(s, "no route to host"):
		return http.StatusBadGateway, "could not reach the device to re-probe its host key"
	case strings.Contains(s, "not supported on tailscale"):
		return http.StatusBadRequest, "host key acceptance is not supported on tailscale SSH"
	case strings.Contains(s, "update known_hosts"),
		strings.Contains(s, "permission denied"):
		return http.StatusInternalServerError, "failed to update known_hosts on the server"
	default:
		return http.StatusInternalServerError, "ssh recovery failed"
	}
}
