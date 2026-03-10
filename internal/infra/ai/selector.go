package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dave/clusterctl/internal/domain"
)

type ClaudeSelector struct {
	apiKey string
	model  string
}

func NewClaudeSelector(apiKey string) *ClaudeSelector {
	return &ClaudeSelector{
		apiKey: apiKey,
		model:  "claude-sonnet-4-6",
	}
}

func (s *ClaudeSelector) SelectHead(ctx context.Context, candidates []domain.ElectionCandidate) (string, string, error) {
	if s.apiKey == "" {
		return "", "", fmt.Errorf("anthropic API key not configured")
	}

	prompt := buildSelectionPrompt(candidates)

	reqBody := map[string]interface{}{
		"model":      s.model,
		"max_tokens": 256,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("claude API returned status %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}

	if len(result.Content) == 0 {
		return "", "", fmt.Errorf("empty response from claude")
	}

	var selection struct {
		NodeID string `json:"node_id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &selection); err != nil {
		return "", "", fmt.Errorf("failed to parse AI response: %w", err)
	}

	return selection.NodeID, selection.Reason, nil
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
