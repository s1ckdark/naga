package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// tailscaleCIDR is the Tailscale CGNAT range
var tailscaleCIDR = func() *net.IPNet {
	_, cidr, _ := net.ParseCIDR("100.64.0.0/10")
	return cidr
}()

// APIKeyStore provides API key lookup
type APIKeyStore interface {
	ValidateAPIKey(keyHash string) (deviceID string, ok bool)
}

// AuthConfig configures the authentication middleware
type AuthConfig struct {
	KeyStore       APIKeyStore
	AllowTailscale bool
}

// HashAPIKey hashes an API key for storage
func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// Auth middleware allows Tailscale network or API key authentication
func Auth(cfg AuthConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Check Tailscale network
			if cfg.AllowTailscale && isTailscaleOrLocal(c.Request().RemoteAddr) {
				return next(c)
			}

			// Check API key
			key := extractAPIKey(c.Request())
			if key == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "authentication required: Tailscale network or API key",
				})
			}

			keyHash := HashAPIKey(key)
			deviceID, ok := cfg.KeyStore.ValidateAPIKey(keyHash)
			if !ok {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "invalid API key",
				})
			}

			// Store device ID in context
			c.Set("deviceID", deviceID)
			c.Set("authMethod", "apikey")
			return next(c)
		}
	}
}

func extractAPIKey(r *http.Request) string {
	// Check Authorization header: "Bearer <key>"
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Check X-API-Key header
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}
	return ""
}

func isTailscaleOrLocal(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	return tailscaleCIDR.Contains(ip)
}

// ConstantTimeCompare does constant-time comparison of two strings
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
