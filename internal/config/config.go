package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	Enabled    bool             `json:"enabled"`
	Model      string           `json:"model,omitempty"`
	MaxTokens  int              `json:"maxTokens,omitempty"`
	DBPath     string           `json:"dbPath,omitempty"`
	Provider   *ProviderConfig  `json:"provider,omitempty"`
	Extraction ExtractionConfig `json:"extraction"`
}

type ExtractionConfig struct {
	QuietGap    string  `json:"quietGap,omitempty"`
	TokenBudget float64 `json:"tokenBudget,omitempty"`
	DailyFlush  string  `json:"dailyFlush,omitempty"`
}

type AgentConfig struct {
	Workspace         string  `json:"workspace"`
	Model             string  `json:"model"`
	MaxTokens         int     `json:"maxTokens"`
	Temperature       float64 `json:"temperature"`
	MaxToolIterations int     `json:"maxToolIterations"`
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
			Enabled: false,
			Extraction: ExtractionConfig{
				QuietGap:    DefaultMemoryQuietGap,
				TokenBudget: DefaultMemoryTokenBudget,
				DailyFlush:  DefaultMemoryDailyFlush,
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

	return cfg, nil
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
