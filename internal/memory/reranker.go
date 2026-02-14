package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stellarlinkco/myclaw/internal/config"
)

const (
	rerankProviderAPI    = "api"
	rerankProviderOllama = "ollama"

	defaultOllamaRerankBaseURL = "http://127.0.0.1:11434"

	rerankLLMFallbackPrompt = `You are a retrieval reranker.
Score each document for query relevance.

Return strict JSON object only (no markdown, no extra keys):
{"scores":[{"index":0,"score":0.0}]}

Rules:
1. index must be integer in [0, %d]
2. score must be numeric (higher means more relevant)
3. include each provided index exactly once

Input JSON:
%s`
)

type RerankScore struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
}

type Reranker interface {
	Rerank(ctx context.Context, query string, docs []string) ([]RerankScore, error)
}

type rerankerClient struct {
	provider   string
	baseURL    string
	apiKey     string
	model      string
	topN       int
	httpClient *http.Client
}

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

type rerankResponse struct {
	Results []rerankResult `json:"results"`
	Data    []rerankResult `json:"data"`
}

type rerankResult struct {
	Index          int      `json:"index"`
	Score          *float64 `json:"score,omitempty"`
	RelevanceScore *float64 `json:"relevance_score,omitempty"`
}

func NewReranker(cfg *config.Config) Reranker {
	client := &rerankerClient{
		provider:   rerankProviderAPI,
		topN:       config.DefaultMemoryRerankTopN,
		httpClient: &http.Client{Timeout: time.Duration(config.DefaultMemoryRerankTimeoutMs) * time.Millisecond},
	}

	if cfg == nil {
		return client
	}

	rerankCfg := cfg.Memory.Rerank
	if provider := strings.ToLower(strings.TrimSpace(rerankCfg.Provider)); provider != "" {
		client.provider = provider
	}

	client.baseURL = rerankFirstNonEmptyTrimmed(
		rerankCfg.BaseURL,
		rerankMemoryProviderBaseURL(cfg),
		cfg.Provider.BaseURL,
	)
	client.apiKey = rerankFirstNonEmptyTrimmed(
		rerankCfg.APIKey,
		rerankMemoryProviderAPIKey(cfg),
		cfg.Provider.APIKey,
	)
	client.model = rerankFirstNonEmptyTrimmed(rerankCfg.Model, cfg.Memory.Model, cfg.Agent.Model)

	if rerankCfg.TimeoutMs > 0 {
		client.httpClient.Timeout = time.Duration(rerankCfg.TimeoutMs) * time.Millisecond
	}
	if rerankCfg.TopN > 0 {
		client.topN = rerankCfg.TopN
	}

	if client.provider == rerankProviderOllama && client.baseURL == "" {
		client.baseURL = defaultOllamaRerankBaseURL
	}

	return client
}

func (c *rerankerClient) Rerank(ctx context.Context, query string, docs []string) ([]RerankScore, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return nil, fmt.Errorf("rerank: empty query")
	}

	normalizedDocs, err := normalizeRerankDocs(docs)
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}

	var apiErr error
	if c.shouldTryRerankAPI() {
		scores, err := c.rerankWithAPI(ctx, trimmedQuery, normalizedDocs)
		if err == nil {
			return normalizeRerankScores(len(normalizedDocs), scores)
		}
		apiErr = err
	}

	scores, err := c.rerankWithLLM(ctx, trimmedQuery, normalizedDocs)
	if err == nil {
		return normalizeRerankScores(len(normalizedDocs), scores)
	}

	if apiErr != nil {
		return nil, fmt.Errorf("rerank llm fallback after api failure: api error: %v: fallback error: %w", apiErr, err)
	}

	return nil, fmt.Errorf("rerank llm fallback: %w", err)
}

func (c *rerankerClient) shouldTryRerankAPI() bool {
	if strings.TrimSpace(c.model) == "" {
		return false
	}

	provider := strings.ToLower(strings.TrimSpace(c.provider))
	if provider == rerankProviderOllama {
		return true
	}

	return strings.TrimSpace(c.baseURL) != ""
}

func (c *rerankerClient) rerankWithAPI(ctx context.Context, query string, docs []string) ([]RerankScore, error) {
	if strings.TrimSpace(c.model) == "" {
		return nil, fmt.Errorf("missing rerank model")
	}

	baseURL, err := c.resolveBaseURL()
	if err != nil {
		return nil, fmt.Errorf("resolve base url: %w", err)
	}

	reqBody := rerankRequest{
		Model:     c.model,
		Query:     query,
		Documents: docs,
		TopN:      c.effectiveTopN(len(docs)),
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/rerank", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.apiKey))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rerank http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded rerankResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	raw := decoded.Results
	if len(raw) == 0 {
		raw = decoded.Data
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty rerank results")
	}

	scores := make([]RerankScore, 0, len(raw))
	for _, item := range raw {
		score, ok := item.numericScore()
		if !ok {
			continue
		}
		scores = append(scores, RerankScore{Index: item.Index, Score: score})
	}
	if len(scores) == 0 {
		return nil, fmt.Errorf("rerank results missing numeric score fields")
	}

	return scores, nil
}

func (c *rerankerClient) rerankWithLLM(ctx context.Context, query string, docs []string) ([]RerankScore, error) {
	if strings.TrimSpace(c.model) == "" {
		return nil, fmt.Errorf("missing rerank model")
	}

	baseURL, err := c.resolveBaseURL()
	if err != nil {
		return nil, fmt.Errorf("resolve base url: %w", err)
	}

	prompt, err := buildRerankFallbackPrompt(query, docs)
	if err != nil {
		return nil, fmt.Errorf("build fallback prompt: %w", err)
	}

	body := map[string]any{
		"model": c.model,
		"messages": []map[string]string{{
			"role":    "user",
			"content": prompt,
		}},
		"temperature": 0,
		"max_tokens":  1024,
		"response_format": map[string]string{
			"type": "json_object",
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.apiKey))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("llm rerank http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode llm response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in llm rerank response")
	}

	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return nil, fmt.Errorf("empty content in llm rerank response")
	}

	scores, err := parseLLMRerankContent(content)
	if err != nil {
		return nil, fmt.Errorf("parse llm rerank content: %w", err)
	}
	return scores, nil
}

func (c *rerankerClient) resolveBaseURL() (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.baseURL), "/")
	provider := strings.ToLower(strings.TrimSpace(c.provider))

	switch provider {
	case "", rerankProviderAPI:
		if baseURL == "" {
			return "", fmt.Errorf("missing rerank base url")
		}
		if strings.TrimSpace(c.apiKey) == "" {
			return "", fmt.Errorf("missing rerank api key")
		}
		return baseURL, nil
	case rerankProviderOllama:
		if baseURL == "" {
			baseURL = defaultOllamaRerankBaseURL
		}
		return strings.TrimRight(baseURL, "/"), nil
	default:
		return "", fmt.Errorf("unsupported rerank provider: %s", provider)
	}
}

func (c *rerankerClient) effectiveTopN(docCount int) int {
	if docCount <= 0 {
		return 0
	}
	if c.topN <= 0 || c.topN > docCount {
		return docCount
	}
	return c.topN
}

func (r rerankResult) numericScore() (float64, bool) {
	if r.Score != nil {
		return *r.Score, true
	}
	if r.RelevanceScore != nil {
		return *r.RelevanceScore, true
	}
	return 0, false
}

func buildRerankFallbackPrompt(query string, docs []string) (string, error) {
	input := map[string]any{
		"query":     query,
		"documents": docs,
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal input: %w", err)
	}

	return fmt.Sprintf(rerankLLMFallbackPrompt, len(docs)-1, string(payload)), nil
}

func parseLLMRerankContent(content string) ([]RerankScore, error) {
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()

	var parsed struct {
		Scores []RerankScore `json:"scores"`
	}
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode strict json: %w", err)
	}

	var trailing struct{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing content")
		}
		return nil, fmt.Errorf("unexpected trailing content: %w", err)
	}

	if len(parsed.Scores) == 0 {
		return nil, fmt.Errorf("empty llm rerank scores")
	}

	return parsed.Scores, nil
}

func normalizeRerankDocs(docs []string) ([]string, error) {
	if len(docs) == 0 {
		return nil, fmt.Errorf("empty docs")
	}

	out := make([]string, len(docs))
	for i, doc := range docs {
		trimmed := strings.TrimSpace(doc)
		if trimmed == "" {
			return nil, fmt.Errorf("empty doc at index %d", i)
		}
		out[i] = trimmed
	}

	return out, nil
}

func normalizeRerankScores(docCount int, raw []RerankScore) ([]RerankScore, error) {
	if docCount <= 0 {
		return nil, fmt.Errorf("invalid doc count: %d", docCount)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty rerank scores")
	}

	result := make([]RerankScore, docCount)
	for i := 0; i < docCount; i++ {
		result[i] = RerankScore{Index: i, Score: 0}
	}

	rawByIndex := make([]float64, docCount)
	valid := make([]bool, docCount)
	for _, item := range raw {
		if item.Index < 0 || item.Index >= docCount {
			continue
		}
		if !isFiniteFloat64(item.Score) {
			continue
		}
		if !valid[item.Index] || item.Score > rawByIndex[item.Index] {
			rawByIndex[item.Index] = item.Score
			valid[item.Index] = true
		}
	}

	validCount := 0
	minScore := 0.0
	maxScore := 0.0
	for i, ok := range valid {
		if !ok {
			continue
		}
		score := rawByIndex[i]
		if validCount == 0 {
			minScore = score
			maxScore = score
		} else {
			if score < minScore {
				minScore = score
			}
			if score > maxScore {
				maxScore = score
			}
		}
		validCount++
	}

	if validCount == 0 {
		return nil, fmt.Errorf("no valid rerank scores")
	}

	if maxScore == minScore {
		for i, ok := range valid {
			if ok {
				result[i].Score = 1
			}
		}
		return result, nil
	}

	rangeSize := maxScore - minScore
	for i, ok := range valid {
		if !ok {
			continue
		}
		normalized := (rawByIndex[i] - minScore) / rangeSize
		if normalized < 0 {
			normalized = 0
		}
		if normalized > 1 {
			normalized = 1
		}
		result[i].Score = normalized
	}

	return result, nil
}

func rerankMemoryProviderAPIKey(cfg *config.Config) string {
	if cfg.Memory.Provider == nil {
		return ""
	}
	return cfg.Memory.Provider.APIKey
}

func rerankMemoryProviderBaseURL(cfg *config.Config) string {
	if cfg.Memory.Provider == nil {
		return ""
	}
	return cfg.Memory.Provider.BaseURL
}

func rerankFirstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
