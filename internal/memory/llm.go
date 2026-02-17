package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/stellarlinkco/myclaw/internal/config"
)

const (
	extractionPrompt = `You are a memory extraction engine. Extract durable facts from the conversation.

Rules:
1. Extract only explicit facts, no speculation
2. Keep each fact concise and independent
3. Add project/topic/category/importance for each fact
4. category must be one of: identity/config/credential/decision/solution/event/conversation/temp/debug
5. importance must be in [0.0, 1.0]
6. Also provide a concise summary

Return strict JSON object:
{"facts":[{"content":"...","project":"...","topic":"...","category":"...","importance":0.8}],"summary":"..."}

Conversation:
%s`

	dailyCompressPrompt = `Summarize these daily events into reusable memory facts.
Return strict JSON object: {"facts":[{"content":"...","project":"...","topic":"...","category":"...","importance":0.6}]}

Input:
%s`

	weeklyCompressPrompt = `Merge and deduplicate these memory entries.
Return strict JSON object: {"facts":[{"content":"...","project":"...","topic":"...","category":"...","importance":0.7}]}

Input:
%s`

	profileUpdatePrompt = `Update the core profile.
Given current profile and high-importance facts, return strict JSON object:
{"entries":[{"content":"...","category":"identity"}]}

Current profile:
%s

New facts:
%s`
)

type LLMClient interface {
	Extract(conversation string) (*ExtractionResult, error)
	Compress(prompt, content string) (*CompressionResult, error)
	UpdateProfile(currentProfile, newFacts string) (*ProfileResult, error)
}

type llmClient struct {
	apiKey          string
	baseURL         string
	model           string
	reasoningEffort string
	maxTokens       int
	httpClient      *http.Client
}

func NewLLMClient(cfg *config.Config) LLMClient {
	c := &llmClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	if cfg.Memory.Provider != nil {
		c.apiKey = cfg.Memory.Provider.APIKey
		c.baseURL = cfg.Memory.Provider.BaseURL
	}
	if c.apiKey == "" {
		c.apiKey = cfg.Provider.APIKey
	}
	if c.baseURL == "" {
		c.baseURL = cfg.Provider.BaseURL
	}
	if cfg.Memory.Model != "" {
		c.model = cfg.Memory.Model
	} else {
		c.model = cfg.Agent.Model
	}
	if cfg.Memory.MaxTokens > 0 {
		c.maxTokens = cfg.Memory.MaxTokens
	} else {
		c.maxTokens = cfg.Agent.MaxTokens
	}
	c.reasoningEffort = cfg.ModelReasoningEffort()

	return c
}

func (c *llmClient) Extract(conversation string) (*ExtractionResult, error) {
	resp, err := c.complete(fmt.Sprintf(extractionPrompt, conversation))
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}
	var out ExtractionResult
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return nil, fmt.Errorf("parse extraction result: %w", err)
	}
	return &out, nil
}

func (c *llmClient) Compress(prompt, content string) (*CompressionResult, error) {
	resp, err := c.complete(fmt.Sprintf(prompt, content))
	if err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	var out CompressionResult
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return nil, fmt.Errorf("parse compression result: %w", err)
	}
	return &out, nil
}

func (c *llmClient) UpdateProfile(currentProfile, newFacts string) (*ProfileResult, error) {
	resp, err := c.complete(fmt.Sprintf(profileUpdatePrompt, currentProfile, newFacts))
	if err != nil {
		return nil, fmt.Errorf("update profile: %w", err)
	}
	var out ProfileResult
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return nil, fmt.Errorf("parse profile result: %w", err)
	}
	return &out, nil
}

func (c *llmClient) complete(prompt string) (string, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return "", fmt.Errorf("missing memory api key")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("missing memory base url")
	}
	if c.model == "" {
		return "", fmt.Errorf("missing memory model")
	}

	body := map[string]any{
		"model": c.model,
		"messages": []map[string]string{{
			"role":    "user",
			"content": prompt,
		}},
		"max_tokens":  c.maxTokens,
		"temperature": 0.3,
		"response_format": map[string]string{
			"type": "json_object",
		},
	}
	if c.reasoningEffort != "" {
		body["reasoning_effort"] = c.reasoningEffort
	}

	content, statusCode, respBody, err := c.sendChatCompletion(baseURL, body)
	if err == nil {
		return content, nil
	}

	if c.reasoningEffort != "" && isReasoningEffortUnsupported(statusCode, respBody) {
		log.Printf("[memory] warning: reasoning_effort unsupported by memory model; retrying without reasoning_effort")
		delete(body, "reasoning_effort")
		content, _, _, retryErr := c.sendChatCompletion(baseURL, body)
		if retryErr == nil {
			return content, nil
		}
		return "", retryErr
	}

	return "", err
}

func (c *llmClient) sendChatCompletion(baseURL string, body map[string]any) (string, int, []byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", 0, nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", 0, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, respBody, fmt.Errorf("memory model http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return "", resp.StatusCode, respBody, fmt.Errorf("decode response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return "", resp.StatusCode, respBody, fmt.Errorf("empty choices in response")
	}
	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return "", resp.StatusCode, respBody, fmt.Errorf("empty content in response")
	}
	return content, resp.StatusCode, respBody, nil
}

func isReasoningEffortUnsupported(statusCode int, respBody []byte) bool {
	if statusCode != http.StatusBadRequest && statusCode != http.StatusUnprocessableEntity {
		return false
	}

	var decoded struct {
		Error struct {
			Param   string `json:"param"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &decoded); err == nil {
		paramName := strings.ToLower(strings.TrimSpace(decoded.Error.Param))
		if paramName == "reasoning_effort" || paramName == "reasoning.effort" {
			return true
		}

		message := strings.ToLower(strings.TrimSpace(decoded.Error.Message))
		if strings.Contains(message, "reasoning_effort") || strings.Contains(message, "reasoning.effort") {
			return true
		}
	}

	bodyText := strings.ToLower(strings.TrimSpace(string(respBody)))
	return strings.Contains(bodyText, "reasoning_effort") || strings.Contains(bodyText, "reasoning.effort")
}
