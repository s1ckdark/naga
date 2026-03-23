package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dave/naga/internal/domain"
	ai "github.com/dave/naga/internal/infra/ai"
)

const defaultModel    = "gpt-4o"
const defaultEndpoint = "https://api.openai.com/v1/chat/completions"

type Provider struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

func NewProvider(apiKey, model string) *Provider {
	if model == "" {
		model = defaultModel
	}
	return &Provider{
		apiKey:   apiKey,
		model:    model,
		endpoint: defaultEndpoint,
		client:   &http.Client{},
	}
}

func (p *Provider) ScheduleTask(ctx context.Context, task *domain.Task, workers []ai.WorkerSnapshot) (*ai.ScheduleDecision, error) {
	prompt := ai.BuildTaskSchedulingPrompt(task, workers)
	content, err := p.chatCompletion(ctx, prompt)
	if err != nil {
		return nil, err
	}
	var decision ai.ScheduleDecision
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		return nil, fmt.Errorf("failed to parse schedule decision: %w", err)
	}
	return &decision, nil
}

func (p *Provider) EstimateCapacity(ctx context.Context, worker ai.WorkerSnapshot, pendingTasks []*domain.Task) (*ai.CapacityEstimate, error) {
	prompt := ai.BuildCapacityEstimationPrompt(worker, pendingTasks)
	content, err := p.chatCompletion(ctx, prompt)
	if err != nil {
		return nil, err
	}
	var estimate ai.CapacityEstimate
	if err := json.Unmarshal([]byte(content), &estimate); err != nil {
		return nil, fmt.Errorf("failed to parse capacity estimate: %w", err)
	}
	return &estimate, nil
}

func (p *Provider) chatCompletion(ctx context.Context, prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model":           p.model,
		"max_tokens":      512,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai API returned status %d", resp.StatusCode)
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
		return "", fmt.Errorf("empty response from openai")
	}
	return result.Choices[0].Message.Content, nil
}
