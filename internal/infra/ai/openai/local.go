package openai

import "net/http"

// NewLocalProvider creates a Provider targeting a local OpenAI-compatible endpoint
// (e.g. Ollama: "http://gpu-server:11434/v1/chat/completions").
// No API key is required.
func NewLocalProvider(endpoint, model string) *Provider {
	if model == "" {
		model = defaultModel
	}
	return &Provider{
		endpoint: endpoint,
		model:    model,
		client:   &http.Client{},
	}
}
