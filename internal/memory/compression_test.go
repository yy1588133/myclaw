package memory

import (
	"path/filepath"
	"testing"
	"time"
)

type compressionMockLLM struct {
	compressFn      func(prompt, content string) (*CompressionResult, error)
	updateProfileFn func(currentProfile, newFacts string) (*ProfileResult, error)
}

func (m *compressionMockLLM) Extract(conversation string) (*ExtractionResult, error) {
	return &ExtractionResult{}, nil
}
func (m *compressionMockLLM) Compress(prompt, content string) (*CompressionResult, error) {
	if m.compressFn != nil {
		return m.compressFn(prompt, content)
	}
	return &CompressionResult{}, nil
}
func (m *compressionMockLLM) UpdateProfile(currentProfile, newFacts string) (*ProfileResult, error) {
	if m.updateProfileFn != nil {
		return m.updateProfileFn(currentProfile, newFacts)
	}
	return &ProfileResult{}, nil
}

func TestDailyCompress(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if err := e.WriteTier3(EventEntry{Date: yesterday, Channel: "telegram", Summary: "fixed deployment issue", Tokens: 120}); err != nil {
		t.Fatalf("WriteTier3 error: %v", err)
	}

	llm := &compressionMockLLM{compressFn: func(prompt, content string) (*CompressionResult, error) {
		return &CompressionResult{Facts: []FactEntry{{Content: "deployment issue fixed", Project: "myclaw", Topic: "deployment", Category: "solution", Importance: 0.8}}}, nil
	}}

	if err := e.DailyCompress(llm); err != nil {
		t.Fatalf("DailyCompress error: %v", err)
	}

	events, err := e.QueryEvents(yesterday, true)
	if err != nil {
		t.Fatalf("QueryEvents compressed error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected compressed event count=1, got %d", len(events))
	}
}

func TestWeeklyDeepCompress(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	for i := 0; i < 12; i++ {
		if err := e.WriteTier2(FactEntry{Content: "entry", Project: "myclaw", Topic: "architecture", Category: "decision", Importance: 0.8}); err != nil {
			t.Fatalf("WriteTier2 error: %v", err)
		}
	}

	llm := &compressionMockLLM{
		compressFn: func(prompt, content string) (*CompressionResult, error) {
			return &CompressionResult{Facts: []FactEntry{{Content: "merged architecture decision", Project: "myclaw", Topic: "architecture", Category: "decision", Importance: 0.9}}}, nil
		},
		updateProfileFn: func(currentProfile, newFacts string) (*ProfileResult, error) {
			return &ProfileResult{Entries: []ProfileEntry{{Content: "active project myclaw", Category: "identity"}}}, nil
		},
	}

	if err := e.WeeklyDeepCompress(llm); err != nil {
		t.Fatalf("WeeklyDeepCompress error: %v", err)
	}

	tier1, err := e.LoadTier1()
	if err != nil {
		t.Fatalf("LoadTier1 error: %v", err)
	}
	if tier1 == "" {
		t.Fatal("expected refreshed tier1 entries")
	}
}

func TestCleanupDecayed(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	if err := e.WriteTier2(FactEntry{Content: "temp debug note", Project: "myclaw", Topic: "debug", Category: "temp", Importance: 0.1}); err != nil {
		t.Fatalf("WriteTier2 error: %v", err)
	}
	if _, err := e.db.Exec(`UPDATE memories SET last_accessed = datetime('now', '-500 day') WHERE tier = 2`); err != nil {
		t.Fatalf("force old last_accessed: %v", err)
	}

	if err := e.cleanupDecayed(); err != nil {
		t.Fatalf("cleanupDecayed error: %v", err)
	}

	var active int
	if err := e.db.QueryRow(`SELECT COUNT(1) FROM memories WHERE tier = 2 AND is_archived = 0`).Scan(&active); err != nil {
		t.Fatalf("count active memories: %v", err)
	}
	if active != 0 {
		t.Fatalf("expected decayed entries archived, active=%d", active)
	}
}
