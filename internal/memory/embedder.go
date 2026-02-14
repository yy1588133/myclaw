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
	embeddingProviderAPI    = "api"
	embeddingProviderOllama = "ollama"

	defaultOllamaEmbeddingBaseURL = "http://127.0.0.1:11434"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

type embedderClient struct {
	provider    string
	baseURL     string
	apiKey      string
	model       string
	expectedDim int
	batchSize   int
	httpClient  *http.Client
}

type embeddingRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type embeddingResponse struct {
	Data []embeddingData `json:"data"`
}

type embeddingData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

func NewEmbedder(cfg *config.Config) Embedder {
	client := &embedderClient{
		provider:   embeddingProviderAPI,
		batchSize:  config.DefaultMemoryEmbeddingBatchSize,
		httpClient: &http.Client{Timeout: time.Duration(config.DefaultMemoryEmbeddingTimeoutMs) * time.Millisecond},
	}

	if cfg == nil {
		return client
	}

	embeddingCfg := cfg.Memory.Embedding
	if provider := strings.ToLower(strings.TrimSpace(embeddingCfg.Provider)); provider != "" {
		client.provider = provider
	}

	client.baseURL = firstNonEmptyTrimmed(
		embeddingCfg.BaseURL,
		memoryProviderBaseURL(cfg),
		cfg.Provider.BaseURL,
	)
	client.apiKey = firstNonEmptyTrimmed(
		embeddingCfg.APIKey,
		memoryProviderAPIKey(cfg),
		cfg.Provider.APIKey,
	)
	client.model = firstNonEmptyTrimmed(embeddingCfg.Model, cfg.Memory.Model, cfg.Agent.Model)
	client.expectedDim = embeddingCfg.Dimension

	if embeddingCfg.TimeoutMs > 0 {
		client.httpClient.Timeout = time.Duration(embeddingCfg.TimeoutMs) * time.Millisecond
	}
	if embeddingCfg.BatchSize > 0 {
		client.batchSize = embeddingCfg.BatchSize
	}

	if client.provider == embeddingProviderOllama && client.baseURL == "" {
		client.baseURL = defaultOllamaEmbeddingBaseURL
	}

	return client
}

func (c *embedderClient) Embed(ctx context.Context, text string) ([]float32, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, fmt.Errorf("embed: empty text")
	}

	vectors, err := c.requestEmbeddings(ctx, trimmed, 1)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}

	return vectors[0], nil
}

func (c *embedderClient) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("embed batch: empty texts")
	}

	normalized := make([]string, len(texts))
	for i, text := range texts {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return nil, fmt.Errorf("embed batch: empty text at index %d", i)
		}
		normalized[i] = trimmed
	}

	if c.batchSize <= 0 || len(normalized) <= c.batchSize {
		vectors, err := c.requestEmbeddings(ctx, normalized, len(normalized))
		if err != nil {
			return nil, fmt.Errorf("embed batch: %w", err)
		}
		return vectors, nil
	}

	vectors := make([][]float32, 0, len(normalized))
	for start := 0; start < len(normalized); start += c.batchSize {
		end := start + c.batchSize
		if end > len(normalized) {
			end = len(normalized)
		}

		chunkVectors, err := c.requestEmbeddings(ctx, normalized[start:end], end-start)
		if err != nil {
			return nil, fmt.Errorf("embed batch: %w", err)
		}
		vectors = append(vectors, chunkVectors...)
	}

	return vectors, nil
}

func (c *embedderClient) requestEmbeddings(ctx context.Context, input any, expectedCount int) ([][]float32, error) {
	if expectedCount <= 0 {
		return nil, fmt.Errorf("invalid expected embedding count: %d", expectedCount)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if c.model == "" {
		return nil, fmt.Errorf("missing embedding model")
	}

	baseURL, err := c.resolveBaseURL()
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(embeddingRequest{Model: c.model, Input: input})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/embeddings", bytes.NewReader(payload))
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
		return nil, fmt.Errorf("embedding http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded embeddingResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	vectors, err := c.validateEmbeddingData(decoded.Data, expectedCount)
	if err != nil {
		return nil, fmt.Errorf("validate response: %w", err)
	}

	return vectors, nil
}

func (c *embedderClient) resolveBaseURL() (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.baseURL), "/")
	provider := strings.ToLower(strings.TrimSpace(c.provider))

	switch provider {
	case "", embeddingProviderAPI:
		if baseURL == "" {
			return "", fmt.Errorf("missing embedding base url")
		}
		if strings.TrimSpace(c.apiKey) == "" {
			return "", fmt.Errorf("missing embedding api key")
		}
		return baseURL, nil
	case embeddingProviderOllama:
		if baseURL == "" {
			baseURL = defaultOllamaEmbeddingBaseURL
		}
		return strings.TrimRight(baseURL, "/"), nil
	default:
		return "", fmt.Errorf("unsupported embedding provider: %s", provider)
	}
}

func (c *embedderClient) validateEmbeddingData(data []embeddingData, expectedCount int) ([][]float32, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty embeddings data")
	}
	if len(data) != expectedCount {
		return nil, fmt.Errorf("response count mismatch: got %d want %d", len(data), expectedCount)
	}

	vectors := make([][]float32, expectedCount)
	seen := make([]bool, expectedCount)
	responseDim := 0

	for _, item := range data {
		if item.Index < 0 || item.Index >= expectedCount {
			return nil, fmt.Errorf("invalid embedding index %d", item.Index)
		}
		if seen[item.Index] {
			return nil, fmt.Errorf("duplicate embedding index %d", item.Index)
		}
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("empty embedding vector at index %d", item.Index)
		}

		if responseDim == 0 {
			responseDim = len(item.Embedding)
		} else if len(item.Embedding) != responseDim {
			return nil, fmt.Errorf("inconsistent embedding dimension at index %d: got %d want %d", item.Index, len(item.Embedding), responseDim)
		}

		if c.expectedDim > 0 && len(item.Embedding) != c.expectedDim {
			return nil, fmt.Errorf("embedding dimension at index %d: got %d want %d", item.Index, len(item.Embedding), c.expectedDim)
		}

		copied := make([]float32, len(item.Embedding))
		copy(copied, item.Embedding)
		vectors[item.Index] = copied
		seen[item.Index] = true
	}

	for idx, ok := range seen {
		if !ok {
			return nil, fmt.Errorf("missing embedding index %d", idx)
		}
	}

	return vectors, nil
}

func memoryProviderAPIKey(cfg *config.Config) string {
	if cfg.Memory.Provider == nil {
		return ""
	}
	return cfg.Memory.Provider.APIKey
}

func memoryProviderBaseURL(cfg *config.Config) string {
	if cfg.Memory.Provider == nil {
		return ""
	}
	return cfg.Memory.Provider.BaseURL
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
