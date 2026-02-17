package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DefaultModel             = "claude-sonnet-4-5-20250929"
	DefaultMaxTokens         = 8192
	DefaultTemperature       = 0.7
	DefaultMaxToolIterations = 20
	DefaultExecTimeout       = 60
	DefaultHost              = "0.0.0.0"
	DefaultPort              = 18790
	DefaultBufSize           = 100
	DefaultMemoryQuietGap    = "3m"
	DefaultMemoryTokenBudget = 0.6
	DefaultMemoryDailyFlush  = "03:00"

	MemoryRetrievalModeClassic  = "classic"
	MemoryRetrievalModeEnhanced = "enhanced"
	ModelReasoningEffortLow     = "low"
	ModelReasoningEffortMedium  = "medium"
	ModelReasoningEffortHigh    = "high"
	ModelReasoningEffortXHigh   = "xhigh"

	DefaultMemoryRetrievalMode           = MemoryRetrievalModeClassic
	DefaultMemoryStrongSignalThreshold   = 0.85
	DefaultMemoryStrongSignalGap         = 0.15
	DefaultMemoryRetrievalCandidateLimit = 40
	DefaultMemoryRetrievalRerankLimit    = 20
	DefaultMemoryEmbeddingTimeoutMs      = 30000
	DefaultMemoryEmbeddingBatchSize      = 16
	DefaultMemoryRerankTimeoutMs         = 30000
	DefaultMemoryRerankTopN              = 8
)

type Config struct {
	Agent         AgentConfig         `json:"agent"`
	Channels      ChannelsConfig      `json:"channels"`
	Provider      ProviderConfig      `json:"provider"`
	Tools         ToolsConfig         `json:"tools"`
	Skills        SkillsConfig        `json:"skills"`
	Hooks         HooksConfig         `json:"hooks"`
	MCP           MCPConfig           `json:"mcp"`
	AutoCompact   AutoCompactConfig   `json:"autoCompact"`
	TokenTracking TokenTrackingConfig `json:"tokenTracking"`
	Gateway       GatewayConfig       `json:"gateway"`
	Memory        MemoryConfig        `json:"memory"`
}

type MemoryConfig struct {
	Enabled              bool             `json:"enabled"`
	Model                string           `json:"model,omitempty"`
	ModelReasoningEffort string           `json:"modelReasoningEffort,omitempty"`
	MaxTokens            int              `json:"maxTokens,omitempty"`
	DBPath               string           `json:"dbPath,omitempty"`
	Provider             *ProviderConfig  `json:"provider,omitempty"`
	Extraction           ExtractionConfig `json:"extraction"`
	Retrieval            RetrievalConfig  `json:"retrieval"`
	Embedding            EmbeddingConfig  `json:"embedding"`
	Rerank               RerankConfig     `json:"rerank"`
}

type ExtractionConfig struct {
	QuietGap    string  `json:"quietGap,omitempty"`
	TokenBudget float64 `json:"tokenBudget,omitempty"`
	DailyFlush  string  `json:"dailyFlush,omitempty"`
}

type RetrievalConfig struct {
	Mode                  string  `json:"mode,omitempty"`
	StrongSignalThreshold float64 `json:"strongSignalThreshold,omitempty"`
	StrongSignalGap       float64 `json:"strongSignalGap,omitempty"`
	CandidateLimit        int     `json:"candidateLimit,omitempty"`
	RerankLimit           int     `json:"rerankLimit,omitempty"`
}

type EmbeddingConfig struct {
	Enabled   bool   `json:"enabled"`
	Provider  string `json:"provider,omitempty"`
	BaseURL   string `json:"baseUrl,omitempty"`
	APIKey    string `json:"apiKey,omitempty"`
	Model     string `json:"model,omitempty"`
	Dimension int    `json:"dimension,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
	BatchSize int    `json:"batchSize,omitempty"`
}

type RerankConfig struct {
	Enabled   bool   `json:"enabled"`
	Provider  string `json:"provider,omitempty"`
	BaseURL   string `json:"baseUrl,omitempty"`
	APIKey    string `json:"apiKey,omitempty"`
	Model     string `json:"model,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
	TopN      int    `json:"topN,omitempty"`
	Mode      string `json:"mode,omitempty"`
}

type AgentConfig struct {
	Workspace            string  `json:"workspace"`
	Model                string  `json:"model"`
	ModelReasoningEffort string  `json:"modelReasoningEffort,omitempty"`
	MaxTokens            int     `json:"maxTokens"`
	Temperature          float64 `json:"temperature"`
	MaxToolIterations    int     `json:"maxToolIterations"`
}

type ProviderConfig struct {
	Type    string `json:"type,omitempty"` // "anthropic" (default) or "openai"
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl,omitempty"`
}

type ChannelsConfig struct {
	Telegram TelegramConfig `json:"telegram"`
	Feishu   FeishuConfig   `json:"feishu"`
	WeCom    WeComConfig    `json:"wecom"`
	WhatsApp WhatsAppConfig `json:"whatsapp"`
	WebUI    WebUIConfig    `json:"webui"`
}

type TelegramConfig struct {
	Enabled   bool     `json:"enabled"`
	Token     string   `json:"token"`
	AllowFrom []string `json:"allowFrom"`
	Proxy     string   `json:"proxy,omitempty"`
}

type FeishuConfig struct {
	Enabled           bool     `json:"enabled"`
	AppID             string   `json:"appId"`
	AppSecret         string   `json:"appSecret"`
	VerificationToken string   `json:"verificationToken"`
	EncryptKey        string   `json:"encryptKey,omitempty"`
	Port              int      `json:"port,omitempty"`
	AllowFrom         []string `json:"allowFrom"`
}

type WeComConfig struct {
	Enabled        bool     `json:"enabled"`
	Token          string   `json:"token"`
	EncodingAESKey string   `json:"encodingAESKey"`
	ReceiveID      string   `json:"receiveId,omitempty"`
	Port           int      `json:"port,omitempty"`
	AllowFrom      []string `json:"allowFrom"`
}

type ToolsConfig struct {
	BraveAPIKey         string `json:"braveApiKey,omitempty"`
	ExecTimeout         int    `json:"execTimeout"`
	RestrictToWorkspace bool   `json:"restrictToWorkspace"`
}

type GatewayConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type SkillsConfig struct {
	Enabled bool   `json:"enabled"`
	Dir     string `json:"dir,omitempty"` // 默认 workspace/skills
}

type HooksConfig struct {
	PreToolUse  []HookEntry `json:"preToolUse,omitempty"`
	PostToolUse []HookEntry `json:"postToolUse,omitempty"`
	Stop        []HookEntry `json:"stop,omitempty"`
}

type HookEntry struct {
	Command string `json:"command"`
	Pattern string `json:"pattern,omitempty"` // tool name regex
	Timeout int    `json:"timeout,omitempty"` // seconds
}

type MCPConfig struct {
	Servers []string `json:"servers,omitempty"`
}

type WhatsAppConfig struct {
	Enabled   bool     `json:"enabled"`
	JID       string   `json:"jid,omitempty"`
	StorePath string   `json:"storePath,omitempty"`
	AllowFrom []string `json:"allowFrom,omitempty"`
}

type WebUIConfig struct {
	Enabled   bool     `json:"enabled"`
	AllowFrom []string `json:"allowFrom,omitempty"`
}

type AutoCompactConfig struct {
	Enabled       bool    `json:"enabled"`
	Threshold     float64 `json:"threshold,omitempty"`
	PreserveCount int     `json:"preserveCount,omitempty"`
}

type TokenTrackingConfig struct {
	Enabled bool `json:"enabled"`
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Agent: AgentConfig{
			Workspace:         filepath.Join(home, ".myclaw", "workspace"),
			Model:             DefaultModel,
			MaxTokens:         DefaultMaxTokens,
			Temperature:       DefaultTemperature,
			MaxToolIterations: DefaultMaxToolIterations,
		},
		Provider: ProviderConfig{},
		Channels: ChannelsConfig{},
		Tools: ToolsConfig{
			ExecTimeout:         DefaultExecTimeout,
			RestrictToWorkspace: true,
		},
		Skills: SkillsConfig{
			Enabled: true,
		},
		AutoCompact: AutoCompactConfig{
			Enabled:       true,
			Threshold:     0.8,
			PreserveCount: 5,
		},
		Gateway: GatewayConfig{
			Host: DefaultHost,
			Port: DefaultPort,
		},
		Memory: MemoryConfig{
			Enabled: true,
			Extraction: ExtractionConfig{
				QuietGap:    DefaultMemoryQuietGap,
				TokenBudget: DefaultMemoryTokenBudget,
				DailyFlush:  DefaultMemoryDailyFlush,
			},
			Retrieval: RetrievalConfig{
				Mode:                  DefaultMemoryRetrievalMode,
				StrongSignalThreshold: DefaultMemoryStrongSignalThreshold,
				StrongSignalGap:       DefaultMemoryStrongSignalGap,
				CandidateLimit:        DefaultMemoryRetrievalCandidateLimit,
				RerankLimit:           DefaultMemoryRetrievalRerankLimit,
			},
			Embedding: EmbeddingConfig{
				Enabled:   false,
				TimeoutMs: DefaultMemoryEmbeddingTimeoutMs,
				BatchSize: DefaultMemoryEmbeddingBatchSize,
			},
			Rerank: RerankConfig{
				Enabled:   false,
				TimeoutMs: DefaultMemoryRerankTimeoutMs,
				TopN:      DefaultMemoryRerankTopN,
			},
		},
	}
}

func ConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".myclaw")
}

func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

func LoadConfig() (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	// Environment variable overrides
	if key := os.Getenv("MYCLAW_API_KEY"); key != "" {
		cfg.Provider.APIKey = key
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" && cfg.Provider.APIKey == "" {
		cfg.Provider.APIKey = key
	}
	if key := os.Getenv("ANTHROPIC_AUTH_TOKEN"); key != "" && cfg.Provider.APIKey == "" {
		cfg.Provider.APIKey = key
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" && cfg.Provider.APIKey == "" {
		cfg.Provider.APIKey = key
		if cfg.Provider.Type == "" {
			cfg.Provider.Type = "openai"
		}
	}
	if url := os.Getenv("MYCLAW_BASE_URL"); url != "" {
		cfg.Provider.BaseURL = url
	}
	if url := os.Getenv("ANTHROPIC_BASE_URL"); url != "" && cfg.Provider.BaseURL == "" {
		cfg.Provider.BaseURL = url
	}
	if token := os.Getenv("MYCLAW_TELEGRAM_TOKEN"); token != "" {
		cfg.Channels.Telegram.Token = token
	}
	if appID := os.Getenv("MYCLAW_FEISHU_APP_ID"); appID != "" {
		cfg.Channels.Feishu.AppID = appID
	}
	if appSecret := os.Getenv("MYCLAW_FEISHU_APP_SECRET"); appSecret != "" {
		cfg.Channels.Feishu.AppSecret = appSecret
	}
	if token := os.Getenv("MYCLAW_WECOM_TOKEN"); token != "" {
		cfg.Channels.WeCom.Token = token
	}
	if aesKey := os.Getenv("MYCLAW_WECOM_ENCODING_AES_KEY"); aesKey != "" {
		cfg.Channels.WeCom.EncodingAESKey = aesKey
	}
	if receiveID := os.Getenv("MYCLAW_WECOM_RECEIVE_ID"); receiveID != "" {
		cfg.Channels.WeCom.ReceiveID = receiveID
	}
	if enabled := os.Getenv("MYCLAW_MEMORY_ENABLED"); enabled != "" {
		if parsed, err := strconv.ParseBool(enabled); err == nil {
			cfg.Memory.Enabled = parsed
		}
	}
	if model := os.Getenv("MYCLAW_MEMORY_MODEL"); model != "" {
		cfg.Memory.Model = model
	}
	if key := os.Getenv("MYCLAW_MEMORY_API_KEY"); key != "" {
		if cfg.Memory.Provider == nil {
			cfg.Memory.Provider = &ProviderConfig{}
		}
		cfg.Memory.Provider.APIKey = key
	}
	if url := os.Getenv("MYCLAW_MEMORY_BASE_URL"); url != "" {
		if cfg.Memory.Provider == nil {
			cfg.Memory.Provider = &ProviderConfig{}
		}
		cfg.Memory.Provider.BaseURL = url
	}
	if dbPath := os.Getenv("MYCLAW_MEMORY_DB_PATH"); dbPath != "" {
		cfg.Memory.DBPath = dbPath
	}
	if maxTokens := os.Getenv("MYCLAW_MEMORY_MAX_TOKENS"); maxTokens != "" {
		if parsed, err := strconv.Atoi(maxTokens); err == nil {
			cfg.Memory.MaxTokens = parsed
		}
	}
	if quietGap := os.Getenv("MYCLAW_MEMORY_QUIET_GAP"); quietGap != "" {
		cfg.Memory.Extraction.QuietGap = quietGap
	}
	if tokenBudget := os.Getenv("MYCLAW_MEMORY_TOKEN_BUDGET"); tokenBudget != "" {
		if parsed, err := strconv.ParseFloat(tokenBudget, 64); err == nil {
			cfg.Memory.Extraction.TokenBudget = parsed
		}
	}
	if dailyFlush := os.Getenv("MYCLAW_MEMORY_DAILY_FLUSH"); dailyFlush != "" {
		cfg.Memory.Extraction.DailyFlush = dailyFlush
	}
	if mode := os.Getenv("MYCLAW_MEMORY_RETRIEVAL_MODE"); mode != "" {
		cfg.Memory.Retrieval.Mode = mode
	}
	if threshold := os.Getenv("MYCLAW_MEMORY_RETRIEVAL_STRONG_SIGNAL_THRESHOLD"); threshold != "" {
		if parsed, err := strconv.ParseFloat(threshold, 64); err == nil {
			cfg.Memory.Retrieval.StrongSignalThreshold = parsed
		}
	}
	if gap := os.Getenv("MYCLAW_MEMORY_RETRIEVAL_STRONG_SIGNAL_GAP"); gap != "" {
		if parsed, err := strconv.ParseFloat(gap, 64); err == nil {
			cfg.Memory.Retrieval.StrongSignalGap = parsed
		}
	}
	if limit := os.Getenv("MYCLAW_MEMORY_RETRIEVAL_CANDIDATE_LIMIT"); limit != "" {
		if parsed, err := strconv.Atoi(limit); err == nil {
			cfg.Memory.Retrieval.CandidateLimit = parsed
		}
	}
	if limit := os.Getenv("MYCLAW_MEMORY_RETRIEVAL_RERANK_LIMIT"); limit != "" {
		if parsed, err := strconv.Atoi(limit); err == nil {
			cfg.Memory.Retrieval.RerankLimit = parsed
		}
	}
	if enabled := os.Getenv("MYCLAW_MEMORY_EMBEDDING_ENABLED"); enabled != "" {
		if parsed, err := strconv.ParseBool(enabled); err == nil {
			cfg.Memory.Embedding.Enabled = parsed
		}
	}
	if provider := os.Getenv("MYCLAW_MEMORY_EMBEDDING_PROVIDER"); provider != "" {
		cfg.Memory.Embedding.Provider = provider
	}
	if url := os.Getenv("MYCLAW_MEMORY_EMBEDDING_BASE_URL"); url != "" {
		cfg.Memory.Embedding.BaseURL = url
	}
	if key := os.Getenv("MYCLAW_MEMORY_EMBEDDING_API_KEY"); key != "" {
		cfg.Memory.Embedding.APIKey = key
	}
	if model := os.Getenv("MYCLAW_MEMORY_EMBEDDING_MODEL"); model != "" {
		cfg.Memory.Embedding.Model = model
	}
	if dimension := os.Getenv("MYCLAW_MEMORY_EMBEDDING_DIMENSION"); dimension != "" {
		if parsed, err := strconv.Atoi(dimension); err == nil {
			cfg.Memory.Embedding.Dimension = parsed
		}
	}
	if timeout := os.Getenv("MYCLAW_MEMORY_EMBEDDING_TIMEOUT_MS"); timeout != "" {
		if parsed, err := strconv.Atoi(timeout); err == nil {
			cfg.Memory.Embedding.TimeoutMs = parsed
		}
	}
	if batchSize := os.Getenv("MYCLAW_MEMORY_EMBEDDING_BATCH_SIZE"); batchSize != "" {
		if parsed, err := strconv.Atoi(batchSize); err == nil {
			cfg.Memory.Embedding.BatchSize = parsed
		}
	}
	if enabled := os.Getenv("MYCLAW_MEMORY_RERANK_ENABLED"); enabled != "" {
		if parsed, err := strconv.ParseBool(enabled); err == nil {
			cfg.Memory.Rerank.Enabled = parsed
		}
	}
	if provider := os.Getenv("MYCLAW_MEMORY_RERANK_PROVIDER"); provider != "" {
		cfg.Memory.Rerank.Provider = provider
	}
	if url := os.Getenv("MYCLAW_MEMORY_RERANK_BASE_URL"); url != "" {
		cfg.Memory.Rerank.BaseURL = url
	}
	if key := os.Getenv("MYCLAW_MEMORY_RERANK_API_KEY"); key != "" {
		cfg.Memory.Rerank.APIKey = key
	}
	if model := os.Getenv("MYCLAW_MEMORY_RERANK_MODEL"); model != "" {
		cfg.Memory.Rerank.Model = model
	}
	if timeout := os.Getenv("MYCLAW_MEMORY_RERANK_TIMEOUT_MS"); timeout != "" {
		if parsed, err := strconv.Atoi(timeout); err == nil {
			cfg.Memory.Rerank.TimeoutMs = parsed
		}
	}
	if topN := os.Getenv("MYCLAW_MEMORY_RERANK_TOP_N"); topN != "" {
		if parsed, err := strconv.Atoi(topN); err == nil {
			cfg.Memory.Rerank.TopN = parsed
		}
	}
	if mode := os.Getenv("MYCLAW_MEMORY_RERANK_MODE"); mode != "" {
		cfg.Memory.Rerank.Mode = mode
	}

	if cfg.Agent.Workspace == "" {
		cfg.Agent.Workspace = DefaultConfig().Agent.Workspace
	}
	if cfg.Memory.Extraction.QuietGap == "" {
		cfg.Memory.Extraction.QuietGap = DefaultMemoryQuietGap
	}
	if cfg.Memory.Extraction.TokenBudget <= 0 {
		cfg.Memory.Extraction.TokenBudget = DefaultMemoryTokenBudget
	}
	if cfg.Memory.Extraction.DailyFlush == "" {
		cfg.Memory.Extraction.DailyFlush = DefaultMemoryDailyFlush
	}
	cfg.Agent.ModelReasoningEffort = normalizeModelReasoningEffort(cfg.Agent.ModelReasoningEffort)
	cfg.Memory.ModelReasoningEffort = normalizeModelReasoningEffort(cfg.Memory.ModelReasoningEffort)
	cfg.Memory.Retrieval.Mode = normalizeRetrievalMode(cfg.Memory.Retrieval.Mode)
	if cfg.Memory.Retrieval.StrongSignalThreshold < 0 {
		cfg.Memory.Retrieval.StrongSignalThreshold = DefaultMemoryStrongSignalThreshold
	}
	if cfg.Memory.Retrieval.StrongSignalGap < 0 {
		cfg.Memory.Retrieval.StrongSignalGap = DefaultMemoryStrongSignalGap
	}
	if cfg.Memory.Retrieval.CandidateLimit <= 0 {
		cfg.Memory.Retrieval.CandidateLimit = DefaultMemoryRetrievalCandidateLimit
	}
	if cfg.Memory.Retrieval.RerankLimit <= 0 {
		cfg.Memory.Retrieval.RerankLimit = DefaultMemoryRetrievalRerankLimit
	}
	if cfg.Memory.Embedding.Dimension < 0 {
		cfg.Memory.Embedding.Dimension = 0
	}
	if cfg.Memory.Embedding.TimeoutMs <= 0 {
		cfg.Memory.Embedding.TimeoutMs = DefaultMemoryEmbeddingTimeoutMs
	}
	if cfg.Memory.Embedding.BatchSize <= 0 {
		cfg.Memory.Embedding.BatchSize = DefaultMemoryEmbeddingBatchSize
	}
	if cfg.Memory.Rerank.TimeoutMs <= 0 {
		cfg.Memory.Rerank.TimeoutMs = DefaultMemoryRerankTimeoutMs
	}
	if cfg.Memory.Rerank.TopN <= 0 {
		cfg.Memory.Rerank.TopN = DefaultMemoryRerankTopN
	}

	return cfg, nil
}

func (c *Config) ModelReasoningEffort() string {
	if c == nil {
		return ""
	}
	return resolveModelReasoningEffort(c.Memory.ModelReasoningEffort, c.Agent.ModelReasoningEffort)
}

func resolveModelReasoningEffort(memoryEffort, agentEffort string) string {
	normalizedMemoryEffort := normalizeModelReasoningEffort(memoryEffort)
	if normalizedMemoryEffort != "" {
		return normalizedMemoryEffort
	}
	normalizedAgentEffort := normalizeModelReasoningEffort(agentEffort)
	if normalizedAgentEffort != "" {
		return normalizedAgentEffort
	}
	return ""
}

func normalizeRetrievalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", MemoryRetrievalModeClassic:
		return MemoryRetrievalModeClassic
	case MemoryRetrievalModeEnhanced:
		return MemoryRetrievalModeEnhanced
	default:
		return MemoryRetrievalModeClassic
	}
}

func normalizeModelReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ModelReasoningEffortLow:
		return ModelReasoningEffortLow
	case ModelReasoningEffortMedium:
		return ModelReasoningEffortMedium
	case ModelReasoningEffortHigh:
		return ModelReasoningEffortHigh
	case ModelReasoningEffortXHigh:
		return ModelReasoningEffortXHigh
	default:
		return ""
	}
}

func SaveConfig(cfg *Config) error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(ConfigPath(), data, 0644)
}
