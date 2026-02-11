package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewMemoryStore(t *testing.T) {
	ms := NewMemoryStore("/tmp/test-workspace")
	if ms == nil {
		t.Fatal("NewMemoryStore returned nil")
	}
	if ms.workspace != "/tmp/test-workspace" {
		t.Errorf("workspace = %q, want /tmp/test-workspace", ms.workspace)
	}
}

func TestLongTermMemory(t *testing.T) {
	tmpDir := t.TempDir()
	ms := NewMemoryStore(tmpDir)

	// Read when no file exists
	content, err := ms.ReadLongTerm()
	if err != nil {
		t.Fatalf("ReadLongTerm error: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty, got %q", content)
	}

	// Write
	if err := ms.WriteLongTerm("test memory content"); err != nil {
		t.Fatalf("WriteLongTerm error: %v", err)
	}

	// Read back
	content, err = ms.ReadLongTerm()
	if err != nil {
		t.Fatalf("ReadLongTerm error: %v", err)
	}
	if content != "test memory content" {
		t.Errorf("content = %q, want 'test memory content'", content)
	}
}

func TestDailyJournal(t *testing.T) {
	tmpDir := t.TempDir()
	ms := NewMemoryStore(tmpDir)

	// Read when no file exists
	content, err := ms.ReadToday()
	if err != nil {
		t.Fatalf("ReadToday error: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty, got %q", content)
	}

	// Append
	if err := ms.AppendToday("entry 1"); err != nil {
		t.Fatalf("AppendToday error: %v", err)
	}
	if err := ms.AppendToday("entry 2"); err != nil {
		t.Fatalf("AppendToday error: %v", err)
	}

	content, err = ms.ReadToday()
	if err != nil {
		t.Fatalf("ReadToday error: %v", err)
	}
	if !strings.Contains(content, "entry 1") || !strings.Contains(content, "entry 2") {
		t.Errorf("content missing entries: %q", content)
	}
}

func TestGetRecentMemories(t *testing.T) {
	tmpDir := t.TempDir()
	ms := NewMemoryStore(tmpDir)

	// Create memory dir and some date files
	memDir := filepath.Join(tmpDir, "memory")
	os.MkdirAll(memDir, 0755)

	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	os.WriteFile(filepath.Join(memDir, today+".md"), []byte("today's notes"), 0644)
	os.WriteFile(filepath.Join(memDir, yesterday+".md"), []byte("yesterday's notes"), 0644)

	result, err := ms.GetRecentMemories(7)
	if err != nil {
		t.Fatalf("GetRecentMemories error: %v", err)
	}
	if !strings.Contains(result, "today's notes") {
		t.Error("missing today's notes")
	}
	if !strings.Contains(result, "yesterday's notes") {
		t.Error("missing yesterday's notes")
	}
}

func TestGetRecentMemories_LimitDays(t *testing.T) {
	tmpDir := t.TempDir()
	ms := NewMemoryStore(tmpDir)

	memDir := filepath.Join(tmpDir, "memory")
	os.MkdirAll(memDir, 0755)

	// Create 3 days of files
	for i := 0; i < 3; i++ {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		os.WriteFile(filepath.Join(memDir, date+".md"), []byte("day "+date), 0644)
	}

	result, err := ms.GetRecentMemories(1)
	if err != nil {
		t.Fatalf("GetRecentMemories error: %v", err)
	}
	// Should only have 1 day
	sections := strings.Count(result, "## ")
	if sections != 1 {
		t.Errorf("expected 1 section, got %d", sections)
	}
}

func TestGetMemoryContext(t *testing.T) {
	tmpDir := t.TempDir()
	ms := NewMemoryStore(tmpDir)

	// Empty context when no memory exists
	ctx := ms.GetMemoryContext()
	if ctx != "" {
		t.Errorf("expected empty context, got %q", ctx)
	}

	// Write long-term memory
	ms.WriteLongTerm("important fact")

	ctx = ms.GetMemoryContext()
	if !strings.Contains(ctx, "Long-term Memory") {
		t.Error("missing Long-term Memory header")
	}
	if !strings.Contains(ctx, "important fact") {
		t.Error("missing memory content")
	}
}

func TestGetMemoryContext_WithRecentMemories(t *testing.T) {
	tmpDir := t.TempDir()
	ms := NewMemoryStore(tmpDir)

	// Write long-term memory
	ms.WriteLongTerm("long-term fact")

	// Write today's journal
	ms.AppendToday("today's entry")

	ctx := ms.GetMemoryContext()
	if !strings.Contains(ctx, "Long-term Memory") {
		t.Error("missing Long-term Memory header")
	}
	if !strings.Contains(ctx, "Recent Journal") {
		t.Error("missing Recent Journal header")
	}
	if !strings.Contains(ctx, "long-term fact") {
		t.Error("missing long-term content")
	}
	if !strings.Contains(ctx, "today's entry") {
		t.Error("missing today's entry")
	}
}

func TestGetRecentMemories_EmptyFiles(t *testing.T) {
	tmpDir := t.TempDir()
	ms := NewMemoryStore(tmpDir)

	memDir := filepath.Join(tmpDir, "memory")
	os.MkdirAll(memDir, 0755)

	// Create an empty date file
	today := time.Now().Format("2006-01-02")
	os.WriteFile(filepath.Join(memDir, today+".md"), []byte("   \n\n  "), 0644)

	result, err := ms.GetRecentMemories(7)
	if err != nil {
		t.Fatalf("GetRecentMemories error: %v", err)
	}
	// Empty/whitespace-only files should be skipped
	if strings.Contains(result, "## ") {
		t.Error("empty file should not produce a section")
	}
}

func TestGetRecentMemories_NoLimit(t *testing.T) {
	tmpDir := t.TempDir()
	ms := NewMemoryStore(tmpDir)

	memDir := filepath.Join(tmpDir, "memory")
	os.MkdirAll(memDir, 0755)

	// Create 5 days of files
	for i := 0; i < 5; i++ {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		os.WriteFile(filepath.Join(memDir, date+".md"), []byte("content "+date), 0644)
	}

	// days=0 means no limit
	result, err := ms.GetRecentMemories(0)
	if err != nil {
		t.Fatalf("GetRecentMemories error: %v", err)
	}
	sections := strings.Count(result, "## ")
	if sections != 5 {
		t.Errorf("expected 5 sections, got %d", sections)
	}
}

func TestMemoryDir(t *testing.T) {
	workspace := filepath.FromSlash("/test/workspace")
	ms := NewMemoryStore(workspace)
	expected := filepath.Join(workspace, "memory")
	if ms.memoryDir() != expected {
		t.Errorf("memoryDir = %q, want %q", ms.memoryDir(), expected)
	}
}

func TestTodayFile(t *testing.T) {
	workspace := filepath.FromSlash("/test/workspace")
	ms := NewMemoryStore(workspace)
	today := time.Now().Format("2006-01-02")
	expected := filepath.Join(workspace, "memory", today+".md")
	if ms.todayFile() != expected {
		t.Errorf("todayFile = %q, want %q", ms.todayFile(), expected)
	}
}
