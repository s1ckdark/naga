package main

import (
	"log"

	"github.com/s1ckdark/hydra/config"
	"github.com/s1ckdark/hydra/internal/infra/ai"
	"github.com/s1ckdark/hydra/internal/infra/ai/claude"
	"github.com/s1ckdark/hydra/internal/infra/ai/lmstudio"
	"github.com/s1ckdark/hydra/internal/infra/ai/ollama"
	"github.com/s1ckdark/hydra/internal/infra/ai/openai"
)

// buildAIRegistry wires role-specific providers from the resolved AIConfig.
// Unsupported (provider,role) combinations fall through to the Registry's
// rule-based fallback.
func buildAIRegistry(aicfg config.AIConfig) *ai.Registry {
	reg := ai.NewRegistry(ai.Config{})
	if hs := buildHeadSelector(aicfg.Resolve("head")); hs != nil {
		reg.SetHeadSelector(hs)
	}
	if ts := buildTaskScheduler(aicfg.Resolve("schedule")); ts != nil {
		reg.SetTaskScheduler(ts)
	}
	// CapacityEstimator: no concrete provider implements it yet; left nil by design.
	return reg
}

// buildTaskScheduler returns an ai.TaskScheduler for the given provider config,
// or nil when credentials/endpoint are missing or the provider is unknown.
func buildTaskScheduler(p config.ProviderConfig) ai.TaskScheduler {
	switch p.Provider {
	case "":
		return nil
	case "claude":
		if p.APIKey == "" {
			log.Println("[ai] claude task-scheduler: empty api_key; disabled")
			return nil
		}
		return claude.NewProvider(p.APIKey, p.Model)
	case "openai":
		if p.APIKey == "" {
			log.Println("[ai] openai task-scheduler: empty api_key; disabled")
			return nil
		}
		return openai.NewProvider(p.APIKey, p.Model)
	case "ollama":
		if p.Endpoint == "" {
			log.Println("[ai] ollama task-scheduler: empty endpoint; disabled")
			return nil
		}
		return ollama.NewProvider(p.Endpoint, p.Model)
	case "lmstudio":
		if p.Endpoint == "" {
			log.Println("[ai] lmstudio task-scheduler: empty endpoint; disabled")
			return nil
		}
		return lmstudio.NewProvider(p.Endpoint, p.Model)
	default:
		log.Printf("[ai] unknown provider %q for task scheduler; disabled", p.Provider)
		return nil
	}
}

// buildHeadSelector returns an ai.HeadSelector for the given provider config,
// or nil when the provider does not support head selection.
func buildHeadSelector(p config.ProviderConfig) ai.HeadSelector {
	if p.Provider != "claude" {
		return nil
	}
	if p.APIKey == "" {
		return nil
	}
	return claude.NewProvider(p.APIKey, p.Model)
}
