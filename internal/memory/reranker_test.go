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

	"github.com/stellarlinkco/myclaw/internal/config"
)

func TestRerankerAPIEndpoint(t *testing.T) {
	apiCalled := false
	llmCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/rerank":
			apiCalled = true
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer test-rerank-key" {
				t.Fatalf("auth header mismatch: %q", r.Header.Get("Authorization"))
			}

			var body struct {
				Model     string   `json:"model"`
				Query     string   `json:"query"`
				Documents []string `json:"documents"`
				TopN      int      `json:"top_n"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if body.Model != "rerank-test-model" {
				t.Fatalf("model = %q", body.Model)
			}
			if body.Query != "how to deploy" {
				t.Fatalf("query = %q", body.Query)
			}
			if body.TopN != 2 {
				t.Fatalf("top_n = %d", body.TopN)
			}
			if len(body.Documents) != 3 {
				t.Fatalf("documents length = %d", len(body.Documents))
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"index": 2, "relevance_score": 0.8},
					{"index": 0, "relevance_score": 0.2},
				},
			})
		case "/chat/completions":
			llmCalled = true
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"error":"should-not-fallback"}`)
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	r := NewReranker(newRerankerTestConfig(srv.URL))
	scores, err := r.Rerank(context.Background(), "how to deploy", []string{"doc0", "doc1", "doc2"})
	if err != nil {
		t.Fatalf("Rerank error: %v", err)
	}
	if !apiCalled {
		t.Fatal("expected API rerank endpoint to be called")
	}
	if llmCalled {
		t.Fatal("LLM fallback should not be called when API rerank succeeds")
	}

	assertRerankScores(t, scores, []RerankScore{
		{Index: 0, Score: 0},
		{Index: 1, Score: 0},
		{Index: 2, Score: 1},
	})
}

func TestRerankerLLMFallback(t *testing.T) {
	apiCalled := false
	llmCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/rerank":
			apiCalled = true
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"error":"rerank endpoint unavailable"}`)
		case "/chat/completions":
			llmCalled = true

			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode llm request: %v", err)
			}
			if body["model"] != "rerank-test-model" {
				t.Fatalf("llm model = %v", body["model"])
			}
			responseFormat, ok := body["response_format"].(map[string]any)
			if !ok || responseFormat["type"] != "json_object" {
				t.Fatalf("unexpected response_format: %#v", body["response_format"])
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": `{"scores":[{"index":1,"score":7.0},{"index":0,"score":3.0},{"index":2,"score":5.0}]}`,
					},
				}},
			})
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	r := NewReranker(newRerankerTestConfig(srv.URL))
	scores, err := r.Rerank(context.Background(), "fallback query", []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Rerank error: %v", err)
	}
	if !apiCalled {
		t.Fatal("expected API rerank attempt before fallback")
	}
	if !llmCalled {
		t.Fatal("expected LLM fallback call")
	}

	assertRerankScores(t, scores, []RerankScore{
		{Index: 0, Score: 0},
		{Index: 1, Score: 1},
		{Index: 2, Score: 0.5},
	})
}

func TestRerankerMalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/rerank":
			_, _ = fmt.Fprint(w, `{"results":`)
		case "/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": `{"scores":[{"index":0,"score":1.0,"extra":true}]}`,
					},
				}},
			})
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	r := NewReranker(newRerankerTestConfig(srv.URL))
	_, err := r.Rerank(context.Background(), "bad response", []string{"a", "b"})
	if err == nil {
		t.Fatal("expected malformed response error")
	}
	if !strings.Contains(err.Error(), "rerank llm fallback after api failure") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "parse llm rerank content") {
		t.Fatalf("expected strict llm parsing context in error, got: %v", err)
	}
}

func newRerankerTestConfig(baseURL string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Memory.Rerank.Enabled = true
	cfg.Memory.Rerank.Provider = "api"
	cfg.Memory.Rerank.BaseURL = baseURL
	cfg.Memory.Rerank.APIKey = "test-rerank-key"
	cfg.Memory.Rerank.Model = "rerank-test-model"
	cfg.Memory.Rerank.TopN = 2
	cfg.Memory.Rerank.TimeoutMs = 1000
	return cfg
}

func assertRerankScores(t *testing.T, got, want []RerankScore) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Index != want[i].Index {
			t.Fatalf("index[%d] = %d, want %d", i, got[i].Index, want[i].Index)
		}
		if math.Abs(got[i].Score-want[i].Score) > 1e-6 {
			t.Fatalf("score[%d] = %.6f, want %.6f", i, got[i].Score, want[i].Score)
		}
	}
}
