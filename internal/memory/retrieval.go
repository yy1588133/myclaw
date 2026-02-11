package memory

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	codeBlockRegex = regexp.MustCompile("(?s)```.*?```")
	cnWordRegex    = regexp.MustCompile(`[\p{Han}]{2,}`)
	enWordRegex    = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9_\-]{2,}`)
)

func shouldRetrieve(msg string) bool {
	trimmed := strings.TrimSpace(msg)
	if len(trimmed) < 5 {
		return false
	}
	if isMainlyCode(trimmed) {
		return false
	}

	msgLower := strings.ToLower(trimmed)
	skip := []string{"继续", "好的", "确认", "ok", "yes", "no"}
	for _, s := range skip {
		if msgLower == s {
			return false
		}
	}

	triggers := []string{
		"我的", "我之前", "你记得", "上次", "之前", "昨天",
		"什么", "怎么", "为什么", "?", "？", "喜欢", "设置", "配置", "密码",
	}
	for _, t := range triggers {
		if strings.Contains(trimmed, t) {
			return true
		}
	}

	return false
}

func ShouldRetrieve(msg string) bool {
	return shouldRetrieve(msg)
}

func isMainlyCode(msg string) bool {
	if codeBlockRegex.MatchString(msg) {
		return true
	}
	// Heuristic: many punctuation tokens and newlines implies code-ish input.
	punct := 0
	for _, r := range msg {
		switch r {
		case '{', '}', '(', ')', '[', ']', ';', '=', '<', '>', '/':
			punct++
		}
	}
	return punct >= 8 && strings.Count(msg, "\n") >= 2
}

func extractKeywords(msg string) []string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return nil
	}

	keywords := make([]string, 0)
	seen := map[string]struct{}{}

	for _, w := range cnWordRegex.FindAllString(msg, -1) {
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		keywords = append(keywords, w)
	}
	for _, w := range enWordRegex.FindAllString(strings.ToLower(msg), -1) {
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		keywords = append(keywords, w)
	}

	if len(keywords) > 8 {
		keywords = keywords[:8]
	}
	return keywords
}

func matchProject(msg string, projects []string) string {
	msgLower := strings.ToLower(msg)
	for _, p := range projects {
		if p == "" {
			continue
		}
		if strings.Contains(msgLower, strings.ToLower(p)) {
			return p
		}
	}
	return ""
}

func relevanceScore(mem Memory, daysSinceAccess float64) float64 {
	switch mem.Category {
	case "identity", "config", "credential":
		return mem.Importance
	case "decision", "solution":
		decay := math.Exp(-0.004 * daysSinceAccess)
		return mem.Importance * (0.3 + 0.7*decay)
	case "event", "conversation":
		decay := math.Exp(-0.023 * daysSinceAccess)
		return mem.Importance * (0.1 + 0.9*decay)
	case "temp", "debug":
		decay := math.Exp(-0.099 * daysSinceAccess)
		return mem.Importance * decay
	default:
		return mem.Importance
	}
}

func (e *Engine) Retrieve(msg string) ([]Memory, error) {
	keywords := extractKeywords(msg)
	project := matchProject(msg, e.knownProjectsSnapshot())

	base, err := e.queryRetrieveBase(project)
	if err != nil {
		return nil, err
	}

	seen := make(map[int64]struct{}, len(base))
	results := make([]Memory, 0, len(base)+10)
	for _, mem := range base {
		seen[mem.ID] = struct{}{}
		results = append(results, mem)
	}

	if len(results) < 5 && len(keywords) > 0 {
		ftsQuery := strings.Join(keywords, " OR ")
		extra, err := e.SearchFTS(ftsQuery, 10)
		if err == nil {
			for _, mem := range extra {
				if _, ok := seen[mem.ID]; ok {
					continue
				}
				seen[mem.ID] = struct{}{}
				results = append(results, mem)
			}
		}
	}

	now := time.Now().UTC()
	sort.Slice(results, func(i, j int) bool {
		di := daysSince(results[i].LastAccessed, now)
		dj := daysSince(results[j].LastAccessed, now)
		si := relevanceScore(results[i], di)
		sj := relevanceScore(results[j], dj)
		if si == sj {
			return results[i].Importance > results[j].Importance
		}
		return si > sj
	})

	if len(results) > 5 {
		results = results[:5]
	}

	for _, mem := range results {
		_ = e.TouchMemory(mem.ID)
	}

	return results, nil
}

func (e *Engine) queryRetrieveBase(project string) ([]Memory, error) {
	query := `
		SELECT id, tier, project, topic, category, content, importance, source,
		       created_at, updated_at, last_accessed, access_count, is_archived
		FROM memories
		WHERE tier = 2 AND is_archived = 0
	`
	args := make([]any, 0)
	if project != "" {
		query += ` AND (project = ? OR project = '_global')`
		args = append(args, project)
	}
	query += ` ORDER BY importance DESC LIMIT 20`

	rows, err := e.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("retrieve base query: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

func formatMemories(memories []Memory) string {
	if len(memories) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, m := range memories {
		sb.WriteString("- [")
		sb.WriteString(m.Project)
		sb.WriteString("/")
		sb.WriteString(m.Topic)
		sb.WriteString("] ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func FormatMemories(memories []Memory) string {
	return formatMemories(memories)
}

func daysSince(lastAccessed string, now time.Time) float64 {
	if strings.TrimSpace(lastAccessed) == "" {
		return 365
	}
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, lastAccessed); err == nil {
			d := now.Sub(t).Hours() / 24
			if d < 0 {
				return 0
			}
			return d
		}
	}
	return 365
}
