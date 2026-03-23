package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dave/naga/internal/domain"
	"github.com/dave/naga/internal/infra/ai"
)

const (
	defaultModel   = "claude-sonnet-4-6"
	anthropicURL   = "https://api.anthropic.com/v1/messages"
	anthropicVer   = "2023-06-01"
)

// Provider implements ai.HeadSelector and ai.TaskScheduler using the Claude Messages API.
type Provider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewProvider creates a Claude provider. If model is empty, defaultModel is used.
func NewProvider(apiKey, model string) *Provider {
	if model == "" {
		model = defaultModel
	}
	return &Provider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

// SelectHead implements ai.HeadSelector.
func (p *Provider) SelectHead(ctx context.Context, candidates []domain.ElectionCandidate) (string, string, error) {
	if p.apiKey == "" {
		return "", "", fmt.Errorf("anthropic API key not configured")
	}

	prompt := buildSelectionPrompt(candidates)
	text, err := p.call(ctx, prompt, 256)
	if err != nil {
		return "", "", err
	}

	var selection struct {
		NodeID string `json:"node_id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &selection); err != nil {
		return "", "", fmt.Errorf("failed to parse AI response: %w", err)
	}

	return selection.NodeID, selection.Reason, nil
}

// ScheduleTask implements ai.TaskScheduler.
func (p *Provider) ScheduleTask(ctx context.Context, task *domain.Task, workers []ai.WorkerSnapshot) (*ai.ScheduleDecision, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("anthropic API key not configured")
	}

	prompt := ai.BuildTaskSchedulingPrompt(task, workers)
	text, err := p.call(ctx, prompt, 512)
	if err != nil {
		return nil, err
	}

	var decision ai.ScheduleDecision
	if err := json.Unmarshal([]byte(text), &decision); err != nil {
		return nil, fmt.Errorf("failed to parse schedule decision: %w", err)
	}

	return &decision, nil
}

func (p *Provider) call(ctx context.Context, prompt string, maxTokens int) (string, error) {
	reqBody := map[string]interface{}{
		"model":      p.model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVer)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claude API returned status %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from claude")
	}

	return result.Content[0].Text, nil
}

func buildSelectionPrompt(candidates []domain.ElectionCandidate) string {
	candidateJSON, _ := json.MarshalIndent(candidates, "", "  ")
	return fmt.Sprintf(`You are a cluster management AI. The head node has failed and you must select the best replacement.

Candidates:
%s

Select considering: lower GPU utilization, more free memory, lower latency, fewer running jobs.

Respond with ONLY valid JSON:
{"node_id": "<selected_node_id>", "reason": "<brief explanation>"}`, string(candidateJSON))
}
