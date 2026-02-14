package memory

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type lockProbeEmbedder struct {
	engine *Engine
	called chan struct{}
}

func newLockProbeEmbedder(engine *Engine) *lockProbeEmbedder {
	return &lockProbeEmbedder{
		engine: engine,
		called: make(chan struct{}),
	}
}

func (m *lockProbeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	defer close(m.called)

	lockAcquired := make(chan struct{})
	go func() {
		m.engine.mu.Lock()
		m.engine.mu.Unlock()
		close(lockAcquired)
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lockAcquired:
		return []float32{0.25, 0.75}, nil
	case <-time.After(300 * time.Millisecond):
		return nil, fmt.Errorf("engine mutex held while embed is running")
	}
}

func (m *lockProbeEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, fmt.Errorf("EmbedBatch not expected in lockProbeEmbedder")
}

type deterministicBackfillEmbedder struct {
	mu    sync.Mutex
	calls [][]string
}

type failingEmbedder struct {
	called chan struct{}
}

func newFailingEmbedder() *failingEmbedder {
	return &failingEmbedder{called: make(chan struct{})}
}

func (m *failingEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	defer close(m.called)
	return nil, fmt.Errorf("forced embedder failure")
}

func (m *failingEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, fmt.Errorf("EmbedBatch not expected in failingEmbedder")
}

func (m *deterministicBackfillEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{float32(len([]rune(text)))}, nil
}

func (m *deterministicBackfillEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	copiedTexts := append([]string(nil), texts...)
	m.mu.Lock()
	m.calls = append(m.calls, copiedTexts)
	m.mu.Unlock()

	vectors := make([][]float32, 0, len(texts))
	for i, text := range texts {
		vectors = append(vectors, []float32{float32(len([]rune(text))), float32(i + 1)})
	}
	return vectors, nil
}

func (m *deterministicBackfillEmbedder) snapshotCalls() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]string, 0, len(m.calls))
	for _, call := range m.calls {
		out = append(out, append([]string(nil), call...))
	}
	return out
}

func TestWriteTier2WithoutEmbedderStillSucceeds(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	fact := FactEntry{Content: "write should succeed without embedder", Project: "myclaw", Topic: "embedding", Category: "event", Importance: 0.6}
	if err := e.WriteTier2(fact); err != nil {
		t.Fatalf("WriteTier2 error: %v", err)
	}

	var (
		count int
		dim   int
		model string
	)
	if err := e.db.QueryRow(`
		SELECT COUNT(1), COALESCE(MAX(embedding_dim), 0), COALESCE(MAX(embedding_model), '')
		FROM memories
		WHERE tier = 2 AND content = ?
	`, fact.Content).Scan(&count, &dim, &model); err != nil {
		t.Fatalf("query inserted tier2 row: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one inserted row, got %d", count)
	}
	if dim != 0 {
		t.Fatalf("expected embedding_dim=0 without embedder, got %d", dim)
	}
	if model != "" {
		t.Fatalf("expected embedding_model empty without embedder, got %q", model)
	}
}

func TestWriteTier2EmbeddingUpdateOutsideLock(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	probe := newLockProbeEmbedder(e)
	e.SetEmbedder(probe, "test-embed-model", 1000)

	fact := FactEntry{Content: "embedding should be async", Project: "myclaw", Topic: "embedding", Category: "solution", Importance: 0.9}
	if err := e.WriteTier2(fact); err != nil {
		t.Fatalf("WriteTier2 error: %v", err)
	}

	var memoryID int64
	if err := e.db.QueryRow(`SELECT id FROM memories WHERE tier = 2 AND content = ? ORDER BY id DESC LIMIT 1`, fact.Content).Scan(&memoryID); err != nil {
		t.Fatalf("query inserted memory id: %v", err)
	}

	select {
	case <-probe.called:
	case <-time.After(2 * time.Second):
		t.Fatal("expected embedder to be invoked asynchronously")
	}

	deadline := time.Now().Add(2 * time.Second)
	var (
		embeddingDim   int
		embeddingModel string
		embeddingBlob  []byte
	)
	for {
		if err := e.db.QueryRow(`
			SELECT embedding_dim, embedding_model, embedding
			FROM memories
			WHERE id = ?
		`, memoryID).Scan(&embeddingDim, &embeddingModel, &embeddingBlob); err != nil {
			t.Fatalf("query embedded row: %v", err)
		}
		if embeddingDim > 0 && embeddingModel == "test-embed-model" && len(embeddingBlob) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("embedding update timeout: dim=%d model=%q blobLen=%d", embeddingDim, embeddingModel, len(embeddingBlob))
		}
		time.Sleep(20 * time.Millisecond)
	}

	decoded, err := DecodeVector(embeddingBlob)
	if err != nil {
		t.Fatalf("DecodeVector error: %v", err)
	}
	if len(decoded) != embeddingDim {
		t.Fatalf("decoded vector dim mismatch: decoded=%d recorded=%d", len(decoded), embeddingDim)
	}
}

func TestWriteTier2EmbedderFailureStillSucceeds(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	failer := newFailingEmbedder()
	e.SetEmbedder(failer, "broken-model", 1000)

	fact := FactEntry{Content: "write survives embed failure", Project: "myclaw", Topic: "embedding", Category: "event", Importance: 0.7}
	if err := e.WriteTier2(fact); err != nil {
		t.Fatalf("WriteTier2 should stay fail-open on embedder errors: %v", err)
	}

	select {
	case <-failer.called:
	case <-time.After(2 * time.Second):
		t.Fatal("expected failing embedder to be invoked")
	}

	var (
		count int
		dim   int
	)
	if err := e.db.QueryRow(`
		SELECT COUNT(1), COALESCE(MAX(embedding_dim), 0)
		FROM memories
		WHERE tier = 2 AND content = ?
	`, fact.Content).Scan(&count, &dim); err != nil {
		t.Fatalf("query row after embedder failure: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected row persisted despite embedder failure, count=%d", count)
	}
	if dim != 0 {
		t.Fatalf("expected embedding to remain missing after failed embedder call, got dim=%d", dim)
	}
}

func TestBackfillEmbeddingsIdempotent(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	facts := []FactEntry{
		{Content: "fact-one-missing", Project: "myclaw", Topic: "backfill", Category: "event", Importance: 0.4},
		{Content: "fact-two-preembedded", Project: "myclaw", Topic: "backfill", Category: "event", Importance: 0.5},
		{Content: "fact-three-missing", Project: "myclaw", Topic: "backfill", Category: "event", Importance: 0.6},
	}
	for _, fact := range facts {
		if err := e.WriteTier2(fact); err != nil {
			t.Fatalf("WriteTier2(%q) error: %v", fact.Content, err)
		}
	}

	preEmbeddedID := memoryIDByContent(t, e, "fact-two-preembedded")
	if err := e.UpdateMemoryEmbedding(preEmbeddedID, "existing-model", []float32{9, 9}); err != nil {
		t.Fatalf("seed pre-embedded row error: %v", err)
	}

	embedder := &deterministicBackfillEmbedder{}
	e.SetEmbedder(embedder, "backfill-model", 1000)

	updated, err := e.BackfillEmbeddings(context.Background(), 2)
	if err != nil {
		t.Fatalf("BackfillEmbeddings first run error: %v", err)
	}
	if updated != 2 {
		t.Fatalf("expected first backfill to update 2 rows, got %d", updated)
	}

	calls := embedder.snapshotCalls()
	if len(calls) != 1 {
		t.Fatalf("expected one EmbedBatch call, got %d", len(calls))
	}
	wantOrder := []string{"fact-one-missing", "fact-three-missing"}
	if !reflect.DeepEqual(calls[0], wantOrder) {
		t.Fatalf("unexpected backfill order: got %+v want %+v", calls[0], wantOrder)
	}

	var missing int
	if err := e.db.QueryRow(`
		SELECT COUNT(1)
		FROM memories
		WHERE tier = 2 AND is_archived = 0 AND (embedding IS NULL OR embedding_dim = 0)
	`).Scan(&missing); err != nil {
		t.Fatalf("count missing embeddings after backfill: %v", err)
	}
	if missing != 0 {
		t.Fatalf("expected all tier2 rows embedded after first run, missing=%d", missing)
	}

	updatedAgain, err := e.BackfillEmbeddings(context.Background(), 2)
	if err != nil {
		t.Fatalf("BackfillEmbeddings second run error: %v", err)
	}
	if updatedAgain != 0 {
		t.Fatalf("expected second backfill to be idempotent (0 updates), got %d", updatedAgain)
	}

	callsAfterSecondRun := embedder.snapshotCalls()
	if len(callsAfterSecondRun) != 1 {
		t.Fatalf("expected no additional EmbedBatch calls on second run, got %d", len(callsAfterSecondRun))
	}
}

func memoryIDByContent(t *testing.T, e *Engine, content string) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM memories WHERE tier = 2 AND content = ? ORDER BY id ASC LIMIT 1`, content).Scan(&id); err != nil {
		t.Fatalf("query memory id by content=%q: %v", content, err)
	}
	return id
}
