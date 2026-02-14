package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stellarlinkco/myclaw/internal/config"
)

const queryExpansionPrompt = `You are a retrieval query expander.
Given the original user query, output alternative search tokens.

Rules:
1. Keep expansions concise and retrieval-oriented.
2. Output only likely lexical/semantic variants.
3. No explanations, no markdown.
4. Return strict JSON object with this exact schema:
{"lexical":["..."],"semantic":["..."],"hyde":["..."]}

Query:
%s`

type QueryExpansion struct {
	Lexical  []string `json:"lexical"`
	Semantic []string `json:"semantic"`
	HyDE     []string `json:"hyde"`
}

func (q QueryExpansion) allTokens() []string {
	tokens := make([]string, 0, len(q.Lexical)+len(q.Semantic)+len(q.HyDE))
	tokens = append(tokens, q.Lexical...)
	tokens = append(tokens, q.Semantic...)
	tokens = append(tokens, q.HyDE...)
	return tokens
}

type QueryExpander interface {
	Expand(query string) (*QueryExpansion, error)
}

type queryExpanderClient struct {
	apiKey     string
	baseURL    string
	model      string
	maxTokens  int
	httpClient *http.Client
}

func NewQueryExpander(cfg *config.Config) QueryExpander {
	if cfg == nil {
		return nil
	}

	apiKey := cfg.Provider.APIKey
	baseURL := cfg.Provider.BaseURL
	if cfg.Memory.Provider != nil {
		if strings.TrimSpace(cfg.Memory.Provider.APIKey) != "" {
			apiKey = cfg.Memory.Provider.APIKey
		}
		if strings.TrimSpace(cfg.Memory.Provider.BaseURL) != "" {
			baseURL = cfg.Memory.Provider.BaseURL
		}
	}

	model := cfg.Agent.Model
	if strings.TrimSpace(cfg.Memory.Model) != "" {
		model = cfg.Memory.Model
	}
	maxTokens := cfg.Agent.MaxTokens
	if cfg.Memory.MaxTokens > 0 {
		maxTokens = cfg.Memory.MaxTokens
	}

	if strings.TrimSpace(apiKey) == "" || strings.TrimSpace(baseURL) == "" || strings.TrimSpace(model) == "" {
		return nil
	}

	return &queryExpanderClient{
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		model:      model,
		maxTokens:  maxTokens,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *queryExpanderClient) Expand(query string) (*QueryExpansion, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return &QueryExpansion{}, nil
	}

	body := map[string]any{
		"model": c.model,
		"messages": []map[string]string{{
			"role":    "user",
			"content": fmt.Sprintf(queryExpansionPrompt, q),
		}},
		"max_tokens":  c.maxTokens,
		"temperature": 0.2,
		"response_format": map[string]string{
			"type": "json_object",
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal expansion request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create expansion request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send expansion request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read expansion response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("query expander http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode expansion response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("decode expansion response: empty choices")
	}

	result, err := parseQueryExpansionPayload(decoded.Choices[0].Message.Content)
	if err != nil {
		return nil, fmt.Errorf("parse expansion payload: %w", err)
	}
	return result, nil
}

func parseQueryExpansionPayload(content string) (*QueryExpansion, error) {
	type payload struct {
		Lexical  []string `json:"lexical"`
		Semantic []string `json:"semantic"`
		HyDE     []string `json:"hyde"`
	}

	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(content)))
	dec.DisallowUnknownFields()

	var parsed payload
	if err := dec.Decode(&parsed); err != nil {
		return nil, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple json values in payload")
		}
		return nil, err
	}

	return &QueryExpansion{
		Lexical:  sanitizeFTSTokens(parsed.Lexical),
		Semantic: sanitizeFTSTokens(parsed.Semantic),
		HyDE:     sanitizeFTSTokens(parsed.HyDE),
	}, nil
}
