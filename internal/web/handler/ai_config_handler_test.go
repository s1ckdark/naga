package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/s1ckdark/hydra/config"
)

func newAIHandler(t *testing.T) (*Handler, *config.Config) {
	t.Helper()
	cfg := config.DefaultConfig()
	// Avoid Save writing to real home dir during tests
	t.Setenv("NAGA_CONFIG_DIR", t.TempDir())
	cfg.Tailscale.APIKey = "tskey-test"
	h := &Handler{cfg: cfg}
	return h, cfg
}

func TestAPIGetAIConfig_MasksSecrets(t *testing.T) {
	h, cfg := newAIHandler(t)
	cfg.Agent.AI = config.AIConfig{
		Default: config.ProviderConfig{Provider: "claude", APIKey: "sk-secret", Model: "claude-sonnet-4-6"},
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/config/ai", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := h.APIGetAIConfig(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	body := rec.Body.String()
	if strings.Contains(body, "sk-secret") {
		t.Errorf("response leaked secret api key: %s", body)
	}
	if !strings.Contains(body, `"has_api_key":true`) {
		t.Errorf("response missing has_api_key:true: %s", body)
	}
}

func TestAPIPutAIConfig_RejectsProviderlessDefault(t *testing.T) {
	h, _ := newAIHandler(t)
	e := echo.New()
	body := `{"default": {"provider": ""}}`
	req := httptest.NewRequest(http.MethodPut, "/api/config/ai", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := h.APIPutAIConfig(ctx); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
}

func TestAPIPutAIConfig_AcceptsValidClaude(t *testing.T) {
	h, cfg := newAIHandler(t)
	e := echo.New()
	payload := map[string]any{
		"default": map[string]any{
			"provider": "claude",
			"api_key":  "sk-new",
			"model":    "claude-sonnet-4-6",
		},
	}
	data, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/config/ai", bytes.NewReader(data))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := h.APIPutAIConfig(ctx); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200; body=%s", rec.Code, rec.Body.String())
	}
	if cfg.Agent.AI.Default.APIKey != "sk-new" {
		t.Errorf("cfg not updated: %+v", cfg.Agent.AI.Default)
	}
}

func TestAPIPutAIConfig_RequiresAPIKeyForCloudProvider(t *testing.T) {
	h, _ := newAIHandler(t)
	e := echo.New()
	body := `{"default": {"provider": "openai", "api_key": ""}}`
	req := httptest.NewRequest(http.MethodPut, "/api/config/ai", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := h.APIPutAIConfig(ctx); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 for cloud provider without api_key", rec.Code)
	}
}

func TestAPIPutAIConfig_RequiresEndpointForLocalProvider(t *testing.T) {
	h, _ := newAIHandler(t)
	e := echo.New()
	body := `{"default": {"provider": "ollama", "endpoint": ""}}`
	req := httptest.NewRequest(http.MethodPut, "/api/config/ai", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := h.APIPutAIConfig(ctx); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 for local provider without endpoint", rec.Code)
	}
}

func TestAPIPutAIConfig_OmittedAlwaysConsultPreservesPrevious(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tailscale.APIKey = "tskey-test"
	cfg.Agent.AI = config.AIConfig{
		Default:       config.ProviderConfig{Provider: "claude", APIKey: "sk-x"},
		AlwaysConsult: true, // user previously enabled
	}
	t.Setenv("NAGA_CONFIG_DIR", t.TempDir())
	h := &Handler{cfg: cfg}

	e := echo.New()
	body := `{"default":{"provider":"claude","api_key":"sk-y"}}` // no always_consult
	req := httptest.NewRequest(http.MethodPut, "/api/config/ai", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.APIPutAIConfig(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !cfg.Agent.AI.AlwaysConsult {
		t.Errorf("AlwaysConsult got flipped to false; should be preserved when omitted")
	}
}

func TestAPIPutAIConfig_ExplicitAlwaysConsultApplies(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tailscale.APIKey = "tskey-test"
	cfg.Agent.AI = config.AIConfig{
		Default:       config.ProviderConfig{Provider: "claude", APIKey: "sk-x"},
		AlwaysConsult: false,
	}
	t.Setenv("NAGA_CONFIG_DIR", t.TempDir())
	h := &Handler{cfg: cfg}

	e := echo.New()
	body := `{"default":{"provider":"claude","api_key":"sk-y"},"always_consult":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/config/ai", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.APIPutAIConfig(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !cfg.Agent.AI.AlwaysConsult {
		t.Errorf("AlwaysConsult should be true after explicit set")
	}
}

func TestAPIPutAIConfig_InvokesArbiterRebuilder(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tailscale.APIKey = "tskey-test"
	t.Setenv("NAGA_CONFIG_DIR", t.TempDir())

	var rebuiltWith *config.AIConfig
	h := &Handler{cfg: cfg}
	h.SetAIArbiterRebuilder(func(newAI config.AIConfig) {
		c := newAI
		rebuiltWith = &c
	})

	e := echo.New()
	body := `{"default":{"provider":"claude","api_key":"sk-new","model":"claude-sonnet-4-6"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/config/ai", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.APIPutAIConfig(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rebuiltWith == nil {
		t.Fatal("aiArbiterRebuilder was not invoked")
	}
	if rebuiltWith.Default.Provider != "claude" || rebuiltWith.Default.APIKey != "sk-new" {
		t.Errorf("rebuilder received wrong config: %+v", rebuiltWith)
	}
}
