package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

const (
	rrfK               = 60.0
	originalQueryBoost = 2.0
	rrfTop1Bonus       = 0.05
	rrfTop3Bonus       = 0.02
)

type enhancedCandidate struct {
	Memory
	rrfScore    float64
	rerankScore float64
	hasRerank   bool
	finalScore  float64
}

func (e *Engine) retrieveEnhanced(msg string) ([]Memory, error) {
	keywords := sanitizeFTSTokens(extractKeywords(msg))
	project := matchProject(msg, e.knownProjectsSnapshot())
	retrievalCfg := e.retrievalConfigSnapshot()

	base, err := e.queryRetrieveBase(project)
	if err != nil {
		return nil, err
	}

	if len(keywords) == 0 {
		return e.finalizeClassicResults(base), nil
	}

	expandedTokens := []string(nil)
	if expander := e.queryExpanderSnapshot(); expander != nil {
		if expansion, err := expander.Expand(msg); err == nil && expansion != nil {
			merged := mergeUniqueTokens(keywords, expansion.allTokens())
			if !tokensEqual(merged, keywords) {
				expandedTokens = merged
			}
		}
	}

	candidateLimit := retrievalCfg.CandidateLimit
	if candidateLimit <= 0 {
		candidateLimit = 40
	}

	var (
		origFTS []scoredFTSMatch
		expFTS  []scoredFTSMatch
		origVec []Memory
		expVec  []Memory
	)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		matches, err := e.searchFTSScored(keywords, candidateLimit)
		if err != nil {
			return
		}
		origFTS = matches
	}()

	if len(expandedTokens) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			matches, err := e.searchFTSScored(expandedTokens, candidateLimit)
			if err != nil {
				return
			}
			expFTS = matches
		}()
	}

	embedder, _, embeddingTimeoutMs := e.embeddingSnapshot()
	if embedder != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := withEmbeddingTimeout(context.Background(), embeddingTimeoutMs)
			defer cancel()
			memories, err := e.searchVectorCandidates(ctx, embedder, msg, project, candidateLimit)
			if err != nil {
				return
			}
			origVec = memories
		}()

		expandedQuery := strings.TrimSpace(strings.Join(expandedTokens, " "))
		if expandedQuery != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := withEmbeddingTimeout(context.Background(), embeddingTimeoutMs)
				defer cancel()
				memories, err := e.searchVectorCandidates(ctx, embedder, expandedQuery, project, candidateLimit)
				if err != nil {
					return
				}
				expVec = memories
			}()
		}
	}

	wg.Wait()

	if !hasHybridCandidates(origFTS, expFTS, origVec, expVec) {
		return e.finalizeClassicResults(base), nil
	}

	fused := fuseCandidatesRRF(base, origFTS, expFTS, origVec, expVec)
	if len(fused) == 0 {
		return e.finalizeClassicResults(base), nil
	}

	ranked := sortCandidatesByRRF(fused)
	rerankLimit := retrievalCfg.RerankLimit
	if rerankLimit <= 0 {
		rerankLimit = 20
	}
	if rerankLimit > len(ranked) {
		rerankLimit = len(ranked)
	}

	reranker := e.rerankerSnapshot()
	if reranker != nil && rerankLimit > 0 {
		docs := make([]string, 0, rerankLimit)
		for i := 0; i < rerankLimit; i++ {
			docs = append(docs, ranked[i].Content)
		}
		if scores, err := reranker.Rerank(context.Background(), msg, docs); err == nil {
			for _, score := range scores {
				if score.Index < 0 || score.Index >= rerankLimit {
					continue
				}
				ranked[score.Index].rerankScore = score.Score
				ranked[score.Index].hasRerank = true
			}
		}
	}

	for i := range ranked {
		ranked[i].finalScore = ranked[i].rrfScore
		if ranked[i].hasRerank {
			position := i + 1
			rrfWeight, rerankWeight := bucketWeights(position)
			ranked[i].finalScore = ranked[i].rrfScore*rrfWeight + ranked[i].rerankScore*rerankWeight
		}
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].finalScore == ranked[j].finalScore {
			if ranked[i].rrfScore == ranked[j].rrfScore {
				if ranked[i].rerankScore == ranked[j].rerankScore {
					if ranked[i].hasRerank != ranked[j].hasRerank {
						return ranked[i].hasRerank
					}
					if ranked[i].Importance == ranked[j].Importance {
						return ranked[i].ID < ranked[j].ID
					}
					return ranked[i].Importance > ranked[j].Importance
				}
				return ranked[i].rerankScore > ranked[j].rerankScore
			}
			return ranked[i].rrfScore > ranked[j].rrfScore
		}
		return ranked[i].finalScore > ranked[j].finalScore
	})

	if len(ranked) > 5 {
		ranked = ranked[:5]
	}

	results := make([]Memory, 0, len(ranked))
	for _, c := range ranked {
		results = append(results, c.Memory)
	}
	for _, mem := range results {
		_ = e.TouchMemory(mem.ID)
	}
	return results, nil
}

func (e *Engine) finalizeClassicResults(base []Memory) []Memory {
	results := sortAndTrimClassicResults(base, 5)
	for _, mem := range results {
		_ = e.TouchMemory(mem.ID)
	}
	return results
}

func (e *Engine) searchVectorCandidates(ctx context.Context, embedder Embedder, query, project string, limit int) ([]Memory, error) {
	if embedder == nil {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	queryVector, err := embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search vector embed query: %w", err)
	}

	rows, err := e.queryVectorRows(project)
	if err != nil {
		return nil, err
	}

	type vectorCandidate struct {
		Memory
		score float64
	}
	candidates := make([]vectorCandidate, 0, len(rows))
	for _, row := range rows {
		vec, err := DecodeVector(row.embedding)
		if err != nil {
			continue
		}
		score, err := CosineSimilarity(queryVector, vec)
		if err != nil {
			continue
		}
		candidates = append(candidates, vectorCandidate{Memory: row.memory, score: score})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			if candidates[i].Importance == candidates[j].Importance {
				return candidates[i].ID < candidates[j].ID
			}
			return candidates[i].Importance > candidates[j].Importance
		}
		return candidates[i].score > candidates[j].score
	})

	if limit <= 0 {
		limit = 40
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	result := make([]Memory, 0, len(candidates))
	for _, c := range candidates {
		result = append(result, c.Memory)
	}
	return result, nil
}

type memoryWithEmbedding struct {
	memory    Memory
	embedding []byte
}

func (e *Engine) queryVectorRows(project string) ([]memoryWithEmbedding, error) {
	query := `
		SELECT id, tier, project, topic, category, content, importance, source,
		       created_at, updated_at, last_accessed, access_count, is_archived, embedding
		FROM memories
		WHERE tier = 2
		  AND is_archived = 0
		  AND embedding IS NOT NULL
		  AND embedding_dim > 0
	`
	args := make([]any, 0, 1)
	if project != "" {
		query += ` AND (project = ? OR project = '_global')`
		args = append(args, project)
	}

	rows, err := e.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query vector rows: %w", err)
	}
	defer rows.Close()

	result := make([]memoryWithEmbedding, 0)
	for rows.Next() {
		var m Memory
		var archived int
		var embedding []byte
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
			&embedding,
		); err != nil {
			return nil, fmt.Errorf("scan vector row: %w", err)
		}
		m.IsArchived = archived == 1
		result = append(result, memoryWithEmbedding{memory: m, embedding: embedding})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vector rows: %w", err)
	}
	return result, nil
}

func fuseCandidatesRRF(base []Memory, origFTS, expFTS []scoredFTSMatch, origVec, expVec []Memory) map[int64]*enhancedCandidate {
	candidates := make(map[int64]*enhancedCandidate, len(base)+len(origFTS)+len(expFTS)+len(origVec)+len(expVec))
	addMemory := func(mem Memory) {
		if _, ok := candidates[mem.ID]; ok {
			return
		}
		copied := mem
		candidates[mem.ID] = &enhancedCandidate{Memory: copied}
	}
	for _, mem := range base {
		addMemory(mem)
	}
	for _, m := range origFTS {
		addMemory(m.Memory)
	}
	for _, m := range expFTS {
		addMemory(m.Memory)
	}
	for _, mem := range origVec {
		addMemory(mem)
	}
	for _, mem := range expVec {
		addMemory(mem)
	}

	applyRank := func(rank int, mem Memory, weight float64) {
		if rank <= 0 {
			return
		}
		c, ok := candidates[mem.ID]
		if !ok {
			return
		}
		c.rrfScore += weight / (rrfK + float64(rank))
		if rank == 1 {
			c.rrfScore += rrfTop1Bonus * weight
		} else if rank <= 3 {
			c.rrfScore += rrfTop3Bonus * weight
		}
	}

	for i, m := range origFTS {
		applyRank(i+1, m.Memory, originalQueryBoost)
	}
	for i, m := range expFTS {
		applyRank(i+1, m.Memory, 1.0)
	}
	for i, mem := range origVec {
		applyRank(i+1, mem, originalQueryBoost)
	}
	for i, mem := range expVec {
		applyRank(i+1, mem, 1.0)
	}

	return candidates
}

func sortCandidatesByRRF(fused map[int64]*enhancedCandidate) []enhancedCandidate {
	out := make([]enhancedCandidate, 0, len(fused))
	for _, c := range fused {
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rrfScore == out[j].rrfScore {
			if out[i].Importance == out[j].Importance {
				return out[i].ID < out[j].ID
			}
			return out[i].Importance > out[j].Importance
		}
		return out[i].rrfScore > out[j].rrfScore
	})
	return out
}

func hasHybridCandidates(origFTS, expFTS []scoredFTSMatch, origVec, expVec []Memory) bool {
	return len(origFTS) > 0 || len(expFTS) > 0 || len(origVec) > 0 || len(expVec) > 0
}

func tokensEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func bucketWeights(position int) (rrfWeight, rerankWeight float64) {
	switch {
	case position <= 3:
		return 0.75, 0.25
	case position <= 10:
		return 0.60, 0.40
	default:
		return 0.40, 0.60
	}
}
