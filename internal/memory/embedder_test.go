package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stellarlinkco/myclaw/internal/config"
)

func TestEmbedderEmbedSingleOpenAICompat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-embed-key" {
			t.Fatalf("auth header mismatch: %q", r.Header.Get("Authorization"))
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "text-embedding-test" {
			t.Fatalf("model = %v", body["model"])
		}
		input, ok := body["input"].(string)
		if !ok {
			t.Fatalf("expected string input, got %T", body["input"])
		}
		if input != "hello embedder" {
			t.Fatalf("input = %q", input)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"index":     0,
				"embedding": []float32{0.1, 0.2, 0.3},
			}},
		})
	}))
	defer srv.Close()

	cfg := newEmbedderTestConfig(srv.URL)
	cfg.Memory.Embedding.Provider = embeddingProviderAPI

	embedder := NewEmbedder(cfg)
	vec, err := embedder.Embed(context.Background(), "  hello embedder  ")
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	assertFloat32Slice(t, vec, []float32{0.1, 0.2, 0.3})
}

func TestEmbedderEmbedBatchOpenAICompat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("expected no auth header for ollama, got %q", got)
		}

		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "text-embedding-test" {
			t.Fatalf("model = %s", body.Model)
		}
		if len(body.Input) != 2 || body.Input[0] != "alpha" || body.Input[1] != "beta" {
			t.Fatalf("unexpected input: %+v", body.Input)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 1, "embedding": []float32{0.4, 0.5}},
				{"index": 0, "embedding": []float32{0.1, 0.2}},
			},
		})
	}))
	defer srv.Close()

	cfg := newEmbedderTestConfig(srv.URL)
	cfg.Memory.Embedding.Provider = embeddingProviderOllama
	cfg.Memory.Embedding.APIKey = ""

	embedder := NewEmbedder(cfg)
	vectors, err := embedder.EmbedBatch(context.Background(), []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("EmbedBatch error: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vectors))
	}
	assertFloat32Slice(t, vectors[0], []float32{0.1, 0.2})
	assertFloat32Slice(t, vectors[1], []float32{0.4, 0.5})
}

func TestEmbedderHandlesTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"index":     0,
				"embedding": []float32{0.1, 0.2},
			}},
		})
	}))
	defer srv.Close()

	cfg := newEmbedderTestConfig(srv.URL)
	cfg.Memory.Embedding.TimeoutMs = 20

	embedder := NewEmbedder(cfg)
	_, err := embedder.Embed(context.Background(), "timeout case")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "embed: send request:") {
		t.Fatalf("expected wrapped send request error, got %v", err)
	}
}

func TestEmbedderResponseCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"index":     0,
				"embedding": []float32{0.1, 0.2},
			}},
		})
	}))
	defer srv.Close()

	cfg := newEmbedderTestConfig(srv.URL)
	embedder := NewEmbedder(cfg)

	_, err := embedder.EmbedBatch(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected count mismatch error")
	}
	if !strings.Contains(err.Error(), "response count mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmbedderRejectsEmptyVector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"index":     0,
				"embedding": []float32{},
			}},
		})
	}))
	defer srv.Close()

	cfg := newEmbedderTestConfig(srv.URL)
	embedder := NewEmbedder(cfg)

	_, err := embedder.Embed(context.Background(), "x")
	if err == nil {
		t.Fatal("expected empty-vector error")
	}
	if !strings.Contains(err.Error(), "empty embedding vector") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmbedderRejectsDimensionMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 0, "embedding": []float32{0.1, 0.2}},
				{"index": 1, "embedding": []float32{0.3, 0.4, 0.5}},
			},
		})
	}))
	defer srv.Close()

	cfg := newEmbedderTestConfig(srv.URL)
	embedder := NewEmbedder(cfg)

	_, err := embedder.EmbedBatch(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
	if !strings.Contains(err.Error(), "inconsistent embedding dimension") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmbedderMalformedPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":`)
	}))
	defer srv.Close()

	cfg := newEmbedderTestConfig(srv.URL)
	embedder := NewEmbedder(cfg)

	_, err := embedder.Embed(context.Background(), "x")
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func newEmbedderTestConfig(baseURL string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Memory.Embedding.Provider = embeddingProviderAPI
	cfg.Memory.Embedding.BaseURL = baseURL
	cfg.Memory.Embedding.APIKey = "test-embed-key"
	cfg.Memory.Embedding.Model = "text-embedding-test"
	cfg.Memory.Embedding.TimeoutMs = 1000
	cfg.Memory.Embedding.BatchSize = 16
	return cfg
}

func assertFloat32Slice(t *testing.T, got, want []float32) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if math.Abs(float64(got[i]-want[i])) > 1e-6 {
			t.Fatalf("value[%d] = %f, want %f", i, got[i], want[i])
		}
	}
}
