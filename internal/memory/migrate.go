package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var dailyFilePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)

func MigrateFromFiles(workspace string, engine *Engine) error {
	memDir := filepath.Join(workspace, "memory")

	if err := migrateLongTerm(memDir, engine); err != nil {
		return err
	}
	if err := migrateDaily(memDir, engine); err != nil {
		return err
	}
	return nil
}

func migrateLongTerm(memDir string, engine *Engine) error {
	memPath := filepath.Join(memDir, "MEMORY.md")
	data, err := os.ReadFile(memPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read MEMORY.md: %w", err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var exists int
		err := engine.db.QueryRow(`
			SELECT COUNT(1) FROM memories
			WHERE tier = 1 AND content = ? AND source = 'migration' AND is_archived = 0
		`, line).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check tier1 migration duplicate: %w", err)
		}
		if exists > 0 {
			continue
		}
		if err := engine.WriteTier1(ProfileEntry{Content: line, Category: "identity"}); err != nil {
			return fmt.Errorf("migrate tier1 line: %w", err)
		}
		if _, err := engine.db.Exec(`
			UPDATE memories SET source = 'migration' WHERE id = last_insert_rowid()
		`); err != nil {
			return fmt.Errorf("mark migrated tier1 source: %w", err)
		}
	}
	return nil
}

func migrateDaily(memDir string, engine *Engine) error {
	entries, err := os.ReadDir(memDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read memory dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !dailyFilePattern.MatchString(name) {
			continue
		}
		date := strings.TrimSuffix(name, ".md")
		contentBytes, err := os.ReadFile(filepath.Join(memDir, name))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		content := strings.TrimSpace(string(contentBytes))
		if content == "" {
			continue
		}

		var exists int
		err = engine.db.QueryRow(`
			SELECT COUNT(1) FROM daily_events
			WHERE event_date = ? AND channel = 'migration' AND summary = ?
		`, date, content).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check event migration duplicate: %w", err)
		}
		if exists > 0 {
			continue
		}

		if err := engine.WriteTier3(EventEntry{
			Date:    date,
			Channel: "migration",
			Summary: content,
			Tokens:  estimateTokens(content),
		}); err != nil {
			return fmt.Errorf("migrate event %s: %w", name, err)
		}
	}

	return nil
}
