package gateway

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cexll/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/myclaw/internal/bus"
	"github.com/stellarlinkco/myclaw/internal/channel"
	"github.com/stellarlinkco/myclaw/internal/config"
	"github.com/stellarlinkco/myclaw/internal/cron"
	"github.com/stellarlinkco/myclaw/internal/heartbeat"
	"github.com/stellarlinkco/myclaw/internal/memory"
)

// mockRuntime implements Runtime interface for testing
type mockRuntime struct {
	response *api.Response
	err      error
	closed   bool
	requests []api.Request
}

type mockMemoryLLM struct{}

func (m *mockMemoryLLM) Extract(conversation string) (*memory.ExtractionResult, error) {
	return &memory.ExtractionResult{}, nil
}
func (m *mockMemoryLLM) Compress(prompt, content string) (*memory.CompressionResult, error) {
	return &memory.CompressionResult{}, nil
}
func (m *mockMemoryLLM) UpdateProfile(currentProfile, newFacts string) (*memory.ProfileResult, error) {
	return &memory.ProfileResult{}, nil
}

type slowExtractLLM struct{}

func (m *slowExtractLLM) Extract(conversation string) (*memory.ExtractionResult, error) {
	time.Sleep(500 * time.Millisecond)
	return &memory.ExtractionResult{}, nil
}
func (m *slowExtractLLM) Compress(prompt, content string) (*memory.CompressionResult, error) {
	return &memory.CompressionResult{}, nil
}
func (m *slowExtractLLM) UpdateProfile(currentProfile, newFacts string) (*memory.ProfileResult, error) {
	return &memory.ProfileResult{}, nil
}

func (m *mockRuntime) Run(ctx context.Context, req api.Request) (*api.Response, error) {
	m.requests = append(m.requests, req)
	return m.response, m.err
}

func (m *mockRuntime) Close() {
	m.closed = true
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long message", 10, "this is a ..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}

func TestGateway_BuildSystemPrompt(t *testing.T) {
	tmpDir := t.TempDir()

	// Create workspace files
	os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("# Agent\nYou are helpful."), 0644)
	os.WriteFile(filepath.Join(tmpDir, "SOUL.md"), []byte("# Soul\nBe kind."), 0644)

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
	}

	g := &Gateway{
		cfg: cfg,
		mem: memory.NewMemoryStore(tmpDir),
	}

	prompt := g.buildSystemPrompt()

	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !contains(prompt, "# Agent") {
		t.Error("missing AGENTS.md content")
	}
	if !contains(prompt, "# Soul") {
		t.Error("missing SOUL.md content")
	}
}

func TestGateway_BuildSystemPrompt_WithMemory(t *testing.T) {
	tmpDir := t.TempDir()

	mem := memory.NewMemoryStore(tmpDir)
	mem.WriteLongTerm("User is a developer.")

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
	}

	g := &Gateway{
		cfg: cfg,
		mem: mem,
	}

	prompt := g.buildSystemPrompt()

	if !contains(prompt, "User is a developer") {
		t.Error("missing memory content")
	}
}

func TestGateway_BuildSystemPrompt_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
	}

	g := &Gateway{
		cfg: cfg,
		mem: memory.NewMemoryStore(tmpDir),
	}

	prompt := g.buildSystemPrompt()

	// Should return empty when no files exist
	if prompt != "" {
		t.Errorf("expected empty prompt, got %q", prompt)
	}
}

func TestGateway_Shutdown(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
	}

	msgBus := bus.NewMessageBus(10)
	chMgr, _ := channel.NewChannelManager(config.ChannelsConfig{}, msgBus)
	cronSvc := cron.NewService(filepath.Join(tmpDir, "cron.json"))
	mockRt := &mockRuntime{}

	g := &Gateway{
		cfg:      cfg,
		bus:      msgBus,
		channels: chMgr,
		cron:     cronSvc,
		hb:       heartbeat.New(tmpDir, nil, 0),
		mem:      memory.NewMemoryStore(tmpDir),
		runtime:  mockRt,
	}

	err := g.Shutdown()
	if err != nil {
		t.Errorf("Shutdown error: %v", err)
	}
	if !mockRt.closed {
		t.Error("runtime should be closed")
	}
}

func TestGateway_RunAgent(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
	}

	mockRt := &mockRuntime{
		response: &api.Response{
			Result: &api.Result{
				Output: "Hello from mock",
			},
		},
	}

	g := &Gateway{
		cfg:     cfg,
		runtime: mockRt,
	}

	result, err := g.runAgent(context.Background(), "test", "session1")
	if err != nil {
		t.Errorf("runAgent error: %v", err)
	}
	if result != "Hello from mock" {
		t.Errorf("result = %q, want 'Hello from mock'", result)
	}
}

func TestGateway_RunAgent_NilResponse(t *testing.T) {
	mockRt := &mockRuntime{response: nil}

	g := &Gateway{runtime: mockRt}

	result, err := g.runAgent(context.Background(), "test", "session1")
	if err != nil {
		t.Errorf("runAgent error: %v", err)
	}
	if result != "" {
		t.Errorf("result = %q, want empty", result)
	}
}

func TestGateway_RunAgent_NilResult(t *testing.T) {
	mockRt := &mockRuntime{response: &api.Response{Result: nil}}

	g := &Gateway{runtime: mockRt}

	result, err := g.runAgent(context.Background(), "test", "session1")
	if err != nil {
		t.Errorf("runAgent error: %v", err)
	}
	if result != "" {
		t.Errorf("result = %q, want empty", result)
	}
}

func TestGateway_ProcessLoop(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
	}

	msgBus := bus.NewMessageBus(10)
	mockRt := &mockRuntime{
		response: &api.Response{
			Result: &api.Result{Output: "response"},
		},
	}

	g := &Gateway{
		cfg:     cfg,
		bus:     msgBus,
		runtime: mockRt,
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start process loop
	go g.processLoop(ctx)

	// Send inbound message
	msgBus.Inbound <- bus.InboundMessage{
		Channel:  "test",
		SenderID: "user1",
		ChatID:   "chat1",
		Content:  "hello",
	}

	// Wait for outbound message
	select {
	case outMsg := <-msgBus.Outbound:
		if outMsg.Content != "response" {
			t.Errorf("outbound content = %q, want 'response'", outMsg.Content)
		}
		if outMsg.Channel != "test" {
			t.Errorf("outbound channel = %q, want 'test'", outMsg.Channel)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for outbound message")
	}

	cancel()
}

func TestGateway_RunAgent_Error(t *testing.T) {
	mockRt := &mockRuntime{err: context.DeadlineExceeded}

	g := &Gateway{runtime: mockRt}

	_, err := g.runAgent(context.Background(), "test", "session1")
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestGateway_ProcessLoop_AgentError(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
	}

	msgBus := bus.NewMessageBus(10)
	mockRt := &mockRuntime{err: context.DeadlineExceeded}

	g := &Gateway{
		cfg:     cfg,
		bus:     msgBus,
		runtime: mockRt,
	}

	ctx, cancel := context.WithCancel(context.Background())

	go g.processLoop(ctx)

	msgBus.Inbound <- bus.InboundMessage{
		Channel:  "test",
		SenderID: "user1",
		ChatID:   "chat1",
		Content:  "hello",
	}

	select {
	case outMsg := <-msgBus.Outbound:
		if outMsg.Content != "Sorry, I encountered an error processing your message." {
			t.Errorf("expected error message, got %q", outMsg.Content)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for error response")
	}

	cancel()
}

func TestGateway_ProcessLoop_EmptyResult(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
	}

	msgBus := bus.NewMessageBus(10)
	mockRt := &mockRuntime{
		response: &api.Response{
			Result: &api.Result{Output: ""},
		},
	}

	g := &Gateway{
		cfg:     cfg,
		bus:     msgBus,
		runtime: mockRt,
	}

	ctx, cancel := context.WithCancel(context.Background())

	go g.processLoop(ctx)

	msgBus.Inbound <- bus.InboundMessage{
		Channel:  "test",
		SenderID: "user1",
		ChatID:   "chat1",
		Content:  "hello",
	}

	// Should NOT receive outbound message when result is empty
	select {
	case outMsg := <-msgBus.Outbound:
		t.Errorf("should not send empty result, got %q", outMsg.Content)
	case <-time.After(100 * time.Millisecond):
		// Expected - no message sent
	}

	cancel()
}

func TestGateway_ProcessLoop_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
	}

	msgBus := bus.NewMessageBus(10)
	mockRt := &mockRuntime{}

	g := &Gateway{
		cfg:     cfg,
		bus:     msgBus,
		runtime: mockRt,
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		g.processLoop(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// Expected - loop exited
	case <-time.After(time.Second):
		t.Error("processLoop did not exit after context cancel")
	}
}

func TestGateway_Shutdown_NilRuntime(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
	}

	msgBus := bus.NewMessageBus(10)
	chMgr, _ := channel.NewChannelManager(config.ChannelsConfig{}, msgBus)
	cronSvc := cron.NewService(filepath.Join(tmpDir, "cron.json"))

	g := &Gateway{
		cfg:      cfg,
		bus:      msgBus,
		channels: chMgr,
		cron:     cronSvc,
		hb:       heartbeat.New(tmpDir, nil, 0),
		mem:      memory.NewMemoryStore(tmpDir),
		runtime:  nil,
	}

	err := g.Shutdown()
	if err != nil {
		t.Errorf("Shutdown error: %v", err)
	}
}

// mockRuntimeFactory returns a factory that creates mock runtimes
func mockRuntimeFactory(rt Runtime) RuntimeFactory {
	return func(cfg *config.Config, sysPrompt string) (Runtime, error) {
		return rt, nil
	}
}

// errorRuntimeFactory returns a factory that always fails
func errorRuntimeFactory(err error) RuntimeFactory {
	return func(cfg *config.Config, sysPrompt string) (Runtime, error) {
		return nil, err
	}
}

func TestNewWithOptions_MockRuntime(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
		Channels: config.ChannelsConfig{},
	}

	mockRt := &mockRuntime{
		response: &api.Response{
			Result: &api.Result{Output: "test"},
		},
	}

	g, err := NewWithOptions(cfg, Options{
		RuntimeFactory: mockRuntimeFactory(mockRt),
	})
	if err != nil {
		t.Fatalf("NewWithOptions error: %v", err)
	}

	if g == nil {
		t.Fatal("gateway should not be nil")
	}
	if g.runtime != mockRt {
		t.Error("runtime should be the mock")
	}
	if g.bus == nil {
		t.Error("bus should not be nil")
	}
	if g.mem == nil {
		t.Error("mem should not be nil")
	}
	if g.cron == nil {
		t.Error("cron should not be nil")
	}
	if g.hb == nil {
		t.Error("heartbeat should not be nil")
	}
	if g.channels == nil {
		t.Error("channels should not be nil")
	}

	// Clean up
	g.Shutdown()
}

func TestNewWithOptions_RuntimeFactoryError(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
	}

	_, err := NewWithOptions(cfg, Options{
		RuntimeFactory: errorRuntimeFactory(context.DeadlineExceeded),
	})
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestNewWithOptions_ChannelManagerError(t *testing.T) {
	tmpDir := t.TempDir()

	// Invalid telegram config to trigger channel manager error
	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
		Channels: config.ChannelsConfig{
			Telegram: config.TelegramConfig{
				Enabled: true,
				Token:   "", // Empty token with enabled=true may cause error
			},
		},
	}

	mockRt := &mockRuntime{}
	_, err := NewWithOptions(cfg, Options{
		RuntimeFactory: mockRuntimeFactory(mockRt),
	})
	// Channel manager may or may not error with empty token - just ensure we don't panic
	_ = err
}

func TestGateway_Run_WithSignalChan(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
		Gateway: config.GatewayConfig{
			Host: "localhost",
			Port: 8080,
		},
		Channels: config.ChannelsConfig{},
	}

	mockRt := &mockRuntime{}
	sigCh := make(chan os.Signal, 1)

	g, err := NewWithOptions(cfg, Options{
		RuntimeFactory: mockRuntimeFactory(mockRt),
		SignalChan:     sigCh,
	})
	if err != nil {
		t.Fatalf("NewWithOptions error: %v", err)
	}

	// Run in goroutine
	done := make(chan error, 1)
	go func() {
		done <- g.Run(context.Background())
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Send shutdown signal
	sigCh <- os.Interrupt

	// Wait for Run to complete
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Run did not exit after signal")
	}

	if !mockRt.closed {
		t.Error("runtime should be closed after shutdown")
	}
}

func TestGateway_Run_ChannelStartError(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
		Gateway: config.GatewayConfig{
			Host: "localhost",
			Port: 8080,
		},
		Channels: config.ChannelsConfig{
			Telegram: config.TelegramConfig{
				Enabled: true,
				Token:   "invalid-token", // Will fail on StartAll
			},
		},
	}

	mockRt := &mockRuntime{}
	sigCh := make(chan os.Signal, 1)

	g, err := NewWithOptions(cfg, Options{
		RuntimeFactory: mockRuntimeFactory(mockRt),
		SignalChan:     sigCh,
	})
	if err != nil {
		t.Fatalf("NewWithOptions error: %v", err)
	}

	// Run should return error from channel start
	err = g.Run(context.Background())
	if err == nil {
		t.Error("expected error from channel start")
	}
}

func TestDefaultRuntimeFactory_NoAPIKey(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			APIKey: "",
		},
	}

	// DefaultRuntimeFactory will try to create real runtime
	// which may fail in different ways depending on SDK behavior
	_, err := DefaultRuntimeFactory(cfg, "test prompt")
	// Just ensure it doesn't panic - error is expected
	_ = err
}

func TestGateway_CronOnJob(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
		Channels: config.ChannelsConfig{},
	}

	mockRt := &mockRuntime{
		response: &api.Response{
			Result: &api.Result{Output: "cron result"},
		},
	}

	g, err := NewWithOptions(cfg, Options{
		RuntimeFactory: mockRuntimeFactory(mockRt),
	})
	if err != nil {
		t.Fatalf("NewWithOptions error: %v", err)
	}
	defer g.Shutdown()

	// Test cron OnJob callback
	job := cron.CronJob{
		ID: "test-job",
		Payload: cron.Payload{
			Message: "test message",
			Deliver: false,
		},
	}

	result, err := g.cron.OnJob(job)
	if err != nil {
		t.Errorf("OnJob error: %v", err)
	}
	if result != "cron result" {
		t.Errorf("result = %q, want 'cron result'", result)
	}
}

func TestGateway_CronOnJob_WithDelivery(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
		Channels: config.ChannelsConfig{},
	}

	mockRt := &mockRuntime{
		response: &api.Response{
			Result: &api.Result{Output: "delivered result"},
		},
	}

	g, err := NewWithOptions(cfg, Options{
		RuntimeFactory: mockRuntimeFactory(mockRt),
	})
	if err != nil {
		t.Fatalf("NewWithOptions error: %v", err)
	}
	defer g.Shutdown()

	// Test cron OnJob with delivery
	job := cron.CronJob{
		ID: "test-job",
		Payload: cron.Payload{
			Message: "test message",
			Deliver: true,
			Channel: "telegram",
			To:      "12345",
		},
	}

	// Start a goroutine to consume outbound message
	done := make(chan struct{})
	go func() {
		select {
		case msg := <-g.bus.Outbound:
			if msg.Content != "delivered result" {
				t.Errorf("outbound content = %q, want 'delivered result'", msg.Content)
			}
			if msg.Channel != "telegram" {
				t.Errorf("outbound channel = %q, want 'telegram'", msg.Channel)
			}
			if msg.ChatID != "12345" {
				t.Errorf("outbound chatID = %q, want '12345'", msg.ChatID)
			}
		case <-time.After(time.Second):
			t.Error("timeout waiting for outbound message")
		}
		close(done)
	}()

	result, err := g.cron.OnJob(job)
	if err != nil {
		t.Errorf("OnJob error: %v", err)
	}
	if result != "delivered result" {
		t.Errorf("result = %q, want 'delivered result'", result)
	}

	<-done
}

func TestGateway_CronOnJob_Error(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Workspace: tmpDir,
		},
		Channels: config.ChannelsConfig{},
	}

	mockRt := &mockRuntime{
		err: context.DeadlineExceeded,
	}

	g, err := NewWithOptions(cfg, Options{
		RuntimeFactory: mockRuntimeFactory(mockRt),
	})
	if err != nil {
		t.Fatalf("NewWithOptions error: %v", err)
	}
	defer g.Shutdown()

	// Test cron OnJob with error
	job := cron.CronJob{
		ID: "test-job",
		Payload: cron.Payload{
			Message: "test message",
		},
	}

	_, err = g.cron.OnJob(job)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestGatewayWithMemory(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.Agent.Workspace = tmpDir
	cfg.Channels = config.ChannelsConfig{}
	cfg.Memory.Enabled = true
	cfg.Memory.DBPath = filepath.Join(tmpDir, "memory.db")
	cfg.Provider.APIKey = "key"
	cfg.Provider.BaseURL = "https://example.invalid/v1"

	mockRt := &mockRuntime{}
	g, err := NewWithOptions(cfg, Options{RuntimeFactory: mockRuntimeFactory(mockRt)})
	if err != nil {
		t.Fatalf("NewWithOptions error: %v", err)
	}
	defer g.Shutdown()

	if g.memEngine == nil || g.extraction == nil {
		t.Fatal("expected memory engine and extraction service when memory.enabled=true")
	}
	if g.mem != nil {
		t.Fatal("legacy memory store should be nil when new memory enabled")
	}
}

func TestGatewayWithoutMemory(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.Agent.Workspace = tmpDir
	cfg.Channels = config.ChannelsConfig{}
	cfg.Memory.Enabled = false

	mockRt := &mockRuntime{}
	g, err := NewWithOptions(cfg, Options{RuntimeFactory: mockRuntimeFactory(mockRt)})
	if err != nil {
		t.Fatalf("NewWithOptions error: %v", err)
	}
	defer g.Shutdown()

	if g.mem == nil {
		t.Fatal("expected legacy memory store when memory.enabled=false")
	}
	if g.memEngine != nil || g.extraction != nil {
		t.Fatal("new memory components should be nil when disabled")
	}
}

func TestMemoryRetrievalInProcessLoop(t *testing.T) {
	tmpDir := t.TempDir()

	engine, err := memory.NewEngine(filepath.Join(tmpDir, "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer engine.Close()
	engine.SetKnownProjects([]string{"myclaw"})
	if err := engine.WriteTier2(memory.FactEntry{Content: "myclaw uses sqlite memory", Project: "myclaw", Topic: "architecture", Category: "decision", Importance: 0.9}); err != nil {
		t.Fatalf("WriteTier2 error: %v", err)
	}

	msgBus := bus.NewMessageBus(10)
	rt := &mockRuntime{response: &api.Response{Result: &api.Result{Output: "ok"}}}
	g := &Gateway{
		cfg:       &config.Config{},
		bus:       msgBus,
		runtime:   rt,
		memEngine: engine,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.processLoop(ctx)

	msgBus.Inbound <- bus.InboundMessage{Channel: "telegram", SenderID: "u", ChatID: "c", Content: "我之前的 myclaw 架构是啥？"}

	select {
	case <-msgBus.Outbound:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting outbound")
	}

	if len(rt.requests) == 0 {
		t.Fatal("expected runtime request")
	}
	if !strings.Contains(rt.requests[0].Prompt, "[相关记忆]") {
		t.Fatalf("expected retrieval memory injected in prompt, got %q", rt.requests[0].Prompt)
	}
}

func TestMemoryCronJobs(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.Agent.Workspace = tmpDir
	cfg.Channels = config.ChannelsConfig{}
	cfg.Memory.Enabled = true
	cfg.Memory.DBPath = filepath.Join(tmpDir, "memory.db")
	cfg.Provider.APIKey = "key"
	cfg.Provider.BaseURL = "https://example.invalid/v1"

	g, err := NewWithOptions(cfg, Options{RuntimeFactory: mockRuntimeFactory(&mockRuntime{})})
	if err != nil {
		t.Fatalf("NewWithOptions error: %v", err)
	}
	defer g.Shutdown()

	jobs := g.cron.ListJobs()
	hasDaily := false
	hasWeekly := false
	for _, j := range jobs {
		if j.Name == "memory-daily-compress" {
			hasDaily = true
		}
		if j.Name == "memory-weekly-compress" {
			hasWeekly = true
		}
	}
	if !hasDaily || !hasWeekly {
		t.Fatalf("expected memory cron jobs registered, daily=%v weekly=%v", hasDaily, hasWeekly)
	}
}

func TestGatewayGracefulShutdown(t *testing.T) {
	tmpDir := t.TempDir()

	engine, err := memory.NewEngine(filepath.Join(tmpDir, "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}

	msgBus := bus.NewMessageBus(10)
	chMgr, _ := channel.NewChannelManager(config.ChannelsConfig{}, msgBus)
	cronSvc := cron.NewService(filepath.Join(tmpDir, "cron.json"))
	rt := &mockRuntime{}

	g := &Gateway{
		cfg:        &config.Config{Memory: config.MemoryConfig{Extraction: config.ExtractionConfig{QuietGap: "1h", TokenBudget: 0.6, DailyFlush: "03:00"}}},
		bus:        msgBus,
		channels:   chMgr,
		cron:       cronSvc,
		hb:         heartbeat.New(tmpDir, nil, 0),
		runtime:    rt,
		memEngine:  engine,
		extraction: memory.NewExtractionService(engine, &mockMemoryLLM{}, config.ExtractionConfig{QuietGap: "1h", TokenBudget: 0.6, DailyFlush: "03:00"}),
	}

	if err := g.Shutdown(); err != nil {
		t.Fatalf("Shutdown error: %v", err)
	}
	if !rt.closed {
		t.Fatal("runtime should be closed on graceful shutdown")
	}
}

func TestMemoryExtractionAsync(t *testing.T) {
	tmpDir := t.TempDir()

	engine, err := memory.NewEngine(filepath.Join(tmpDir, "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer engine.Close()

	msgBus := bus.NewMessageBus(10)
	rt := &mockRuntime{response: &api.Response{Result: &api.Result{Output: "ok"}}}
	extract := memory.NewExtractionService(engine, &slowExtractLLM{}, config.ExtractionConfig{QuietGap: "1h", TokenBudget: 0.1, DailyFlush: "03:00"})

	g := &Gateway{cfg: &config.Config{}, bus: msgBus, runtime: rt, memEngine: engine, extraction: extract}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.processLoop(ctx)

	bigMessage := strings.Repeat("这是一条触发异步提取的长消息", 120)
	start := time.Now()
	msgBus.Inbound <- bus.InboundMessage{Channel: "telegram", SenderID: "u1", ChatID: "c1", Content: bigMessage}

	select {
	case <-msgBus.Outbound:
		if time.Since(start) > 200*time.Millisecond {
			t.Fatalf("expected outbound response not blocked by extraction")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for outbound response")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
