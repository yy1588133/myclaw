package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	volume := filepath.VolumeName(home)
	if volume == "" {
		t.Setenv("HOMEDRIVE", "")
		t.Setenv("HOMEPATH", "")
		return
	}

	t.Setenv("HOMEDRIVE", volume)
	homePath := strings.TrimPrefix(home, volume)
	homePath = strings.ReplaceAll(homePath, "/", `\`)
	if homePath == "" {
		homePath = `\`
	}
	if !strings.HasPrefix(homePath, `\`) {
		homePath = `\` + homePath
	}
	t.Setenv("HOMEPATH", homePath)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatal("DefaultConfig returned nil")
	}
	if cfg.Agent.Model != DefaultModel {
		t.Errorf("model = %q, want %q", cfg.Agent.Model, DefaultModel)
	}
	if cfg.Agent.MaxTokens != DefaultMaxTokens {
		t.Errorf("maxTokens = %d, want %d", cfg.Agent.MaxTokens, DefaultMaxTokens)
	}
	if cfg.Agent.MaxToolIterations != DefaultMaxToolIterations {
		t.Errorf("maxToolIterations = %d, want %d", cfg.Agent.MaxToolIterations, DefaultMaxToolIterations)
	}
	if cfg.Gateway.Host != DefaultHost {
		t.Errorf("host = %q, want %q", cfg.Gateway.Host, DefaultHost)
	}
	if cfg.Gateway.Port != DefaultPort {
		t.Errorf("port = %d, want %d", cfg.Gateway.Port, DefaultPort)
	}
	if cfg.Tools.ExecTimeout != DefaultExecTimeout {
		t.Errorf("execTimeout = %d, want %d", cfg.Tools.ExecTimeout, DefaultExecTimeout)
	}
	if !cfg.Tools.RestrictToWorkspace {
		t.Error("restrictToWorkspace should be true by default")
	}
	if !cfg.Skills.Enabled {
		t.Error("skills.enabled should be true by default")
	}
	if !cfg.AutoCompact.Enabled {
		t.Error("autoCompact.enabled should be true by default")
	}
	if cfg.AutoCompact.Threshold != 0.8 {
		t.Errorf("autoCompact.threshold = %v, want 0.8", cfg.AutoCompact.Threshold)
	}
	if cfg.AutoCompact.PreserveCount != 5 {
		t.Errorf("autoCompact.preserveCount = %d, want 5", cfg.AutoCompact.PreserveCount)
	}
	if cfg.Agent.Workspace == "" {
		t.Error("workspace should not be empty")
	}
	if !cfg.Memory.Enabled {
		t.Error("memory.enabled should be true by default")
	}
	if cfg.Memory.Extraction.QuietGap != DefaultMemoryQuietGap {
		t.Errorf("memory.extraction.quietGap = %q, want %q", cfg.Memory.Extraction.QuietGap, DefaultMemoryQuietGap)
	}
}

func TestLoadConfig_NoFile(t *testing.T) {
	// Override config dir to a temp location
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	// Clear any env overrides
	t.Setenv("MYCLAW_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Agent.Model != DefaultModel {
		t.Errorf("expected default model %q, got %q", DefaultModel, cfg.Agent.Model)
	}
}

func TestLoadConfig_FromFile(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	// Clear env overrides
	t.Setenv("MYCLAW_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	// Create config file
	cfgDir := filepath.Join(tmpDir, ".myclaw")
	os.MkdirAll(cfgDir, 0755)

	testCfg := map[string]any{
		"agent": map[string]any{
			"model":     "claude-opus-4-20250514",
			"maxTokens": 4096,
		},
		"provider": map[string]any{
			"apiKey": "sk-test-key",
		},
	}
	data, _ := json.MarshalIndent(testCfg, "", "  ")
	os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0644)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Agent.Model != "claude-opus-4-20250514" {
		t.Errorf("model = %q, want claude-opus-4-20250514", cfg.Agent.Model)
	}
	if cfg.Agent.MaxTokens != 4096 {
		t.Errorf("maxTokens = %d, want 4096", cfg.Agent.MaxTokens)
	}
	if cfg.Provider.APIKey != "sk-test-key" {
		t.Errorf("apiKey = %q, want sk-test-key", cfg.Provider.APIKey)
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	tests := []struct {
		name    string
		envKey  string
		envVal  string
		wantKey string
	}{
		{"MYCLAW_API_KEY", "MYCLAW_API_KEY", "myclaw-key", "myclaw-key"},
		{"ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY", "anthropic-key", "anthropic-key"},
		{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN", "auth-token", "auth-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MYCLAW_API_KEY", "")
			t.Setenv("ANTHROPIC_API_KEY", "")
			t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
			t.Setenv(tt.envKey, tt.envVal)

			cfg, err := LoadConfig()
			if err != nil {
				t.Fatalf("LoadConfig error: %v", err)
			}
			if cfg.Provider.APIKey != tt.wantKey {
				t.Errorf("apiKey = %q, want %q", cfg.Provider.APIKey, tt.wantKey)
			}
		})
	}
}

func TestLoadConfig_EnvPriority(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	// MYCLAW_API_KEY takes priority over ANTHROPIC_API_KEY
	t.Setenv("MYCLAW_API_KEY", "myclaw-wins")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-loses")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "token-loses")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Provider.APIKey != "myclaw-wins" {
		t.Errorf("apiKey = %q, want myclaw-wins", cfg.Provider.APIKey)
	}
}

func TestLoadConfig_BaseURLEnv(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	t.Setenv("MYCLAW_API_KEY", "key")
	t.Setenv("ANTHROPIC_BASE_URL", "http://localhost:8080")
	t.Setenv("MYCLAW_BASE_URL", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Provider.BaseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %q, want http://localhost:8080", cfg.Provider.BaseURL)
	}
}

func TestSaveConfig(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	cfg := DefaultConfig()
	cfg.Provider.APIKey = "test-key"

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, ".myclaw", "config.json"))
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal saved config: %v", err)
	}
	if loaded.Provider.APIKey != "test-key" {
		t.Errorf("saved apiKey = %q, want test-key", loaded.Provider.APIKey)
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	cfgDir := filepath.Join(tmpDir, ".myclaw")
	os.MkdirAll(cfgDir, 0755)
	os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte("invalid json"), 0644)

	_, err := LoadConfig()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadConfig_EmptyWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	cfgDir := filepath.Join(tmpDir, ".myclaw")
	os.MkdirAll(cfgDir, 0755)

	// Config with empty workspace - should use default
	testCfg := map[string]any{
		"agent": map[string]any{
			"workspace": "",
		},
	}
	data, _ := json.MarshalIndent(testCfg, "", "  ")
	os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0644)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Agent.Workspace == "" {
		t.Error("workspace should not be empty")
	}
}

func TestLoadConfig_TelegramToken(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	t.Setenv("MYCLAW_TELEGRAM_TOKEN", "test-telegram-token")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Channels.Telegram.Token != "test-telegram-token" {
		t.Errorf("telegram token = %q, want test-telegram-token", cfg.Channels.Telegram.Token)
	}
}

func TestLoadConfig_MYCLAWBaseURL(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	t.Setenv("MYCLAW_BASE_URL", "http://myclaw.local")
	t.Setenv("ANTHROPIC_BASE_URL", "http://anthropic.local")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	// MYCLAW_BASE_URL takes priority
	if cfg.Provider.BaseURL != "http://myclaw.local" {
		t.Errorf("baseURL = %q, want http://myclaw.local", cfg.Provider.BaseURL)
	}
}

func TestLoadConfig_WeComEnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	t.Setenv("MYCLAW_WECOM_TOKEN", "wecom-token")
	t.Setenv("MYCLAW_WECOM_ENCODING_AES_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG")
	t.Setenv("MYCLAW_WECOM_RECEIVE_ID", "wecom-receive-id")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.Channels.WeCom.Token != "wecom-token" {
		t.Errorf("wecom token = %q, want wecom-token", cfg.Channels.WeCom.Token)
	}
	if cfg.Channels.WeCom.EncodingAESKey != "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG" {
		t.Errorf("wecom aes key = %q, want configured value", cfg.Channels.WeCom.EncodingAESKey)
	}
	if cfg.Channels.WeCom.ReceiveID != "wecom-receive-id" {
		t.Errorf("wecom receiveId = %q, want wecom-receive-id", cfg.Channels.WeCom.ReceiveID)
	}
}

func TestDefaultConfigMemoryRetrievalClassic(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Memory.Retrieval.Mode != MemoryRetrievalModeClassic {
		t.Errorf("memory.retrieval.mode = %q, want %q", cfg.Memory.Retrieval.Mode, MemoryRetrievalModeClassic)
	}
	if cfg.Memory.Embedding.Enabled {
		t.Error("memory.embedding.enabled should be false by default")
	}
	if cfg.Memory.Rerank.Enabled {
		t.Error("memory.rerank.enabled should be false by default")
	}
	if cfg.Memory.Retrieval.CandidateLimit != DefaultMemoryRetrievalCandidateLimit {
		t.Errorf("memory.retrieval.candidateLimit = %d, want %d", cfg.Memory.Retrieval.CandidateLimit, DefaultMemoryRetrievalCandidateLimit)
	}
	if cfg.Memory.Retrieval.RerankLimit != DefaultMemoryRetrievalRerankLimit {
		t.Errorf("memory.retrieval.rerankLimit = %d, want %d", cfg.Memory.Retrieval.RerankLimit, DefaultMemoryRetrievalRerankLimit)
	}
}

func TestLoadConfigBackwardCompatibleMemoryDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	t.Setenv("MYCLAW_MEMORY_MODEL", "")
	t.Setenv("MYCLAW_MEMORY_RETRIEVAL_MODE", "")
	t.Setenv("MYCLAW_MEMORY_EMBEDDING_ENABLED", "")
	t.Setenv("MYCLAW_MEMORY_RERANK_ENABLED", "")
	t.Setenv("MYCLAW_MEMORY_EMBEDDING_TIMEOUT_MS", "")
	t.Setenv("MYCLAW_MEMORY_RERANK_TIMEOUT_MS", "")

	cfgDir := filepath.Join(tmpDir, ".myclaw")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}

	legacyCfg := map[string]any{
		"memory": map[string]any{
			"enabled":   true,
			"model":     "legacy-memory-model",
			"maxTokens": 2048,
			"extraction": map[string]any{
				"quietGap":    "2m",
				"tokenBudget": 0.5,
				"dailyFlush":  "04:00",
			},
		},
	}
	data, err := json.MarshalIndent(legacyCfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.Memory.Model != "legacy-memory-model" {
		t.Errorf("memory.model = %q, want legacy-memory-model", cfg.Memory.Model)
	}
	if cfg.Memory.Retrieval.Mode != MemoryRetrievalModeClassic {
		t.Errorf("memory.retrieval.mode = %q, want %q", cfg.Memory.Retrieval.Mode, MemoryRetrievalModeClassic)
	}
	if cfg.Memory.Embedding.Enabled {
		t.Error("memory.embedding.enabled should remain false for legacy config")
	}
	if cfg.Memory.Rerank.Enabled {
		t.Error("memory.rerank.enabled should remain false for legacy config")
	}
	if cfg.Memory.Embedding.TimeoutMs != DefaultMemoryEmbeddingTimeoutMs {
		t.Errorf("memory.embedding.timeoutMs = %d, want %d", cfg.Memory.Embedding.TimeoutMs, DefaultMemoryEmbeddingTimeoutMs)
	}
	if cfg.Memory.Rerank.TimeoutMs != DefaultMemoryRerankTimeoutMs {
		t.Errorf("memory.rerank.timeoutMs = %d, want %d", cfg.Memory.Rerank.TimeoutMs, DefaultMemoryRerankTimeoutMs)
	}
}

func TestLoadConfigMemoryRetrievalEnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)

	t.Setenv("MYCLAW_MEMORY_RETRIEVAL_MODE", "enhanced")
	t.Setenv("MYCLAW_MEMORY_RETRIEVAL_STRONG_SIGNAL_THRESHOLD", "0.92")
	t.Setenv("MYCLAW_MEMORY_RETRIEVAL_STRONG_SIGNAL_GAP", "0.25")
	t.Setenv("MYCLAW_MEMORY_RETRIEVAL_CANDIDATE_LIMIT", "64")
	t.Setenv("MYCLAW_MEMORY_RETRIEVAL_RERANK_LIMIT", "24")
	t.Setenv("MYCLAW_MEMORY_EMBEDDING_ENABLED", "true")
	t.Setenv("MYCLAW_MEMORY_EMBEDDING_PROVIDER", "ollama")
	t.Setenv("MYCLAW_MEMORY_EMBEDDING_BASE_URL", "http://localhost:11434/v1")
	t.Setenv("MYCLAW_MEMORY_EMBEDDING_API_KEY", "embed-key")
	t.Setenv("MYCLAW_MEMORY_EMBEDDING_MODEL", "nomic-embed-text")
	t.Setenv("MYCLAW_MEMORY_EMBEDDING_DIMENSION", "768")
	t.Setenv("MYCLAW_MEMORY_EMBEDDING_TIMEOUT_MS", "45000")
	t.Setenv("MYCLAW_MEMORY_EMBEDDING_BATCH_SIZE", "32")
	t.Setenv("MYCLAW_MEMORY_RERANK_ENABLED", "true")
	t.Setenv("MYCLAW_MEMORY_RERANK_PROVIDER", "api")
	t.Setenv("MYCLAW_MEMORY_RERANK_BASE_URL", "https://rerank.example.com/v1")
	t.Setenv("MYCLAW_MEMORY_RERANK_API_KEY", "rerank-key")
	t.Setenv("MYCLAW_MEMORY_RERANK_MODEL", "rerank-v1")
	t.Setenv("MYCLAW_MEMORY_RERANK_TIMEOUT_MS", "12000")
	t.Setenv("MYCLAW_MEMORY_RERANK_TOP_N", "12")
	t.Setenv("MYCLAW_MEMORY_RERANK_MODE", "api")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.Memory.Retrieval.Mode != MemoryRetrievalModeEnhanced {
		t.Errorf("memory.retrieval.mode = %q, want %q", cfg.Memory.Retrieval.Mode, MemoryRetrievalModeEnhanced)
	}
	if cfg.Memory.Retrieval.StrongSignalThreshold != 0.92 {
		t.Errorf("memory.retrieval.strongSignalThreshold = %v, want 0.92", cfg.Memory.Retrieval.StrongSignalThreshold)
	}
	if cfg.Memory.Retrieval.StrongSignalGap != 0.25 {
		t.Errorf("memory.retrieval.strongSignalGap = %v, want 0.25", cfg.Memory.Retrieval.StrongSignalGap)
	}
	if cfg.Memory.Retrieval.CandidateLimit != 64 {
		t.Errorf("memory.retrieval.candidateLimit = %d, want 64", cfg.Memory.Retrieval.CandidateLimit)
	}
	if cfg.Memory.Retrieval.RerankLimit != 24 {
		t.Errorf("memory.retrieval.rerankLimit = %d, want 24", cfg.Memory.Retrieval.RerankLimit)
	}
	if !cfg.Memory.Embedding.Enabled {
		t.Error("memory.embedding.enabled = false, want true")
	}
	if cfg.Memory.Embedding.Provider != "ollama" {
		t.Errorf("memory.embedding.provider = %q, want ollama", cfg.Memory.Embedding.Provider)
	}
	if cfg.Memory.Embedding.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("memory.embedding.baseUrl = %q, want http://localhost:11434/v1", cfg.Memory.Embedding.BaseURL)
	}
	if cfg.Memory.Embedding.APIKey != "embed-key" {
		t.Errorf("memory.embedding.apiKey = %q, want embed-key", cfg.Memory.Embedding.APIKey)
	}
	if cfg.Memory.Embedding.Model != "nomic-embed-text" {
		t.Errorf("memory.embedding.model = %q, want nomic-embed-text", cfg.Memory.Embedding.Model)
	}
	if cfg.Memory.Embedding.Dimension != 768 {
		t.Errorf("memory.embedding.dimension = %d, want 768", cfg.Memory.Embedding.Dimension)
	}
	if cfg.Memory.Embedding.TimeoutMs != 45000 {
		t.Errorf("memory.embedding.timeoutMs = %d, want 45000", cfg.Memory.Embedding.TimeoutMs)
	}
	if cfg.Memory.Embedding.BatchSize != 32 {
		t.Errorf("memory.embedding.batchSize = %d, want 32", cfg.Memory.Embedding.BatchSize)
	}
	if !cfg.Memory.Rerank.Enabled {
		t.Error("memory.rerank.enabled = false, want true")
	}
	if cfg.Memory.Rerank.Provider != "api" {
		t.Errorf("memory.rerank.provider = %q, want api", cfg.Memory.Rerank.Provider)
	}
	if cfg.Memory.Rerank.BaseURL != "https://rerank.example.com/v1" {
		t.Errorf("memory.rerank.baseUrl = %q, want https://rerank.example.com/v1", cfg.Memory.Rerank.BaseURL)
	}
	if cfg.Memory.Rerank.APIKey != "rerank-key" {
		t.Errorf("memory.rerank.apiKey = %q, want rerank-key", cfg.Memory.Rerank.APIKey)
	}
	if cfg.Memory.Rerank.Model != "rerank-v1" {
		t.Errorf("memory.rerank.model = %q, want rerank-v1", cfg.Memory.Rerank.Model)
	}
	if cfg.Memory.Rerank.TimeoutMs != 12000 {
		t.Errorf("memory.rerank.timeoutMs = %d, want 12000", cfg.Memory.Rerank.TimeoutMs)
	}
	if cfg.Memory.Rerank.TopN != 12 {
		t.Errorf("memory.rerank.topN = %d, want 12", cfg.Memory.Rerank.TopN)
	}
	if cfg.Memory.Rerank.Mode != "api" {
		t.Errorf("memory.rerank.mode = %q, want api", cfg.Memory.Rerank.Mode)
	}
}

func TestLoadConfigInvalidRetrievalModeFallback(t *testing.T) {
	tmpDir := t.TempDir()
	setTestHome(t, tmpDir)
	t.Setenv("MYCLAW_MEMORY_RETRIEVAL_MODE", "")

	cfgDir := filepath.Join(tmpDir, ".myclaw")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}

	testCfg := map[string]any{
		"memory": map[string]any{
			"retrieval": map[string]any{
				"mode": "unsupported",
			},
		},
	}
	data, err := json.MarshalIndent(testCfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.Memory.Retrieval.Mode != MemoryRetrievalModeClassic {
		t.Errorf("memory.retrieval.mode = %q, want fallback %q", cfg.Memory.Retrieval.Mode, MemoryRetrievalModeClassic)
	}
}

func TestNormalizeModelReasoningEffort(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "low passthrough", input: "low", want: "low"},
		{name: "trim and lowercase xhigh", input: "  XHIGH  ", want: "xhigh"},
		{name: "trim tabs and newline medium", input: "\tMeDiuM\n", want: "medium"},
		{name: "invalid value filtered", input: "turbo", want: ""},
		{name: "empty value filtered", input: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeModelReasoningEffort(tt.input); got != tt.want {
				t.Errorf("normalizeModelReasoningEffort(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLoadConfigModelReasoningEffortNormalization(t *testing.T) {
	tests := []struct {
		name        string
		agentInput  string
		memoryInput string
		wantAgent   string
		wantMemory  string
	}{
		{
			name:        "normalizes valid values",
			agentInput:  "  XHIGH  ",
			memoryInput: "  MeDiuM ",
			wantAgent:   "xhigh",
			wantMemory:  "medium",
		},
		{
			name:        "filters invalid values to empty",
			agentInput:  " turbo ",
			memoryInput: "  ultra  ",
			wantAgent:   "",
			wantMemory:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			setTestHome(t, tmpDir)

			cfgDir := filepath.Join(tmpDir, ".myclaw")
			if err := os.MkdirAll(cfgDir, 0755); err != nil {
				t.Fatalf("create config dir: %v", err)
			}

			testCfg := map[string]any{
				"agent": map[string]any{
					"modelReasoningEffort": tt.agentInput,
				},
				"memory": map[string]any{
					"modelReasoningEffort": tt.memoryInput,
				},
			}
			data, err := json.MarshalIndent(testCfg, "", "  ")
			if err != nil {
				t.Fatalf("marshal config: %v", err)
			}
			if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0644); err != nil {
				t.Fatalf("write config file: %v", err)
			}

			cfg, err := LoadConfig()
			if err != nil {
				t.Fatalf("LoadConfig error: %v", err)
			}

			if cfg.Agent.ModelReasoningEffort != tt.wantAgent {
				t.Errorf("agent.modelReasoningEffort = %q, want %q", cfg.Agent.ModelReasoningEffort, tt.wantAgent)
			}
			if cfg.Memory.ModelReasoningEffort != tt.wantMemory {
				t.Errorf("memory.modelReasoningEffort = %q, want %q", cfg.Memory.ModelReasoningEffort, tt.wantMemory)
			}
		})
	}
}

func TestModelReasoningEffortPrecedence(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "memory override wins",
			cfg: Config{
				Agent:  AgentConfig{ModelReasoningEffort: "low"},
				Memory: MemoryConfig{ModelReasoningEffort: "high"},
			},
			want: "high",
		},
		{
			name: "agent default used when memory unset",
			cfg: Config{
				Agent:  AgentConfig{ModelReasoningEffort: "medium"},
				Memory: MemoryConfig{},
			},
			want: "medium",
		},
		{
			name: "empty when both unset",
			cfg: Config{
				Agent:  AgentConfig{},
				Memory: MemoryConfig{},
			},
			want: "",
		},
		{
			name: "normalizes and filters before precedence",
			cfg: Config{
				Agent:  AgentConfig{ModelReasoningEffort: "  XHIGH "},
				Memory: MemoryConfig{ModelReasoningEffort: " turbo "},
			},
			want: "xhigh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFromMethod := tt.cfg.ModelReasoningEffort()
			if gotFromMethod != tt.want {
				t.Errorf("ModelReasoningEffort() = %q, want %q", gotFromMethod, tt.want)
			}

			gotFromHelper := resolveModelReasoningEffort(tt.cfg.Memory.ModelReasoningEffort, tt.cfg.Agent.ModelReasoningEffort)
			if gotFromHelper != tt.want {
				t.Errorf("resolveModelReasoningEffort(memory=%q, agent=%q) = %q, want %q", tt.cfg.Memory.ModelReasoningEffort, tt.cfg.Agent.ModelReasoningEffort, gotFromHelper, tt.want)
			}
		})
	}
}
