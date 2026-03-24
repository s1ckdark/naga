package lmstudio

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
	defaultModel    = "gpt-oss-20b"
	defaultEndpoint = "http://localhost:1234"
)

// Provider implements ai.HeadSelector, ai.TaskScheduler, and ai.CapacityEstimator
// using LM Studio's OpenAI-compatible API.
type Provider struct {
	endpoint string
	model    string
	client   *http.Client
}

// NewProvider creates an LM Studio provider. No API key is needed.
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
	text, err := p.chatCompletion(ctx, prompt)
	if err != nil {
		return "", "", err
	}

	var selection struct {
		NodeID string `json:"node_id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &selection); err != nil {
		return "", "", fmt.Errorf("failed to parse lmstudio response: %w", err)
	}
	return selection.NodeID, selection.Reason, nil
}

// ScheduleTask implements ai.TaskScheduler.
func (p *Provider) ScheduleTask(ctx context.Context, task *domain.Task, workers []ai.WorkerSnapshot) (*ai.ScheduleDecision, error) {
	prompt := ai.BuildTaskSchedulingPrompt(task, workers)
	text, err := p.chatCompletion(ctx, prompt)
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
	text, err := p.chatCompletion(ctx, prompt)
	if err != nil {
		return nil, err
	}

	var estimate ai.CapacityEstimate
	if err := json.Unmarshal([]byte(text), &estimate); err != nil {
		return nil, fmt.Errorf("failed to parse capacity estimate: %w", err)
	}
	return &estimate, nil
}

// chatCompletion sends a prompt to LM Studio's OpenAI-compatible endpoint.
func (p *Provider) chatCompletion(ctx context.Context, prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model":      p.model,
		"max_tokens": 512,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("lmstudio request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("lmstudio API returned status %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from lmstudio")
	}
	return result.Choices[0].Message.Content, nil
}

// Health checks if the LM Studio server is reachable.
func (p *Provider) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", p.endpoint+"/v1/models", nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("lmstudio unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("lmstudio health check returned status %d", resp.StatusCode)
	}
	return nil
}

// ListModels returns the models loaded in LM Studio.
func (p *Provider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.endpoint+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lmstudio unreachable: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// ModelInfo describes an LM Studio model.
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}
