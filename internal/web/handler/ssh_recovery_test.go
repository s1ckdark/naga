package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestValidSHA256Fingerprint(t *testing.T) {
	cases := []struct {
		fp   string
		want bool
	}{
		{"SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU", true},   // 43 chars, unpadded
		{"SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=", true},  // 44 chars, padded
		{"SHA256:abcDEF0123456789", false},                              // too short
		{"SHA256:" + strings.Repeat("A", 42), false},                    // off-by-one short
		{"SHA256:" + strings.Repeat("A", 45), false},                    // off-by-one long
		{"sha256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU", false},
		{"SHA256:", false},
		{"SHA256:has spaces in payload here xxxxxxxxxxxxxx", false},     // 44 chars but with space
		{"MD5:47DEQpj8HBSa+/TImW+5JC", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.fp, func(t *testing.T) {
			if got := validSHA256Fingerprint(tc.fp); got != tc.want {
				t.Fatalf("validSHA256Fingerprint(%q) = %v, want %v", tc.fp, got, tc.want)
			}
		})
	}
}

// validFingerprint covers the well-formed fingerprint from base64-encoded
// SHA256(empty input), used as a stand-in payload in the route tests below.
const validFingerprint = "SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU"

func newTestEcho(h *Handler) *echo.Echo {
	e := echo.New()
	e.POST("/api/devices/:id/ssh/diagnose", h.APISSHDiagnose)
	e.POST("/api/devices/:id/ssh/accept-key", h.APISSHAcceptHostKey)
	return e
}

func decodeError(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode error envelope: %v (body=%s)", err, body)
	}
	return env.Error
}

func TestSSHRoutes_ServiceUnavailable_WhenDepsMissing(t *testing.T) {
	e := newTestEcho(&Handler{})

	for _, path := range []string{
		"/api/devices/dev-1/ssh/diagnose",
		"/api/devices/dev-1/ssh/accept-key",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"fingerprint":"`+validFingerprint+`"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status got %d want 503 (body=%s)", path, rec.Code, rec.Body.String())
		}
		if msg := decodeError(t, rec.Body.Bytes()); msg == "" {
			t.Fatalf("%s: empty error envelope", path)
		}
	}
}

func TestSSHAcceptKey_BadInputReturns400(t *testing.T) {
	e := newTestEcho(&Handler{})

	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{`},
		{"missing fingerprint", `{}`},
		{"non-sha256 prefix", `{"fingerprint":"MD5:abc"}`},
		{"sha256 too short", `{"fingerprint":"SHA256:abc"}`},
		{"illegal char", `{"fingerprint":"SHA256:has spaces in payload here"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost,
				"/api/devices/dev-1/ssh/accept-key",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status got %d want 400 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestClassifyAcceptHostKeyError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"mid-confirmation change", errors.New("host key changed mid-confirmation: got X, expected Y"), http.StatusConflict},
		{"probe wrap", errors.New("probe server key: dial tcp: connection refused"), http.StatusBadGateway},
		{"raw network", errors.New("dial tcp: i/o timeout"), http.StatusBadGateway},
		{"no route", errors.New("dial tcp: no route to host"), http.StatusBadGateway},
		{"tailscale", errors.New("host key acceptance is not supported on tailscale SSH"), http.StatusBadRequest},
		{"known_hosts write", errors.New("update known_hosts: permission denied"), http.StatusInternalServerError},
		{"unknown", errors.New("something unexpected"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, msg := classifyAcceptHostKeyError(tc.err)
			if code != tc.wantCode {
				t.Fatalf("status: got %d, want %d", code, tc.wantCode)
			}
			if msg == "" || strings.Contains(msg, "/") {
				t.Fatalf("sanitized message must be non-empty and free of paths, got %q", msg)
			}
		})
	}
}
