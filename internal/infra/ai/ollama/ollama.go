package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dave/naga/internal/domain"
	"github.com/dave/naga/internal/infra/ai"
)

const (
	defaultModel    = "gpt-oss:20b"
	defaultEndpoint = "http://localhost:11434"
)

// Provider implements ai.HeadSelector, ai.TaskScheduler, and ai.CapacityEstimator
// using the Ollama native API (/api/chat).
type Provider struct {
	endpoint string
	model    string
	client   *http.Client
}

// NewProvider creates an Ollama provider. No API key is needed.
func NewProvider(endpoint, model string) *Provider {
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if model == "" {
		model = defaultModel
	}
	return &Provider{
		endpoint: endpoint,
		model:    model,
		client:   &http.Client{Timeout: 120 * time.Second},
	}
}

// SelectHead implements ai.HeadSelector.
func (p *Provider) SelectHead(ctx context.Context, candidates []domain.ElectionCandidate) (string, string, error) {
	prompt := ai.BuildSelectionPrompt(candidates)
	text, err := p.chat(ctx, prompt)
	if err != nil {
		return "", "", err
	}

	var selection struct {
		NodeID string `json:"node_id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &selection); err != nil {
		return "", "", fmt.Errorf("failed to parse ollama response: %w", err)
	}
	return selection.NodeID, selection.Reason, nil
}

// ScheduleTask implements ai.TaskScheduler.
func (p *Provider) ScheduleTask(ctx context.Context, task *domain.Task, workers []ai.WorkerSnapshot) (*ai.ScheduleDecision, error) {
	prompt := ai.BuildTaskSchedulingPrompt(task, workers)
	text, err := p.chat(ctx, prompt)
	if err != nil {
		return nil, err
	}

	var decision ai.ScheduleDecision
	if err := json.Unmarshal([]byte(text), &decision); err != nil {
		return nil, fmt.Errorf("failed to parse schedule decision: %w", err)
	}
	return &decision, nil
}

// EstimateCapacity implements ai.CapacityEstimator.
func (p *Provider) EstimateCapacity(ctx context.Context, worker ai.WorkerSnapshot, pendingTasks []*domain.Task) (*ai.CapacityEstimate, error) {
	prompt := ai.BuildCapacityEstimationPrompt(worker, pendingTasks)
	text, err := p.chat(ctx, prompt)
	if err != nil {
		return nil, err
	}

	var estimate ai.CapacityEstimate
	if err := json.Unmarshal([]byte(text), &estimate); err != nil {
		return nil, fmt.Errorf("failed to parse capacity estimate: %w", err)
	}
	return &estimate, nil
}

// chat sends a prompt to Ollama's native /api/chat endpoint with thinking disabled.
func (p *Provider) chat(ctx context.Context, prompt string) (string, error) {
	reqBody := chatRequest{
		Model:  p.model,
		Stream: false,
		Think:  boolPtr(false),
		Messages: []message{
			{Role: "user", Content: prompt},
		},
		Options: options{NumPredict: 512},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("ollama API returned status %d: %s", resp.StatusCode, string(errBody))
	}

	var result chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Message.Content == "" {
		return "", fmt.Errorf("empty response from ollama")
	}
	return result.Message.Content, nil
}

// Health checks if the Ollama server is reachable.
func (p *Provider) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", p.endpoint+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health check returned status %d", resp.StatusCode)
	}
	return nil
}

// ListModels returns the models available on the Ollama server.
func (p *Provider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama unreachable: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Models []ModelInfo `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Models, nil
}

func boolPtr(b bool) *bool { return &b }

// -- request/response types --

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Stream   bool      `json:"stream"`
	Think    *bool     `json:"think,omitempty"`
	Options  options   `json:"options,omitempty"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type options struct {
	NumPredict int `json:"num_predict,omitempty"`
}

type chatResponse struct {
	Model   string  `json:"model"`
	Message message `json:"message"`
	Done    bool    `json:"done"`
}

// ModelInfo describes an Ollama model.
type ModelInfo struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	Size       int64  `json:"size"`
	Details    ModelDetails `json:"details"`
}

// ModelDetails holds model metadata.
type ModelDetails struct {
	Family          string `json:"family"`
	ParameterSize   string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
}
