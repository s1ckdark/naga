package zai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dave/naga/internal/domain"
	"github.com/dave/naga/internal/infra/ai"
)

// Provider implements ai.TaskScheduler using the Z.AI API (OpenAI-compatible).
type Provider struct {
	apiKey   string
	endpoint string
	model    string
	client   *http.Client
}

// NewProvider creates a Z.AI provider.
func NewProvider(apiKey, endpoint, model string) *Provider {
	return &Provider{
		apiKey:   apiKey,
		endpoint: endpoint,
		model:    model,
		client:   &http.Client{},
	}
}

// ScheduleTask implements ai.TaskScheduler.
func (p *Provider) ScheduleTask(ctx context.Context, task *domain.Task, workers []ai.WorkerSnapshot) (*ai.ScheduleDecision, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("Z.AI API key not configured")
	}

	prompt := ai.BuildTaskSchedulingPrompt(task, workers)

	reqBody := map[string]interface{}{
		"model":      p.model,
		"max_tokens": 512,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := p.endpoint + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Z.AI API returned status %d", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("empty response from Z.AI")
	}

	var decision ai.ScheduleDecision
	if err := json.Unmarshal([]byte(result.Choices[0].Message.Content), &decision); err != nil {
		return nil, fmt.Errorf("failed to parse schedule decision: %w", err)
	}

	return &decision, nil
}
