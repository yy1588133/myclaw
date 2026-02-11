package memory

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stellarlinkco/myclaw/internal/config"
)

type mockLLM struct {
	extractFn func(conversation string) (*ExtractionResult, error)
}

func (m *mockLLM) Extract(conversation string) (*ExtractionResult, error) {
	if m.extractFn != nil {
		return m.extractFn(conversation)
	}
	return &ExtractionResult{}, nil
}
func (m *mockLLM) Compress(prompt, content string) (*CompressionResult, error) {
	return &CompressionResult{}, nil
}
func (m *mockLLM) UpdateProfile(currentProfile, newFacts string) (*ProfileResult, error) {
	return &ProfileResult{}, nil
}

func TestBufferMessage(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	svc := NewExtractionService(e, &mockLLM{}, config.ExtractionConfig{QuietGap: "1h", TokenBudget: 0.9, DailyFlush: "03:00"})
	svc.BufferMessage("telegram", "u1", "user", "hello world")

	count, err := e.BufferTokenCount()
	if err != nil {
		t.Fatalf("BufferTokenCount error: %v", err)
	}
	if count <= 0 {
		t.Fatal("expected buffered tokens > 0")
	}
}

func TestFlushOnTokenBudget(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	svc := NewExtractionService(e, &mockLLM{extractFn: func(conversation string) (*ExtractionResult, error) {
		return &ExtractionResult{Facts: []FactEntry{{Content: "fact", Project: "myclaw", Topic: "test", Category: "event", Importance: 0.5}}, Summary: "summary"}, nil
	}}, config.ExtractionConfig{QuietGap: "1h", TokenBudget: 0.1, DailyFlush: "03:00"})
	svc.tokenCap = 1

	svc.BufferMessage("telegram", "u1", "user", "这是一条比较长的消息用于触发token预算")
	time.Sleep(100 * time.Millisecond)

	mems, err := e.QueryTier2("myclaw", "test", 10)
	if err != nil {
		t.Fatalf("QueryTier2 error: %v", err)
	}
	if len(mems) == 0 {
		t.Fatal("expected extracted fact written to tier2")
	}
}

func TestFlushOnQuietGap(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	svc := NewExtractionService(e, &mockLLM{extractFn: func(conversation string) (*ExtractionResult, error) {
		return &ExtractionResult{Facts: []FactEntry{{Content: "qfact", Project: "myclaw", Topic: "quiet", Category: "event", Importance: 0.5}}, Summary: "qsummary"}, nil
	}}, config.ExtractionConfig{QuietGap: "50ms", TokenBudget: 0.9, DailyFlush: "03:00"})

	svc.BufferMessage("telegram", "u1", "user", "quiet message")
	time.Sleep(200 * time.Millisecond)

	mems, err := e.QueryTier2("myclaw", "quiet", 10)
	if err != nil {
		t.Fatalf("QueryTier2 error: %v", err)
	}
	if len(mems) == 0 {
		t.Fatal("expected quiet-gap flush to persist fact")
	}
}

func TestExtractionResultParsing(t *testing.T) {
	if estimateTokens("中文测试") <= 0 {
		t.Fatal("estimateTokens should be positive")
	}
	if estimateTokens("hello world") <= 0 {
		t.Fatal("estimateTokens should be positive")
	}
}
