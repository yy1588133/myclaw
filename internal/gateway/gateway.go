package gateway

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cexll/agentsdk-go/pkg/api"
	"github.com/cexll/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/myclaw/internal/bus"
	"github.com/stellarlinkco/myclaw/internal/channel"
	"github.com/stellarlinkco/myclaw/internal/config"
	"github.com/stellarlinkco/myclaw/internal/cron"
	"github.com/stellarlinkco/myclaw/internal/heartbeat"
	"github.com/stellarlinkco/myclaw/internal/memory"
	"github.com/stellarlinkco/myclaw/internal/skills"
)

// Runtime interface for agent runtime (allows mocking in tests)
type Runtime interface {
	Run(ctx context.Context, req api.Request) (*api.Response, error)
	Close()
}

// runtimeAdapter wraps api.Runtime to implement Runtime interface
type runtimeAdapter struct {
	rt *api.Runtime
}

func (r *runtimeAdapter) Run(ctx context.Context, req api.Request) (*api.Response, error) {
	return r.rt.Run(ctx, req)
}

func (r *runtimeAdapter) Close() {
	r.rt.Close()
}

// RuntimeFactory creates a Runtime instance
type RuntimeFactory func(cfg *config.Config, sysPrompt string) (Runtime, error)

// Options for creating a Gateway
type Options struct {
	RuntimeFactory RuntimeFactory
	SignalChan     chan os.Signal // for testing signal handling
}

// DefaultRuntimeFactory creates the default agentsdk-go runtime
func DefaultRuntimeFactory(cfg *config.Config, sysPrompt string) (Runtime, error) {
	return newRuntime(cfg, sysPrompt, nil)
}

func newRuntime(cfg *config.Config, sysPrompt string, skillRegs []api.SkillRegistration) (Runtime, error) {
	var provider api.ModelFactory
	switch cfg.Provider.Type {
	case "openai":
		provider = &model.OpenAIProvider{
			APIKey:    cfg.Provider.APIKey,
			BaseURL:   cfg.Provider.BaseURL,
			ModelName: cfg.Agent.Model,
			MaxTokens: cfg.Agent.MaxTokens,
		}
	default: // "anthropic" or empty
		provider = &model.AnthropicProvider{
			APIKey:    cfg.Provider.APIKey,
			BaseURL:   cfg.Provider.BaseURL,
			ModelName: cfg.Agent.Model,
			MaxTokens: cfg.Agent.MaxTokens,
		}
	}

	rt, err := api.New(context.Background(), api.Options{
		ProjectRoot:   cfg.Agent.Workspace,
		ModelFactory:  provider,
		SystemPrompt:  sysPrompt,
		MaxIterations: cfg.Agent.MaxToolIterations,
		MCPServers:    cfg.MCP.Servers,
		TokenTracking: cfg.TokenTracking.Enabled,
		AutoCompact: api.CompactConfig{
			Enabled:       cfg.AutoCompact.Enabled,
			Threshold:     cfg.AutoCompact.Threshold,
			PreserveCount: cfg.AutoCompact.PreserveCount,
		},
		Skills: skillRegs,
	})
	if err != nil {
		return nil, fmt.Errorf("create runtime: %w", err)
	}
	return &runtimeAdapter{rt: rt}, nil
}

type Gateway struct {
	cfg                *config.Config
	bus                *bus.MessageBus
	runtime            Runtime
	channels           *channel.ChannelManager
	cron               *cron.Service
	hb                 *heartbeat.Service
	memEngine          *memory.Engine
	memLLM             memory.LLMClient
	extraction         *memory.ExtractionService
	retrieveClassicFn  func(string) ([]memory.Memory, error)
	retrieveEnhancedFn func(string) ([]memory.Memory, error)
	skillRegs          []api.SkillRegistration
	signalChan         chan os.Signal // for testing
}

// New creates a Gateway with default options
func New(cfg *config.Config) (*Gateway, error) {
	return NewWithOptions(cfg, Options{})
}

// NewWithOptions creates a Gateway with custom options for testing
func NewWithOptions(cfg *config.Config, opts Options) (*Gateway, error) {
	g := &Gateway{cfg: cfg}

	// Message bus
	g.bus = bus.NewMessageBus(config.DefaultBufSize)

	// Memory (SQLite layered memory is the primary runtime backend)
	dbPath := strings.TrimSpace(cfg.Memory.DBPath)
	if dbPath == "" {
		dbPath = filepath.Join(config.ConfigDir(), "data", "memory.db")
	}
	engine, err := memory.NewEngine(dbPath)
	if err != nil {
		return nil, fmt.Errorf("create memory engine: %w", err)
	}
	g.memEngine = engine

	empty, err := g.memEngine.IsEmpty()
	if err != nil {
		_ = g.memEngine.Close()
		return nil, fmt.Errorf("inspect memory engine state: %w", err)
	}
	if empty {
		if err := memory.MigrateFromFiles(cfg.Agent.Workspace, g.memEngine); err != nil {
			_ = g.memEngine.Close()
			return nil, fmt.Errorf("migrate legacy file memory: %w", err)
		}
	}
	if projects, err := g.memEngine.LoadKnownProjects(); err != nil {
		log.Printf("[memory] load known projects warning: %v", err)
	} else {
		g.memEngine.SetKnownProjects(projects)
	}

	g.memEngine.SetRetrievalConfig(g.retrievalConfigForMode(config.MemoryRetrievalModeClassic))
	if strings.EqualFold(strings.TrimSpace(g.cfg.Memory.Retrieval.Mode), config.MemoryRetrievalModeEnhanced) {
		g.memEngine.SetQueryExpander(memory.NewQueryExpander(cfg))
		if cfg.Memory.Rerank.Enabled {
			g.memEngine.SetReranker(memory.NewReranker(cfg))
		}
	}
	if cfg.Memory.Embedding.Enabled {
		embeddingModel := strings.TrimSpace(cfg.Memory.Embedding.Model)
		if embeddingModel == "" {
			embeddingModel = strings.TrimSpace(cfg.Memory.Model)
		}
		if embeddingModel == "" {
			embeddingModel = strings.TrimSpace(cfg.Agent.Model)
		}
		g.memEngine.SetEmbedder(memory.NewEmbedder(cfg), embeddingModel, cfg.Memory.Embedding.TimeoutMs)
	}
	g.ensureRetrievalFns()

	g.memLLM = memory.NewLLMClient(cfg)
	g.extraction = memory.NewExtractionService(g.memEngine, g.memLLM, cfg.Memory.Extraction)

	// Build system prompt
	sysPrompt := g.buildSystemPrompt()

	if cfg.Skills.Enabled {
		skillDir := cfg.Skills.Dir
		if skillDir == "" {
			skillDir = filepath.Join(cfg.Agent.Workspace, "skills")
		}
		skillRegs, err := skills.LoadSkills(skillDir)
		if err != nil {
			log.Printf("[gateway] skills load warning: %v", err)
		}
		g.skillRegs = skillRegs
	}

	// Create runtime using factory (allows injection for testing)
	factory := opts.RuntimeFactory
	var rt Runtime
	if factory == nil {
		rt, err = newRuntime(cfg, sysPrompt, g.skillRegs)
	} else {
		rt, err = factory(cfg, sysPrompt)
	}
	if err != nil {
		_ = g.memEngine.Close()
		return nil, err
	}
	g.runtime = rt

	// Signal channel for testing
	g.signalChan = opts.SignalChan

	// runAgent helper for cron/heartbeat
	runAgent := func(prompt string) (string, error) {
		return g.runAgent(context.Background(), prompt, "system", nil)
	}

	// Cron
	cronStorePath := filepath.Join(config.ConfigDir(), "data", "cron", "jobs.json")
	g.cron = cron.NewService(cronStorePath)
	g.cron.OnJob = func(job cron.CronJob) (string, error) {
		switch job.Payload.Message {
		case "__internal:memory:daily-compress":
			return "ok", g.memEngine.DailyCompress(g.memLLM)
		case "__internal:memory:weekly-compress":
			return "ok", g.memEngine.WeeklyDeepCompress(g.memLLM)
		}

		result, err := runAgent(job.Payload.Message)
		if err != nil {
			return "", err
		}
		if job.Payload.Deliver && job.Payload.Channel != "" {
			g.bus.Outbound <- bus.OutboundMessage{
				Channel: job.Payload.Channel,
				ChatID:  job.Payload.To,
				Content: result,
			}
		}
		return result, nil
	}

	// Heartbeat
	g.hb = heartbeat.New(cfg.Agent.Workspace, runAgent, 0)

	// Channels (with gateway config for WebUI port)
	chMgr, err := channel.NewChannelManagerWithGateway(cfg.Channels, cfg.Gateway, g.bus)
	if err != nil {
		return nil, fmt.Errorf("create channel manager: %w", err)
	}
	g.channels = chMgr

	return g, nil
}

func (g *Gateway) buildSystemPrompt() string {
	var sb strings.Builder

	if data, err := os.ReadFile(filepath.Join(g.cfg.Agent.Workspace, "AGENTS.md")); err == nil {
		sb.Write(data)
		sb.WriteString("\n\n")
	}

	if data, err := os.ReadFile(filepath.Join(g.cfg.Agent.Workspace, "SOUL.md")); err == nil {
		sb.Write(data)
		sb.WriteString("\n\n")
	}

	if profile, err := g.memEngine.LoadTier1(); err != nil {
		log.Printf("[memory] load tier1 for system prompt warning: %v", err)
	} else if strings.TrimSpace(profile) != "" {
		sb.WriteString("# Core Memory\n")
		sb.WriteString(profile)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

func (g *Gateway) ensureInternalMemoryJobs() error {
	const (
		dailyName  = "__internal_memory_daily_compress"
		weeklyName = "__internal_memory_weekly_compress"
		dailyMsg   = "__internal:memory:daily-compress"
		weeklyMsg  = "__internal:memory:weekly-compress"
		dailyExpr  = "0 0 3 * * *"
		weeklyExpr = "0 0 4 * * 1"
	)

	jobs := g.cron.ListJobs()
	hasDaily := false
	hasWeekly := false
	for _, job := range jobs {
		if job.Payload.Message == dailyMsg || job.Name == dailyName {
			hasDaily = true
		}
		if job.Payload.Message == weeklyMsg || job.Name == weeklyName {
			hasWeekly = true
		}
	}

	if !hasDaily {
		_, err := g.cron.AddJob(dailyName, cron.Schedule{Kind: "cron", Expr: dailyExpr}, cron.Payload{Message: dailyMsg})
		if err != nil {
			return err
		}
	}
	if !hasWeekly {
		_, err := g.cron.AddJob(weeklyName, cron.Schedule{Kind: "cron", Expr: weeklyExpr}, cron.Payload{Message: weeklyMsg})
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *Gateway) runAgent(ctx context.Context, prompt, sessionID string, contentBlocks []model.ContentBlock) (string, error) {
	// Workaround: agentsdk-go drops Prompt when ContentBlocks exist (anthropic.go:420-431).
	// Merge text prompt into ContentBlocks so both text and media reach the API.
	blocks := contentBlocks
	if len(contentBlocks) > 0 && strings.TrimSpace(prompt) != "" {
		blocks = make([]model.ContentBlock, 0, len(contentBlocks)+1)
		blocks = append(blocks, model.ContentBlock{Type: model.ContentBlockText, Text: prompt})
		blocks = append(blocks, contentBlocks...)
		prompt = "" // clear to avoid duplication if SDK is fixed later
	}

	resp, err := g.runtime.Run(ctx, api.Request{
		Prompt:        prompt,
		ContentBlocks: blocks,
		SessionID:     sessionID,
	})
	if err != nil {
		return "", err
	}
	if resp == nil || resp.Result == nil {
		return "", nil
	}
	return resp.Result.Output, nil
}

func (g *Gateway) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go g.bus.DispatchOutbound(ctx)

	if err := g.channels.StartAll(ctx); err != nil {
		return fmt.Errorf("start channels: %w", err)
	}
	log.Printf("[gateway] channels started: %v", g.channels.EnabledChannels())

	if err := g.cron.Start(ctx); err != nil {
		log.Printf("[gateway] cron start warning: %v", err)
	}
	if err := g.ensureInternalMemoryJobs(); err != nil {
		log.Printf("[gateway] ensure internal memory jobs warning: %v", err)
	}

	if g.extraction != nil {
		g.extraction.Start(ctx)
	}

	go func() {
		if err := g.hb.Start(ctx); err != nil {
			log.Printf("[gateway] heartbeat error: %v", err)
		}
	}()

	go g.processLoop(ctx)

	log.Printf("[gateway] running on %s:%d", g.cfg.Gateway.Host, g.cfg.Gateway.Port)

	// Use injected signal channel for testing, or create default
	sigCh := g.signalChan
	if sigCh == nil {
		sigCh = make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	}
	<-sigCh

	log.Printf("[gateway] shutting down...")
	return g.Shutdown()
}

func (g *Gateway) processLoop(ctx context.Context) {
	for {
		select {
		case msg := <-g.bus.Inbound:
			log.Printf("[gateway] inbound from %s/%s: %s", msg.Channel, msg.SenderID, truncate(msg.Content, 80))

			if g.extraction != nil {
				go g.extraction.BufferMessage(msg.Channel, msg.SenderID, "user", msg.Content)
			}

			prompt := msg.Content
			if memory.ShouldRetrieve(msg.Content) {
				memories, err := g.retrieveMemories(msg.Content)
				if err != nil {
					log.Printf("[memory] retrieve warning: %v", err)
				} else if len(memories) > 0 {
					memoryContext := memory.FormatMemories(memories)
					prompt = fmt.Sprintf("[Relevant Memory]\n%s\n\n[User Message]\n%s", memoryContext, msg.Content)
				}
			}

			result, err := g.runAgent(ctx, prompt, msg.SessionKey(), msg.ContentBlocks)
			if err != nil {
				log.Printf("[gateway] agent error: %v", err)
				result = "Sorry, I encountered an error processing your message."
			}

			if g.extraction != nil && strings.TrimSpace(result) != "" {
				go g.extraction.BufferMessage(msg.Channel, msg.SenderID, "assistant", result)
			}

			if result != "" {
				g.bus.Outbound <- bus.OutboundMessage{
					Channel: msg.Channel,
					ChatID:  msg.ChatID,
					Content: result,
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (g *Gateway) ensureRetrievalFns() {
	if g.memEngine == nil {
		return
	}
	if g.retrieveClassicFn == nil {
		g.retrieveClassicFn = func(msg string) ([]memory.Memory, error) {
			g.memEngine.SetRetrievalConfig(g.retrievalConfigForMode(config.MemoryRetrievalModeClassic))
			return g.memEngine.Retrieve(msg)
		}
	}
	if g.retrieveEnhancedFn == nil {
		g.retrieveEnhancedFn = func(msg string) ([]memory.Memory, error) {
			g.memEngine.SetRetrievalConfig(g.retrievalConfigForMode(config.MemoryRetrievalModeEnhanced))
			return g.memEngine.Retrieve(msg)
		}
	}
}

func (g *Gateway) retrievalConfigForMode(mode string) config.RetrievalConfig {
	retrievalCfg := config.RetrievalConfig{
		Mode:                  config.DefaultMemoryRetrievalMode,
		StrongSignalThreshold: config.DefaultMemoryStrongSignalThreshold,
		StrongSignalGap:       config.DefaultMemoryStrongSignalGap,
		CandidateLimit:        config.DefaultMemoryRetrievalCandidateLimit,
		RerankLimit:           config.DefaultMemoryRetrievalRerankLimit,
	}
	if g.cfg != nil {
		retrievalCfg = g.cfg.Memory.Retrieval
	}

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case config.MemoryRetrievalModeEnhanced:
		retrievalCfg.Mode = config.MemoryRetrievalModeEnhanced
	default:
		retrievalCfg.Mode = config.MemoryRetrievalModeClassic
	}

	if retrievalCfg.StrongSignalThreshold < 0 {
		retrievalCfg.StrongSignalThreshold = config.DefaultMemoryStrongSignalThreshold
	}
	if retrievalCfg.StrongSignalGap < 0 {
		retrievalCfg.StrongSignalGap = config.DefaultMemoryStrongSignalGap
	}
	if retrievalCfg.CandidateLimit <= 0 {
		retrievalCfg.CandidateLimit = config.DefaultMemoryRetrievalCandidateLimit
	}
	if retrievalCfg.RerankLimit <= 0 {
		retrievalCfg.RerankLimit = config.DefaultMemoryRetrievalRerankLimit
	}

	return retrievalCfg
}

func (g *Gateway) retrieveMemories(msg string) ([]memory.Memory, error) {
	g.ensureRetrievalFns()

	mode := config.MemoryRetrievalModeClassic
	if g.cfg != nil {
		mode = strings.ToLower(strings.TrimSpace(g.cfg.Memory.Retrieval.Mode))
	}

	if mode == config.MemoryRetrievalModeEnhanced {
		if g.retrieveEnhancedFn == nil {
			return nil, nil
		}
		memories, err := g.retrieveEnhancedFn(msg)
		if err == nil {
			return memories, nil
		}
		log.Printf("[memory] enhanced retrieve warning, falling back to classic: %v", err)
		if g.retrieveClassicFn == nil {
			return nil, nil
		}
		return g.retrieveClassicFn(msg)
	}

	if g.retrieveClassicFn == nil {
		return nil, nil
	}
	return g.retrieveClassicFn(msg)
}

func (g *Gateway) Shutdown() error {
	if g.extraction != nil {
		g.extraction.Stop()
	}
	g.cron.Stop()
	if g.memEngine != nil {
		if err := g.memEngine.Close(); err != nil {
			log.Printf("[gateway] close memory engine warning: %v", err)
		}
	}
	_ = g.channels.StopAll()
	if g.runtime != nil {
		g.runtime.Close()
	}
	log.Printf("[gateway] shutdown complete")
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
