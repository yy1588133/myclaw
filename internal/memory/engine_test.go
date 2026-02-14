package memory

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testLatestSchemaVersion = 1

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

	if version := schemaUserVersion(t, e); version != testLatestSchemaVersion {
		t.Fatalf("expected user_version=%d, got %d", testLatestSchemaVersion, version)
	}
}

func TestMigrateSchemaAddsEmbeddingColumns(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	if version := schemaUserVersion(t, e); version != testLatestSchemaVersion {
		t.Fatalf("expected user_version=%d, got %d", testLatestSchemaVersion, version)
	}

	columns := memoriesTableColumns(t, e)
	assertMemoriesColumn(t, columns, "embedding", "BLOB", false, nil)
	emptyDefault := "''"
	assertMemoriesColumn(t, columns, "embedding_model", "TEXT", true, &emptyDefault)
	zeroDefault := "0"
	assertMemoriesColumn(t, columns, "embedding_dim", "INTEGER", true, &zeroDefault)
	assertMemoriesColumn(t, columns, "embedding_updated_at", "TEXT", true, &emptyDefault)
}

func TestMigrateSchemaIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")

	e1, err := NewEngine(dbPath)
	if err != nil {
		t.Fatalf("NewEngine first open error: %v", err)
	}
	if err := e1.Close(); err != nil {
		t.Fatalf("Close first engine: %v", err)
	}

	e2, err := NewEngine(dbPath)
	if err != nil {
		t.Fatalf("NewEngine second open error: %v", err)
	}
	defer e2.Close()

	if version := schemaUserVersion(t, e2); version != testLatestSchemaVersion {
		t.Fatalf("expected user_version=%d, got %d", testLatestSchemaVersion, version)
	}

	columns := memoriesTableColumns(t, e2)
	for _, name := range []string{"embedding", "embedding_model", "embedding_dim", "embedding_updated_at"} {
		if count := countColumn(columns, name); count != 1 {
			t.Fatalf("expected column %q count=1, got %d", name, count)
		}
	}

	if err := e2.migrateSchema(); err != nil {
		t.Fatalf("migrateSchema should be idempotent: %v", err)
	}
}

func TestMigrateSchemaFromLegacyDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyMemoriesSchema(t, dbPath, 0)

	e, err := NewEngine(dbPath)
	if err != nil {
		t.Fatalf("NewEngine upgrade legacy db error: %v", err)
	}
	defer e.Close()

	if version := schemaUserVersion(t, e); version != testLatestSchemaVersion {
		t.Fatalf("expected upgraded user_version=%d, got %d", testLatestSchemaVersion, version)
	}

	columns := memoriesTableColumns(t, e)
	for _, name := range []string{"embedding", "embedding_model", "embedding_dim", "embedding_updated_at"} {
		if count := countColumn(columns, name); count != 1 {
			t.Fatalf("expected migrated column %q to exist once, got %d", name, count)
		}
	}
}

func TestMigrateSchemaRejectsInvalidState(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "corrupt.db")
	seedLegacyMemoriesSchema(t, dbPath, testLatestSchemaVersion)

	_, err := NewEngine(dbPath)
	if err == nil {
		t.Fatal("expected NewEngine to fail for invalid schema state")
	}
	if got := err.Error(); !containsAll(got, "migrate schema", "missing required columns") {
		t.Fatalf("expected migration validation error, got: %v", err)
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

type memoriesColumn struct {
	Name     string
	Type     string
	NotNull  bool
	Default  sql.NullString
	Position int
}

func schemaUserVersion(t *testing.T, e *Engine) int {
	t.Helper()
	row := e.db.QueryRow(`PRAGMA user_version`)
	var version int
	if err := row.Scan(&version); err != nil {
		t.Fatalf("scan schema version: %v", err)
	}
	return version
}

func memoriesTableColumns(t *testing.T, e *Engine) []memoriesColumn {
	t.Helper()
	rows, err := e.db.Query(`PRAGMA table_info(memories)`)
	if err != nil {
		t.Fatalf("query table_info(memories): %v", err)
	}
	defer rows.Close()

	columns := make([]memoriesColumn, 0)
	for rows.Next() {
		var (
			c       memoriesColumn
			notNull int
			pk      int
		)
		if err := rows.Scan(&c.Position, &c.Name, &c.Type, &notNull, &c.Default, &pk); err != nil {
			t.Fatalf("scan table_info(memories): %v", err)
		}
		c.NotNull = notNull == 1
		columns = append(columns, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info(memories): %v", err)
	}
	return columns
}

func assertMemoriesColumn(t *testing.T, columns []memoriesColumn, name, typ string, notNull bool, defaultValue *string) {
	t.Helper()
	for _, col := range columns {
		if col.Name != name {
			continue
		}
		if col.Type != typ {
			t.Fatalf("column %q type: expected %q, got %q", name, typ, col.Type)
		}
		if col.NotNull != notNull {
			t.Fatalf("column %q notnull: expected %v, got %v", name, notNull, col.NotNull)
		}
		if defaultValue == nil {
			if col.Default.Valid {
				t.Fatalf("column %q default: expected NULL, got %q", name, col.Default.String)
			}
			return
		}
		if !col.Default.Valid {
			t.Fatalf("column %q default: expected %q, got NULL", name, *defaultValue)
		}
		if col.Default.String != *defaultValue {
			t.Fatalf("column %q default: expected %q, got %q", name, *defaultValue, col.Default.String)
		}
		return
	}
	t.Fatalf("column %q not found", name)
}

func countColumn(columns []memoriesColumn, name string) int {
	count := 0
	for _, col := range columns {
		if col.Name == name {
			count++
		}
	}
	return count
}

func seedLegacyMemoriesSchema(t *testing.T, dbPath string, userVersion int) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite legacy db: %v", err)
	}
	defer db.Close()

	legacySchema := `CREATE TABLE memories (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tier INTEGER NOT NULL DEFAULT 2,
		project TEXT NOT NULL DEFAULT '_global',
		topic TEXT NOT NULL DEFAULT '_general',
		category TEXT NOT NULL DEFAULT 'event',
		content TEXT NOT NULL,
		importance REAL NOT NULL DEFAULT 0.5,
		source TEXT NOT NULL DEFAULT 'extraction',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now')),
		last_accessed TEXT NOT NULL DEFAULT (datetime('now')),
		access_count INTEGER NOT NULL DEFAULT 0,
		is_archived INTEGER NOT NULL DEFAULT 0
	)`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("create legacy memories table: %v", err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", userVersion)); err != nil {
		t.Fatalf("set legacy user_version: %v", err)
	}
}

func containsAll(s string, want ...string) bool {
	for _, w := range want {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
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

func TestEngineStateHelpers(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	empty, err := e.IsEmpty()
	if err != nil {
		t.Fatalf("IsEmpty error: %v", err)
	}
	if !empty {
		t.Fatal("new engine should be empty")
	}

	if err := e.WriteTier2(FactEntry{Content: "project fact", Project: "myclaw", Topic: "memory", Category: "event", Importance: 0.6}); err != nil {
		t.Fatalf("WriteTier2 error: %v", err)
	}

	empty, err = e.IsEmpty()
	if err != nil {
		t.Fatalf("IsEmpty error: %v", err)
	}
	if empty {
		t.Fatal("engine should not be empty after writes")
	}

	projects, err := e.LoadKnownProjects()
	if err != nil {
		t.Fatalf("LoadKnownProjects error: %v", err)
	}
	if len(projects) != 1 || projects[0] != "myclaw" {
		t.Fatalf("unexpected known projects: %+v", projects)
	}

	stats, err := e.Stats()
	if err != nil {
		t.Fatalf("Stats error: %v", err)
	}
	if stats.Tier2ActiveCount != 1 {
		t.Fatalf("expected tier2 active count=1, got %d", stats.Tier2ActiveCount)
	}
}
