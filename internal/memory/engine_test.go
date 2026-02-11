package memory

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestNewEngine(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")

	e, err := NewEngine(dbPath)
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// Idempotent reopen against the same path.
	e2, err := NewEngine(dbPath)
	if err != nil {
		t.Fatalf("NewEngine reopen error: %v", err)
	}
	defer e2.Close()
}

func TestInitSchema(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	for _, table := range []string{"memories", "daily_events", "extraction_buffer", "memories_fts"} {
		if !schemaObjectExists(t, e, table, "table") {
			t.Fatalf("expected table %q to exist", table)
		}
	}

	for _, index := range []string{"idx_memories_partition", "idx_memories_category", "idx_memories_created", "idx_events_date", "idx_buffer_created"} {
		if !schemaObjectExists(t, e, index, "index") {
			t.Fatalf("expected index %q to exist", index)
		}
	}

	for _, trigger := range []string{"memories_ai", "memories_ad", "memories_au"} {
		if !schemaObjectExists(t, e, trigger, "trigger") {
			t.Fatalf("expected trigger %q to exist", trigger)
		}
	}
}

func schemaObjectExists(t *testing.T, e *Engine, name, typ string) bool {
	t.Helper()
	row := e.db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = ? AND name = ?`, typ, name)
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("scan sqlite_master: %v", err)
	}
	return count > 0
}

func TestEngineCRUDAndFTS(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	if err := e.WriteTier1(ProfileEntry{Content: "user likes Go", Category: "identity"}); err != nil {
		t.Fatalf("WriteTier1 error: %v", err)
	}
	t1, err := e.LoadTier1()
	if err != nil || t1 == "" {
		t.Fatalf("LoadTier1 error=%v tier1=%q", err, t1)
	}

	f := FactEntry{Content: "myclaw uses sqlite fts", Project: "myclaw", Topic: "memory", Category: "decision", Importance: 0.8}
	if err := e.WriteTier2(f); err != nil {
		t.Fatalf("WriteTier2 error: %v", err)
	}

	q, err := e.QueryTier2("myclaw", "memory", 10)
	if err != nil || len(q) != 1 {
		t.Fatalf("QueryTier2 err=%v len=%d", err, len(q))
	}

	fts, err := e.SearchFTS("sqlite", 10)
	if err != nil || len(fts) == 0 {
		t.Fatalf("SearchFTS err=%v len=%d", err, len(fts))
	}

	id := q[0].ID
	if err := e.TouchMemory(id); err != nil {
		t.Fatalf("TouchMemory error: %v", err)
	}
	q2, _ := e.QueryTier2("myclaw", "memory", 10)
	if q2[0].AccessCount < 1 {
		t.Fatalf("expected access count increment")
	}

	if err := e.ArchiveMemory(id); err != nil {
		t.Fatalf("ArchiveMemory error: %v", err)
	}
	q3, _ := e.QueryTier2("myclaw", "memory", 10)
	if len(q3) != 0 {
		t.Fatalf("expected archived memory hidden")
	}

	if err := e.WriteTier3(EventEntry{Date: "2026-02-11", Channel: "telegram", SenderID: "u1", Summary: "summary", Tokens: 10}); err != nil {
		t.Fatalf("WriteTier3 error: %v", err)
	}
	events, err := e.QueryEvents("2026-02-11", false)
	if err != nil || len(events) != 1 {
		t.Fatalf("QueryEvents err=%v len=%d", err, len(events))
	}
	if err := e.MarkEventsCompressed("2026-02-11"); err != nil {
		t.Fatalf("MarkEventsCompressed error: %v", err)
	}
	eventsCompressed, err := e.QueryEvents("2026-02-11", true)
	if err != nil || len(eventsCompressed) != 1 {
		t.Fatalf("QueryEvents compressed err=%v len=%d", err, len(eventsCompressed))
	}

	if err := e.WriteBuffer(BufferMessage{Channel: "telegram", SenderID: "u1", Role: "user", Content: "hello", TokenCount: 5}); err != nil {
		t.Fatalf("WriteBuffer error: %v", err)
	}
	tokens, err := e.BufferTokenCount()
	if err != nil || tokens != 5 {
		t.Fatalf("BufferTokenCount err=%v tokens=%d", err, tokens)
	}
	drained, err := e.DrainBuffer(10)
	if err != nil || len(drained) != 1 {
		t.Fatalf("DrainBuffer err=%v len=%d", err, len(drained))
	}
}

func TestEngineConcurrentWrites(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = e.WriteTier2(FactEntry{Content: "concurrent fact", Project: "myclaw", Topic: "concurrency", Category: "event", Importance: 0.5})
		}()
	}
	wg.Wait()

	q, err := e.QueryTier2("myclaw", "concurrency", 100)
	if err != nil {
		t.Fatalf("QueryTier2 error: %v", err)
	}
	if len(q) != 20 {
		t.Fatalf("expected 20 rows, got %d", len(q))
	}
}
