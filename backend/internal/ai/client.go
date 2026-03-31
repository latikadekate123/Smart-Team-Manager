package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

func NewClient(apiKey, model, baseURL string) *Client {
	return &Client{
		apiKey:  strings.TrimSpace(apiKey),
		model:   model,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) GenerateSubtasks(title, description string) ([]string, error) {
	if c.apiKey == "" {
		return fallbackSubtasks(title), nil
	}

	prompt := fmt.Sprintf("Break down this task into exactly 5 actionable subtasks. Return strict JSON array of strings only. Task: %s. Context: %s", title, description)
	reqBody := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a sprint planner assistant. Output only JSON."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.4,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("llm request failed: %s", strings.TrimSpace(string(body)))
	}

	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if len(data.Choices) == 0 {
		return fallbackSubtasks(title), nil
	}

	content := strings.TrimSpace(data.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var subtasks []string
	if err := json.Unmarshal([]byte(content), &subtasks); err != nil || len(subtasks) == 0 {
		return fallbackSubtasks(title), nil
	}
	if len(subtasks) > 5 {
		subtasks = subtasks[:5]
	}
	return subtasks, nil
}

func fallbackSubtasks(title string) []string {
	short := strings.TrimSpace(title)
	if short == "" {
		short = "task"
	}
	return []string{
		"Define scope and acceptance criteria for " + short,
		"Design implementation approach and dependencies",
		"Implement core functionality in small increments",
		"Add tests and handle edge cases",
		"Review, document, and deploy",
	}
}
