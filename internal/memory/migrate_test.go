package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateFromFiles(t *testing.T) {
	workspace := t.TempDir()
	memDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("# long-term\nuser prefers Go\n"), 0644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "2026-02-10.md"), []byte("fixed gateway bug"), 0644); err != nil {
		t.Fatalf("write daily file: %v", err)
	}

	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	if err := MigrateFromFiles(workspace, e); err != nil {
		t.Fatalf("MigrateFromFiles error: %v", err)
	}

	tier1, err := e.LoadTier1()
	if err != nil {
		t.Fatalf("LoadTier1 error: %v", err)
	}
	if tier1 == "" {
		t.Fatal("expected migrated tier1 data")
	}

	events, err := e.QueryEvents("2026-02-10", false)
	if err != nil {
		t.Fatalf("QueryEvents error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 migrated event, got %d", len(events))
	}

	if err := MigrateFromFiles(workspace, e); err != nil {
		t.Fatalf("second MigrateFromFiles error: %v", err)
	}
	events2, err := e.QueryEvents("2026-02-10", false)
	if err != nil {
		t.Fatalf("QueryEvents after second migrate error: %v", err)
	}
	if len(events2) != 1 {
		t.Fatalf("expected idempotent migration events=1, got %d", len(events2))
	}
}
