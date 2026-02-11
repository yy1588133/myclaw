package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

type Engine struct {
	db            *sql.DB
	mu            sync.Mutex
	knownProjects []string
	knownMu       sync.RWMutex
}

func NewEngine(dbPath string) (*Engine, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	e := &Engine{db: db}
	if err := e.configure(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := e.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return e, nil
}

func (e *Engine) configure() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := e.db.Exec(p); err != nil {
			return fmt.Errorf("sqlite pragma %q: %w", p, err)
		}
	}
	return nil
}

func (e *Engine) Close() error {
	if e.db == nil {
		return nil
	}
	return e.db.Close()
}

func (e *Engine) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS memories (
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_partition ON memories(tier, project, topic, is_archived)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(category, last_accessed)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
			content,
			content='memories',
			content_rowid='id',
			tokenize='unicode61'
		)`,
		`CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
			INSERT INTO memories_fts(rowid, content) VALUES (new.id, new.content);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content) VALUES('delete', old.id, old.content);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content) VALUES('delete', old.id, old.content);
			INSERT INTO memories_fts(rowid, content) VALUES (new.id, new.content);
		END`,
		`CREATE TABLE IF NOT EXISTS daily_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_date TEXT NOT NULL,
			channel TEXT NOT NULL DEFAULT 'unknown',
			sender_id TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL,
			raw_tokens INTEGER NOT NULL DEFAULT 0,
			is_compressed INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_date ON daily_events(event_date, is_compressed)`,
		`CREATE TABLE IF NOT EXISTS extraction_buffer (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel TEXT NOT NULL,
			sender_id TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			token_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_buffer_created ON extraction_buffer(created_at)`,
	}

	for _, stmt := range stmts {
		if _, err := e.db.Exec(stmt); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	return nil
}

func (e *Engine) LoadTier1() (string, error) {
	rows, err := e.db.Query(`
		SELECT content FROM memories
		WHERE tier = 1 AND is_archived = 0
		ORDER BY importance DESC, created_at ASC
		LIMIT 100
	`)
	if err != nil {
		return "", fmt.Errorf("load tier1: %w", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return "", fmt.Errorf("scan tier1: %w", err)
		}
		if trimmed := strings.TrimSpace(content); trimmed != "" {
			lines = append(lines, "- "+trimmed)
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate tier1: %w", err)
	}
	return strings.Join(lines, "\n"), nil
}

func (e *Engine) WriteTier1(entry ProfileEntry) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	category := strings.TrimSpace(entry.Category)
	if category == "" {
		category = "identity"
	}
	_, err := e.db.Exec(`
		INSERT INTO memories (tier, project, topic, category, content, importance, source)
		VALUES (1, '_global', '_profile', ?, ?, 1.0, 'manual')
	`, category, strings.TrimSpace(entry.Content))
	if err != nil {
		return fmt.Errorf("write tier1: %w", err)
	}
	return nil
}

func (e *Engine) WriteTier2(fact FactEntry) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	project := strings.TrimSpace(fact.Project)
	if project == "" {
		project = "_global"
	}
	topic := strings.TrimSpace(fact.Topic)
	if topic == "" {
		topic = "_general"
	}
	category := strings.TrimSpace(fact.Category)
	if category == "" {
		category = "event"
	}
	importance := fact.Importance
	if importance <= 0 {
		importance = 0.5
	}
	if importance > 1 {
		importance = 1
	}

	_, err := e.db.Exec(`
		INSERT INTO memories (tier, project, topic, category, content, importance, source)
		VALUES (2, ?, ?, ?, ?, ?, 'extraction')
	`, project, topic, category, strings.TrimSpace(fact.Content), importance)
	if err != nil {
		return fmt.Errorf("write tier2: %w", err)
	}
	return nil
}

func (e *Engine) QueryTier2(project, topic string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 20
	}
	q := `
		SELECT id, tier, project, topic, category, content, importance, source,
		       created_at, updated_at, last_accessed, access_count, is_archived
		FROM memories
		WHERE tier = 2 AND is_archived = 0
	`
	args := []any{}
	if p := strings.TrimSpace(project); p != "" {
		q += ` AND project = ?`
		args = append(args, p)
	}
	if t := strings.TrimSpace(topic); t != "" {
		q += ` AND topic = ?`
		args = append(args, t)
	}
	q += ` ORDER BY importance DESC, created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := e.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query tier2: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

func (e *Engine) SearchFTS(keywords string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	query := strings.TrimSpace(keywords)
	if query == "" {
		return nil, nil
	}

	rows, err := e.db.Query(`
		SELECT m.id, m.tier, m.project, m.topic, m.category, m.content, m.importance, m.source,
		       m.created_at, m.updated_at, m.last_accessed, m.access_count, m.is_archived
		FROM memories m
		JOIN memories_fts f ON m.id = f.rowid
		WHERE memories_fts MATCH ?
		  AND m.tier = 2
		  AND m.is_archived = 0
		ORDER BY bm25(memories_fts), m.importance DESC
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search fts: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

func (e *Engine) ArchiveMemory(id int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err := e.db.Exec(`UPDATE memories SET is_archived = 1, updated_at = datetime('now') WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("archive memory: %w", err)
	}
	return nil
}

func (e *Engine) TouchMemory(id int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err := e.db.Exec(`
		UPDATE memories
		SET last_accessed = datetime('now'), access_count = access_count + 1, updated_at = datetime('now')
		WHERE id = ?
	`, id)
	if err != nil {
		return fmt.Errorf("touch memory: %w", err)
	}
	return nil
}

func (e *Engine) WriteTier3(event EventEntry) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	channel := strings.TrimSpace(event.Channel)
	if channel == "" {
		channel = "unknown"
	}
	_, err := e.db.Exec(`
		INSERT INTO daily_events (event_date, channel, sender_id, summary, raw_tokens, is_compressed)
		VALUES (?, ?, ?, ?, ?, ?)
	`, strings.TrimSpace(event.Date), channel, strings.TrimSpace(event.SenderID), strings.TrimSpace(event.Summary), event.Tokens, boolToInt(event.IsCompressed))
	if err != nil {
		return fmt.Errorf("write tier3: %w", err)
	}
	return nil
}

func (e *Engine) QueryEvents(date string, compressed bool) ([]EventEntry, error) {
	rows, err := e.db.Query(`
		SELECT id, event_date, channel, sender_id, summary, raw_tokens, is_compressed, created_at
		FROM daily_events
		WHERE event_date = ? AND is_compressed = ?
		ORDER BY id ASC
	`, date, boolToInt(compressed))
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	result := make([]EventEntry, 0)
	for rows.Next() {
		var e2 EventEntry
		var compressedInt int
		if err := rows.Scan(&e2.ID, &e2.Date, &e2.Channel, &e2.SenderID, &e2.Summary, &e2.Tokens, &compressedInt, &e2.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e2.IsCompressed = compressedInt == 1
		result = append(result, e2)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return result, nil
}

func (e *Engine) MarkEventsCompressed(date string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err := e.db.Exec(`UPDATE daily_events SET is_compressed = 1 WHERE event_date = ?`, date)
	if err != nil {
		return fmt.Errorf("mark compressed: %w", err)
	}
	return nil
}

func (e *Engine) WriteBuffer(msg BufferMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err := e.db.Exec(`
		INSERT INTO extraction_buffer (channel, sender_id, role, content, token_count)
		VALUES (?, ?, ?, ?, ?)
	`, strings.TrimSpace(msg.Channel), strings.TrimSpace(msg.SenderID), strings.TrimSpace(msg.Role), strings.TrimSpace(msg.Content), msg.TokenCount)
	if err != nil {
		return fmt.Errorf("write buffer: %w", err)
	}
	return nil
}

func (e *Engine) DrainBuffer(limit int) ([]BufferMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if limit <= 0 {
		limit = 500
	}
	tx, err := e.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin drain: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT id, channel, sender_id, role, content, token_count, created_at
		FROM extraction_buffer
		ORDER BY id ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query drain buffer: %w", err)
	}
	defer rows.Close()

	msgs := make([]BufferMessage, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var msg BufferMessage
		if err := rows.Scan(&msg.ID, &msg.Channel, &msg.SenderID, &msg.Role, &msg.Content, &msg.TokenCount, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan drain buffer: %w", err)
		}
		msgs = append(msgs, msg)
		ids = append(ids, msg.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate drain buffer: %w", err)
	}

	if len(ids) > 0 {
		deleteSQL := `DELETE FROM extraction_buffer WHERE id IN (` + placeholders(len(ids)) + `)`
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		if _, err := tx.Exec(deleteSQL, args...); err != nil {
			return nil, fmt.Errorf("delete drained buffer: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit drain: %w", err)
	}
	return msgs, nil
}

func (e *Engine) BufferTokenCount() (int, error) {
	row := e.db.QueryRow(`SELECT COALESCE(SUM(token_count), 0) FROM extraction_buffer`)
	var total int
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("buffer token count: %w", err)
	}
	return total, nil
}

func (e *Engine) SetKnownProjects(projects []string) {
	e.knownMu.Lock()
	defer e.knownMu.Unlock()
	copyProjects := make([]string, 0, len(projects))
	for _, p := range projects {
		if p = strings.TrimSpace(p); p != "" {
			copyProjects = append(copyProjects, p)
		}
	}
	e.knownProjects = copyProjects
}

func (e *Engine) knownProjectsSnapshot() []string {
	e.knownMu.RLock()
	defer e.knownMu.RUnlock()
	out := make([]string, len(e.knownProjects))
	copy(out, e.knownProjects)
	return out
}

func scanMemories(rows *sql.Rows) ([]Memory, error) {
	result := make([]Memory, 0)
	for rows.Next() {
		var m Memory
		var archived int
		if err := rows.Scan(
			&m.ID,
			&m.Tier,
			&m.Project,
			&m.Topic,
			&m.Category,
			&m.Content,
			&m.Importance,
			&m.Source,
			&m.CreatedAt,
			&m.UpdatedAt,
			&m.LastAccessed,
			&m.AccessCount,
			&archived,
		); err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		m.IsArchived = archived == 1
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories: %w", err)
	}
	return result, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}
