package memory

import (
	"fmt"
	"log"
	"strings"
	"time"
)

func (e *Engine) DailyCompress(llm LLMClient) error {
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	events, err := e.QueryEvents(yesterday, false)
	if err != nil {
		return fmt.Errorf("daily compress query events: %w", err)
	}
	if len(events) == 0 {
		return nil
	}

	content := joinEventSummaries(events)
	if strings.TrimSpace(content) == "" {
		return e.MarkEventsCompressed(yesterday)
	}

	result, err := llm.Compress(dailyCompressPrompt, content)
	if err != nil {
		log.Printf("[memory] daily compress llm error: %v", err)
		return nil
	}

	for _, fact := range result.Facts {
		if err := e.WriteTier2(fact); err != nil {
			log.Printf("[memory] daily compress write tier2 error: %v", err)
		}
	}

	return e.MarkEventsCompressed(yesterday)
}

func (e *Engine) WeeklyDeepCompress(llm LLMClient) error {
	rows, err := e.db.Query(`
		SELECT DISTINCT project, topic FROM memories
		WHERE tier = 2 AND is_archived = 0
	`)
	if err != nil {
		return fmt.Errorf("weekly compress query partitions: %w", err)
	}
	defer rows.Close()

	type partition struct{ project, topic string }
	parts := make([]partition, 0)
	for rows.Next() {
		var p partition
		if err := rows.Scan(&p.project, &p.topic); err != nil {
			return fmt.Errorf("scan partition: %w", err)
		}
		parts = append(parts, p)
	}

	for _, p := range parts {
		entries, err := e.QueryTier2(p.project, p.topic, 500)
		if err != nil {
			log.Printf("[memory] weekly compress query partition error: %v", err)
			continue
		}
		if len(entries) < 10 {
			continue
		}

		merged, err := llm.Compress(weeklyCompressPrompt, formatEntries(entries))
		if err != nil {
			log.Printf("[memory] weekly compress llm error for %s/%s: %v", p.project, p.topic, err)
			continue
		}

		for _, old := range entries {
			if err := e.ArchiveMemory(old.ID); err != nil {
				log.Printf("[memory] weekly compress archive old id=%d error: %v", old.ID, err)
			}
		}
		for _, fact := range merged.Facts {
			if err := e.WriteTier2(fact); err != nil {
				log.Printf("[memory] weekly compress write merged fact error: %v", err)
			}
		}
	}

	if err := e.refreshTier1(llm); err != nil {
		log.Printf("[memory] refresh tier1 error: %v", err)
	}
	if err := e.cleanupDecayed(); err != nil {
		return fmt.Errorf("cleanup decayed: %w", err)
	}
	return nil
}

func (e *Engine) refreshTier1(llm LLMClient) error {
	current, err := e.LoadTier1()
	if err != nil {
		return fmt.Errorf("load current tier1: %w", err)
	}

	rows, err := e.db.Query(`
		SELECT id, tier, project, topic, category, content, importance, source,
		       created_at, updated_at, last_accessed, access_count, is_archived
		FROM memories
		WHERE tier = 2 AND importance >= 0.7 AND is_archived = 0
		ORDER BY importance DESC
		LIMIT 200
	`)
	if err != nil {
		return fmt.Errorf("query high-importance facts: %w", err)
	}
	defer rows.Close()

	high, err := scanMemories(rows)
	if err != nil {
		return err
	}

	result, err := llm.UpdateProfile(current, formatEntries(high))
	if err != nil {
		return fmt.Errorf("llm update profile: %w", err)
	}
	if len(result.Entries) == 0 {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, err := e.db.Exec(`UPDATE memories SET is_archived = 1, updated_at = datetime('now') WHERE tier = 1 AND is_archived = 0`); err != nil {
		return fmt.Errorf("archive old tier1: %w", err)
	}
	for _, p := range result.Entries {
		category := strings.TrimSpace(p.Category)
		if category == "" {
			category = "identity"
		}
		if _, err := e.db.Exec(`
			INSERT INTO memories (tier, project, topic, category, content, importance, source)
			VALUES (1, '_global', '_profile', ?, ?, 1.0, 'compression')
		`, category, strings.TrimSpace(p.Content)); err != nil {
			return fmt.Errorf("insert new tier1: %w", err)
		}
	}
	return nil
}

func (e *Engine) cleanupDecayed() error {
	rows, err := e.db.Query(`
		SELECT id, tier, project, topic, category, content, importance, source,
		       created_at, updated_at, last_accessed, access_count, is_archived
		FROM memories
		WHERE tier = 2 AND is_archived = 0 AND category IN ('temp', 'debug')
	`)
	if err != nil {
		return fmt.Errorf("query decayed candidates: %w", err)
	}
	defer rows.Close()

	mems, err := scanMemories(rows)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, m := range mems {
		score := relevanceScore(m, daysSince(m.LastAccessed, now))
		if score <= 0.001 {
			if err := e.ArchiveMemory(m.ID); err != nil {
				log.Printf("[memory] cleanup decayed archive error: %v", err)
			}
		}
	}
	return nil
}

func joinEventSummaries(events []EventEntry) string {
	var sb strings.Builder
	for _, ev := range events {
		if strings.TrimSpace(ev.Summary) == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(ev.Summary)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func formatEntries(memories []Memory) string {
	var sb strings.Builder
	for _, m := range memories {
		sb.WriteString("- [")
		sb.WriteString(m.Project)
		sb.WriteString("/")
		sb.WriteString(m.Topic)
		sb.WriteString("] ")
		sb.WriteString(m.Content)
		sb.WriteString(" (category=")
		sb.WriteString(m.Category)
		sb.WriteString(", importance=")
		sb.WriteString(fmt.Sprintf("%.2f", m.Importance))
		sb.WriteString(")\n")
	}
	return strings.TrimSpace(sb.String())
}
