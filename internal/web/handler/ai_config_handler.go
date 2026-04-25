package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/s1ckdark/hydra/config"
)

// providerConfigJSON is a JSON-bindable mirror of config.ProviderConfig.
// config.ProviderConfig uses mapstructure tags only, so we use this local type
// for HTTP request binding and then copy into the canonical type.
type providerConfigJSON struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
}

func (p providerConfigJSON) toConfig() config.ProviderConfig {
	return config.ProviderConfig{
		Provider: p.Provider,
		APIKey:   p.APIKey,
		Endpoint: p.Endpoint,
		Model:    p.Model,
	}
}

// AIConfigRequest is the payload for updating AI provider configuration.
//
// AlwaysConsult promotes the AI to the primary scheduler (every task
// goes through the AI provider, subject to per-tick budget) instead of a
// rule-based tiebreaker. Per-task `aiSchedule` overrides this default both
// ways. Defaults to false when omitted from the JSON body.
type AIConfigRequest struct {
	Default            providerConfigJSON  `json:"default"`
	HeadSelection      *providerConfigJSON `json:"head_selection,omitempty"`
	TaskScheduling     *providerConfigJSON `json:"task_scheduling,omitempty"`
	CapacityEstimation *providerConfigJSON `json:"capacity_estimation,omitempty"`
	AlwaysConsult      bool                `json:"always_consult"`
}

// APIGetAIConfig returns the current AI configuration with API keys masked.
func (h *Handler) APIGetAIConfig(c echo.Context) error {
	ai := h.cfg.Agent.AI
	return c.JSON(http.StatusOK, map[string]any{
		"default":             maskedProvider(ai.Default),
		"head_selection":      maskedProviderPtr(ai.HeadSelection),
		"task_scheduling":     maskedProviderPtr(ai.TaskScheduling),
		"capacity_estimation": maskedProviderPtr(ai.CapacityEstimation),
		"always_consult":      ai.AlwaysConsult,
	})
}

// APIPutAIConfig updates AI provider config and persists to disk.
func (h *Handler) APIPutAIConfig(c echo.Context) error {
	var req AIConfigRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if err := validateProvider(req.Default.toConfig(), "default"); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	type roleOverride struct {
		role string
		p    *providerConfigJSON
	}
	overrides := []roleOverride{
		{"head_selection", req.HeadSelection},
		{"task_scheduling", req.TaskScheduling},
		{"capacity_estimation", req.CapacityEstimation},
	}
	for _, o := range overrides {
		if o.p == nil || o.p.Provider == "" {
			continue
		}
		if err := validateProvider(o.p.toConfig(), o.role); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
	}

	newAI := config.AIConfig{
		Default:       req.Default.toConfig(),
		AlwaysConsult: req.AlwaysConsult,
	}
	if req.HeadSelection != nil {
		pc := req.HeadSelection.toConfig()
		newAI.HeadSelection = &pc
	}
	if req.TaskScheduling != nil {
		pc := req.TaskScheduling.toConfig()
		newAI.TaskScheduling = &pc
	}
	if req.CapacityEstimation != nil {
		pc := req.CapacityEstimation.toConfig()
		newAI.CapacityEstimation = &pc
	}

	h.cfg.Agent.AI = newAI

	if err := config.Save(h.cfg); err != nil {
		return internalError(c, "failed to save config", err)
	}

	// Propagate the always_consult flag to the running supervisor so flips
	// take effect without requiring a server restart. Per-task aiSchedule
	// continues to override.
	if h.taskSupervisor != nil {
		h.taskSupervisor.SetAlwaysConsultAI(newAI.AlwaysConsult)
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "updated"})
}

// maskedProvider returns a JSON-safe view of a ProviderConfig with the secret
// APIKey replaced by a boolean flag.
func maskedProvider(p config.ProviderConfig) map[string]any {
	return map[string]any{
		"provider":    p.Provider,
		"has_api_key": p.APIKey != "",
		"endpoint":    p.Endpoint,
		"model":       p.Model,
	}
}

func maskedProviderPtr(p *config.ProviderConfig) any {
	if p == nil {
		return nil
	}
	return maskedProvider(*p)
}

// validateProvider checks that a ProviderConfig has the required field for its
// auth mode (api_key for cloud providers, endpoint for local providers).
func validateProvider(p config.ProviderConfig, role string) error {
	switch p.Provider {
	case "":
		if role == "default" {
			return echoError("default provider is required")
		}
		return nil
	case "claude", "openai", "zai":
		if p.APIKey == "" {
			return echoError(role + ": api_key required for provider " + p.Provider)
		}
	case "ollama", "lmstudio", "openai_compatible":
		if p.Endpoint == "" {
			return echoError(role + ": endpoint required for provider " + p.Provider)
		}
	default:
		return echoError(role + ": unknown provider " + p.Provider)
	}
	return nil
}

type handlerError struct{ msg string }

func (e handlerError) Error() string { return e.msg }
func echoError(msg string) error     { return handlerError{msg: msg} }
