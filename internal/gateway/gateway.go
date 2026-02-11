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
	cfg        *config.Config
	bus        *bus.MessageBus
	runtime    Runtime
	channels   *channel.ChannelManager
	cron       *cron.Service
	hb         *heartbeat.Service
	mem        *memory.MemoryStore
	skillRegs  []api.SkillRegistration
	signalChan chan os.Signal // for testing
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

	// Memory
	g.mem = memory.NewMemoryStore(cfg.Agent.Workspace)

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
	var (
		rt  Runtime
		err error
	)
	if factory == nil {
		rt, err = newRuntime(cfg, sysPrompt, g.skillRegs)
	} else {
		rt, err = factory(cfg, sysPrompt)
	}
	if err != nil {
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

	if memCtx := g.mem.GetMemoryContext(); memCtx != "" {
		sb.WriteString(memCtx)
	}

	return sb.String()
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

			result, err := g.runAgent(ctx, msg.Content, msg.SessionKey(), msg.ContentBlocks)
			if err != nil {
				log.Printf("[gateway] agent error: %v", err)
				result = "Sorry, I encountered an error processing your message."
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

func (g *Gateway) Shutdown() error {
	g.cron.Stop()
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
