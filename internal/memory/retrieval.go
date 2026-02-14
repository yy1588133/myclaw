package memory

import (
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/stellarlinkco/myclaw/internal/config"
)

var (
	codeBlockRegex = regexp.MustCompile("(?s)```.*?```")
	cnWordRegex    = regexp.MustCompile(`[\p{Han}]{2,}`)
	enWordRegex    = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9_\-]{2,}`)
)

type scoredFTSMatch struct {
	Memory Memory
	Score  float64
}

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
	retrievalCfg := e.retrievalConfigSnapshot()
	if retrievalCfg.Mode == config.MemoryRetrievalModeEnhanced {
		results, err := e.retrieveEnhanced(msg)
		if err == nil {
			return results, nil
		}
	}
	return e.retrieveClassic(msg)
}

func (e *Engine) retrieveClassic(msg string) ([]Memory, error) {
	keywords := sanitizeFTSTokens(extractKeywords(msg))
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
		retrievalCfg := e.retrievalConfigSnapshot()
		stage1Matches, stage1Err := e.searchFTSScored(keywords, 10)
		if stage1Err == nil && isStrongSignalMatch(stage1Matches, retrievalCfg) {
			results = appendUniqueScoredMatches(results, seen, stage1Matches)
		} else {
			expandedTokens := keywords
			if expander := e.queryExpanderSnapshot(); expander != nil {
				if expansion, err := expander.Expand(msg); err == nil && expansion != nil {
					expandedTokens = mergeUniqueTokens(expandedTokens, expansion.allTokens())
				}
			}

			extraMatches, err := e.searchFTSScored(expandedTokens, 10)
			if err == nil {
				results = appendUniqueScoredMatches(results, seen, extraMatches)
			}
		}
	}

	results = sortAndTrimClassicResults(results, 5)

	for _, mem := range results {
		_ = e.TouchMemory(mem.ID)
	}

	return results, nil
}

func sortAndTrimClassicResults(results []Memory, limit int) []Memory {
	if len(results) == 0 {
		return nil
	}

	sorted := append([]Memory(nil), results...)
	now := time.Now().UTC()
	sort.SliceStable(sorted, func(i, j int) bool {
		di := daysSince(sorted[i].LastAccessed, now)
		dj := daysSince(sorted[j].LastAccessed, now)
		si := relevanceScore(sorted[i], di)
		sj := relevanceScore(sorted[j], dj)
		if si == sj {
			if sorted[i].Importance == sorted[j].Importance {
				return sorted[i].ID < sorted[j].ID
			}
			return sorted[i].Importance > sorted[j].Importance
		}
		return si > sj
	})

	if limit > 0 && len(sorted) > limit {
		sorted = sorted[:limit]
	}

	return sorted
}

func mergeUniqueTokens(base, extra []string) []string {
	merged := make([]string, 0, len(base)+len(extra))
	seen := make(map[string]struct{}, len(base)+len(extra))
	appendTokens := func(tokens []string) {
		for _, token := range sanitizeFTSTokens(tokens) {
			if _, exists := seen[token]; exists {
				continue
			}
			seen[token] = struct{}{}
			merged = append(merged, token)
			if len(merged) >= maxFTSTokens {
				return
			}
		}
	}
	appendTokens(base)
	if len(merged) >= maxFTSTokens {
		return merged
	}
	appendTokens(extra)
	return merged
}

func (e *Engine) searchFTSScored(tokens []string, limit int) ([]scoredFTSMatch, error) {
	if limit <= 0 {
		limit = 10
	}
	matchQuery := buildFTSMatchQuery(tokens)
	if matchQuery == "" {
		return nil, nil
	}

	rows, err := e.db.Query(`
		SELECT m.id, m.tier, m.project, m.topic, m.category, m.content, m.importance, m.source,
		       m.created_at, m.updated_at, m.last_accessed, m.access_count, m.is_archived,
		       bm25(memories_fts) AS bm25_score
		FROM memories m
		JOIN memories_fts f ON m.id = f.rowid
		WHERE memories_fts MATCH ?
		  AND m.tier = 2
		  AND m.is_archived = 0
		ORDER BY bm25(memories_fts), m.importance DESC
		LIMIT ?
	`, matchQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("search fts scored: %w", err)
	}
	defer rows.Close()

	result := make([]scoredFTSMatch, 0)
	for rows.Next() {
		item, err := scanScoredFTSMatch(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fts scored: %w", err)
	}

	return result, nil
}

func scanScoredFTSMatch(rows *sql.Rows) (scoredFTSMatch, error) {
	var m Memory
	var archived int
	var score float64
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
		&score,
	); err != nil {
		return scoredFTSMatch{}, fmt.Errorf("scan scored fts match: %w", err)
	}
	m.IsArchived = archived == 1
	return scoredFTSMatch{Memory: m, Score: score}, nil
}

func isStrongSignalMatch(matches []scoredFTSMatch, cfg retrievalRuntimeConfig) bool {
	if len(matches) == 0 {
		return false
	}

	rawScores := make([]float64, len(matches))
	for i, match := range matches {
		rawScores[i] = match.Score
	}
	normalized := normalizeBM25(rawScores)
	top := normalized[0]
	next := 0.0
	if len(normalized) > 1 {
		next = normalized[1]
	}
	gap := top - next

	return top >= cfg.StrongSignalThreshold && gap >= cfg.StrongSignalGap
}

func normalizeBM25(rawScores []float64) []float64 {
	if len(rawScores) == 0 {
		return nil
	}

	minScore := rawScores[0]
	maxScore := rawScores[0]
	for _, score := range rawScores[1:] {
		if score < minScore {
			minScore = score
		}
		if score > maxScore {
			maxScore = score
		}
	}

	normalized := make([]float64, len(rawScores))
	if maxScore == minScore {
		for i := range normalized {
			normalized[i] = 1
		}
		return normalized
	}

	rangeSize := maxScore - minScore
	for i, score := range rawScores {
		norm := 1 - ((score - minScore) / rangeSize)
		if norm < 0 {
			norm = 0
		}
		if norm > 1 {
			norm = 1
		}
		normalized[i] = norm
	}
	return normalized
}

func appendUniqueScoredMatches(results []Memory, seen map[int64]struct{}, matches []scoredFTSMatch) []Memory {
	for _, match := range matches {
		mem := match.Memory
		if _, exists := seen[mem.ID]; exists {
			continue
		}
		seen[mem.ID] = struct{}{}
		results = append(results, mem)
	}
	return results
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
