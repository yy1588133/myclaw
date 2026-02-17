package memory

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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

func TestLLMClient_ReasoningEffortPrecedence(t *testing.T) {
	tests := []struct {
		name         string
		memoryEffort string
		agentEffort  string
		want         string
	}{
		{
			name:         "memory overrides agent",
			memoryEffort: "high",
			agentEffort:  "low",
			want:         "high",
		},
		{
			name:         "agent used when memory empty",
			memoryEffort: "",
			agentEffort:  "medium",
			want:         "medium",
		},
		{
			name:         "empty when both empty",
			memoryEffort: "",
			agentEffort:  "",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Memory.ModelReasoningEffort = tt.memoryEffort
			cfg.Agent.ModelReasoningEffort = tt.agentEffort

			c := NewLLMClient(cfg).(*llmClient)
			if c.reasoningEffort != tt.want {
				t.Fatalf("reasoningEffort = %q, want %q", c.reasoningEffort, tt.want)
			}
		})
	}
}

func TestLLMClient_ReasoningEffort_PayloadInjection(t *testing.T) {
	tests := []struct {
		name         string
		memoryEffort string
		agentEffort  string
		want         string
	}{
		{
			name:         "injects when resolved non-empty",
			memoryEffort: "high",
			agentEffort:  "low",
			want:         "high",
		},
		{
			name:         "omits when resolved empty",
			memoryEffort: "",
			agentEffort:  "",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Fatalf("decode request: %v", err)
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
			cfg.Memory.ModelReasoningEffort = tt.memoryEffort
			cfg.Agent.ModelReasoningEffort = tt.agentEffort

			client := NewLLMClient(cfg)
			if _, err := client.Extract("conversation"); err != nil {
				t.Fatalf("Extract error: %v", err)
			}

			got, exists := captured["reasoning_effort"]
			if tt.want == "" {
				if exists {
					t.Fatalf("unexpected reasoning_effort in payload: %v", got)
				}
				return
			}

			if !exists {
				t.Fatalf("expected reasoning_effort=%q in payload", tt.want)
			}
			if got != tt.want {
				t.Fatalf("reasoning_effort = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestLLMClient_ReasoningEffort_UnsupportedFallback(t *testing.T) {
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, body)

		switch len(requests) {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"param":   "reasoning_effort",
					"message": "Unsupported parameter: 'reasoning_effort'",
				},
			})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": `{"facts":[{"content":"x","project":"myclaw","topic":"arch","category":"decision","importance":0.8}],"summary":"s"}`,
					},
				}},
			})
		default:
			t.Fatalf("unexpected extra retry: %d", len(requests))
		}
	}))
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.Provider.APIKey = "test-key"
	cfg.Provider.BaseURL = srv.URL
	cfg.Agent.Model = "gpt-test"
	cfg.Agent.ModelReasoningEffort = "medium"

	logBuf := bytes.NewBuffer(nil)
	prevLogWriter := log.Writer()
	prevLogFlags := log.Flags()
	log.SetOutput(logBuf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prevLogWriter)
		log.SetFlags(prevLogFlags)
	}()

	client := NewLLMClient(cfg)
	if _, err := client.Extract("conversation"); err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}

	first := requests[0]
	if first["reasoning_effort"] != "medium" {
		t.Fatalf("first request reasoning_effort = %v, want %q", first["reasoning_effort"], "medium")
	}

	second := requests[1]
	if _, exists := second["reasoning_effort"]; exists {
		t.Fatalf("second request unexpectedly includes reasoning_effort: %v", second["reasoning_effort"])
	}

	delete(first, "reasoning_effort")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("fallback payload mismatch: first(without reasoning_effort)=%v second=%v", first, second)
	}

	if !strings.Contains(logBuf.String(), "retrying without reasoning_effort") {
		t.Fatalf("expected warning log about unsupported reasoning_effort, got: %q", logBuf.String())
	}
}
