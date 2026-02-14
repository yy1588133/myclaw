package memory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stellarlinkco/myclaw/internal/config"
)

type stubEmbedder struct {
	vectors map[string][]float32
}

func (s *stubEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if vec, ok := s.vectors[text]; ok {
		out := make([]float32, len(vec))
		copy(out, vec)
		return out, nil
	}
	return []float32{0, 1}, nil
}

func (s *stubEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vec, err := s.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, vec)
	}
	return out, nil
}

type stubReranker struct {
	scores []RerankScore
}

func (s *stubReranker) Rerank(_ context.Context, _ string, _ []string) ([]RerankScore, error) {
	out := make([]RerankScore, len(s.scores))
	copy(out, s.scores)
	return out, nil
}

func TestEnhancedRetrieveHybridRRF(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	e.SetRetrievalConfig(config.RetrievalConfig{Mode: config.MemoryRetrievalModeEnhanced})

	semantic := "token refresh race mitigation"
	lexicalNoise := "cache cache cache invalidation note"
	vectorDistractor := "concurrency lock ordering"

	seed := []FactEntry{
		{Content: lexicalNoise, Project: "myclaw", Topic: "retrieval", Category: "decision", Importance: 0.6},
		{Content: semantic, Project: "myclaw", Topic: "retrieval", Category: "solution", Importance: 0.7},
		{Content: vectorDistractor, Project: "myclaw", Topic: "retrieval", Category: "event", Importance: 0.5},
	}
	for _, fact := range seed {
		if err := e.WriteTier2(fact); err != nil {
			t.Fatalf("WriteTier2 error: %v", err)
		}
	}

	memories, err := e.QueryTier2("myclaw", "retrieval", 10)
	if err != nil {
		t.Fatalf("QueryTier2 error: %v", err)
	}
	if len(memories) < 3 {
		t.Fatalf("expected >=3 memories, got %d", len(memories))
	}

	expander := &stubQueryExpander{result: &QueryExpansion{
		Lexical:  []string{"token", "refresh", "mitigation"},
		Semantic: []string{"race"},
	}}
	e.SetQueryExpander(expander)

	embedder := &stubEmbedder{vectors: map[string][]float32{
		"cache invalidation race": {1, 0},
		semantic:                  {0.99, 0.01},
		lexicalNoise:              {0, 1},
		vectorDistractor:          {0.6, 0.4},
	}}
	e.SetEmbedder(embedder, "test-model", 5000)

	for _, mem := range memories {
		vec, _ := embedder.Embed(context.Background(), mem.Content)
		if err := e.UpdateMemoryEmbedding(mem.ID, "test-model", vec); err != nil {
			t.Fatalf("UpdateMemoryEmbedding error: %v", err)
		}
	}

	before := snapshotTier2ByID(t, e, "myclaw", "retrieval")

	results, err := e.Retrieve("cache invalidation race")
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected retrieval results")
	}
	if results[0].Content != semantic {
		t.Fatalf("expected semantic target first, got %q", results[0].Content)
	}
	if expander.calls == 0 {
		t.Fatal("expected query expander to be used for expanded branch")
	}

	repeated, err := e.Retrieve("cache invalidation race")
	if err != nil {
		t.Fatalf("second Retrieve error: %v", err)
	}
	assertSameResultOrder(t, results, repeated)

	after := snapshotTier2ByID(t, e, "myclaw", "retrieval")
	assertTouchDelta(t, before, after, results, 2)
}

func TestEnhancedRetrieveWithRerankBlend(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	e.SetRetrievalConfig(config.RetrievalConfig{Mode: config.MemoryRetrievalModeEnhanced})

	docA := "error timeout retry strategy"
	docB := "root cause details and fix"

	if err := e.WriteTier2(FactEntry{Content: docA, Project: "myclaw", Topic: "ops", Category: "decision", Importance: 0.8}); err != nil {
		t.Fatalf("WriteTier2 A error: %v", err)
	}
	if err := e.WriteTier2(FactEntry{Content: docB, Project: "myclaw", Topic: "ops", Category: "solution", Importance: 0.7}); err != nil {
		t.Fatalf("WriteTier2 B error: %v", err)
	}

	memories, err := e.QueryTier2("myclaw", "ops", 10)
	if err != nil {
		t.Fatalf("QueryTier2 error: %v", err)
	}

	embedder := &stubEmbedder{vectors: map[string][]float32{
		"timeout fix": {1, 0},
		docA:          {0.95, 0.05},
		docB:          {0.8, 0.2},
	}}
	e.SetEmbedder(embedder, "test-model", 5000)
	for _, mem := range memories {
		vec, _ := embedder.Embed(context.Background(), mem.Content)
		if err := e.UpdateMemoryEmbedding(mem.ID, "test-model", vec); err != nil {
			t.Fatalf("UpdateMemoryEmbedding error: %v", err)
		}
	}

	e.SetReranker(&stubReranker{scores: []RerankScore{{Index: 0, Score: 0}, {Index: 1, Score: 1}}})
	before := snapshotTier2ByID(t, e, "myclaw", "ops")

	results, err := e.Retrieve("timeout fix")
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected >=2 results, got %d", len(results))
	}
	if results[0].Content != docB {
		t.Fatalf("expected rerank blend to promote docB, got %q", results[0].Content)
	}

	repeated, err := e.Retrieve("timeout fix")
	if err != nil {
		t.Fatalf("second Retrieve error: %v", err)
	}
	assertSameResultOrder(t, results, repeated)

	after := snapshotTier2ByID(t, e, "myclaw", "ops")
	assertTouchDelta(t, before, after, results, 2)
}

func TestEnhancedRetrieveFallbackWithoutEmbeddings(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	e.SetRetrievalConfig(config.RetrievalConfig{Mode: config.MemoryRetrievalModeEnhanced})

	staleTemp := "stale scratchpad draft"
	recentSolution := "stable deployment rollback playbook"

	if err := e.WriteTier2(FactEntry{Content: staleTemp, Project: "myclaw", Topic: "ops", Category: "temp", Importance: 1.0}); err != nil {
		t.Fatalf("WriteTier2 staleTemp error: %v", err)
	}
	if err := e.WriteTier2(FactEntry{Content: recentSolution, Project: "myclaw", Topic: "ops", Category: "solution", Importance: 0.6}); err != nil {
		t.Fatalf("WriteTier2 recentSolution error: %v", err)
	}

	if _, err := e.db.Exec(`
		UPDATE memories
		SET last_accessed = datetime('now', '-120 day')
		WHERE content = ?
	`, staleTemp); err != nil {
		t.Fatalf("set stale last_accessed error: %v", err)
	}

	before := snapshotTier2ByID(t, e, "myclaw", "ops")

	results, err := e.Retrieve("phantom vector unmatched")
	if err != nil {
		t.Fatalf("Retrieve error without embeddings should fallback safely: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected fallback retrieval results")
	}
	if results[0].Content != recentSolution {
		t.Fatalf("expected classic fallback relevance ordering to promote recent solution, got %q", results[0].Content)
	}

	after := snapshotTier2ByID(t, e, "myclaw", "ops")
	assertTouchDelta(t, before, after, results, 1)
}

func snapshotTier2ByID(t *testing.T, e *Engine, project, topic string) map[int64]Memory {
	t.Helper()
	rows, err := e.QueryTier2(project, topic, 100)
	if err != nil {
		t.Fatalf("QueryTier2 snapshot error: %v", err)
	}

	byID := make(map[int64]Memory, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	return byID
}

func assertTouchDelta(t *testing.T, before, after map[int64]Memory, results []Memory, minDelta int) {
	t.Helper()
	for _, mem := range results {
		beforeRow, ok := before[mem.ID]
		if !ok {
			t.Fatalf("missing before snapshot for id=%d", mem.ID)
		}
		afterRow, ok := after[mem.ID]
		if !ok {
			t.Fatalf("missing after snapshot for id=%d", mem.ID)
		}
		if afterRow.AccessCount < beforeRow.AccessCount+minDelta {
			t.Fatalf("expected access count delta >= %d for id=%d, before=%d after=%d", minDelta, mem.ID, beforeRow.AccessCount, afterRow.AccessCount)
		}
	}
}

func assertSameResultOrder(t *testing.T, first, second []Memory) {
	t.Helper()
	if len(first) != len(second) {
		t.Fatalf("result length mismatch: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("result order mismatch at index %d: first=%d second=%d", i, first[i].ID, second[i].ID)
		}
	}
}
