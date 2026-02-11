package memory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stellarlinkco/myclaw/internal/config"
)

func TestLLMClient_RequestAndResponseParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("auth header mismatch")
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["temperature"].(float64) != 0.3 {
			t.Fatalf("expected temperature 0.3")
		}

		resp := map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": `{"facts":[{"content":"x","project":"myclaw","topic":"arch","category":"decision","importance":0.8}],"summary":"s"}`,
				},
			}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.APIKey = "test-key"
	cfg.Provider.BaseURL = srv.URL
	cfg.Agent.Model = "gpt-test"

	client := NewLLMClient(cfg)
	out, err := client.Extract("conversation")
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(out.Facts) != 1 || out.Summary != "s" {
		t.Fatalf("unexpected extract output: %+v", out)
	}
}

func TestLLMClient_ProviderSelection(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.APIKey = "main-key"
	cfg.Provider.BaseURL = "https://main.example.com"
	cfg.Agent.Model = "main-model"

	c := NewLLMClient(cfg).(*llmClient)
	if c.apiKey != "main-key" || c.baseURL != "https://main.example.com" || c.model != "main-model" {
		t.Fatal("expected fallback to main provider/model")
	}

	cfg.Memory.Provider = &config.ProviderConfig{APIKey: "mem-key", BaseURL: "https://mem.example.com"}
	cfg.Memory.Model = "mem-model"
	cfg.Memory.MaxTokens = 1234

	c2 := NewLLMClient(cfg).(*llmClient)
	if c2.apiKey != "mem-key" || c2.baseURL != "https://mem.example.com" || c2.model != "mem-model" || c2.maxTokens != 1234 {
		t.Fatal("expected memory-specific provider/model/maxTokens")
	}
}
