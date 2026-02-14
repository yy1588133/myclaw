package memory

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/stellarlinkco/myclaw/internal/config"
)

type missingEmbeddingRow struct {
	ID      int64
	Content string
}

// SetEmbedder wires optional embedding generation for tier-2 writes/backfill.
// timeoutMs<=0 falls back to DefaultMemoryEmbeddingTimeoutMs.
func (e *Engine) SetEmbedder(embedder Embedder, model string, timeoutMs int) {
	e.embeddingMu.Lock()
	defer e.embeddingMu.Unlock()

	e.embedder = embedder
	e.embeddingModel = strings.TrimSpace(model)
	if timeoutMs <= 0 {
		timeoutMs = config.DefaultMemoryEmbeddingTimeoutMs
	}
	e.embeddingTimeoutMs = timeoutMs
}

func (e *Engine) embeddingSnapshot() (Embedder, string, int) {
	e.embeddingMu.RLock()
	defer e.embeddingMu.RUnlock()
	return e.embedder, e.embeddingModel, e.embeddingTimeoutMs
}

func (e *Engine) insertTier2Row(project, topic, category, content string, importance float64) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	result, err := e.db.Exec(`
		INSERT INTO memories (tier, project, topic, category, content, importance, source)
		VALUES (2, ?, ?, ?, ?, ?, 'extraction')
	`, project, topic, category, content, importance)
	if err != nil {
		return 0, fmt.Errorf("write tier2: %w", err)
	}

	memoryID, err := result.LastInsertId()
	if err != nil {
		// Keep WriteTier2 fail-open compatibility: data is persisted even if ID retrieval fails.
		log.Printf("[memory] write tier2 last insert id unavailable: %v", err)
		return 0, nil
	}
	return memoryID, nil
}

func (e *Engine) scheduleTier2Embedding(memoryID int64, content string) {
	embedder, model, timeoutMs := e.embeddingSnapshot()
	if embedder == nil || memoryID <= 0 {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	go func() {
		ctx, cancel := withEmbeddingTimeout(context.Background(), timeoutMs)
		defer cancel()

		vector, err := embedder.Embed(ctx, content)
		if err != nil {
			log.Printf("[memory] async tier2 embedding failed id=%d: %v", memoryID, err)
			return
		}
		if err := e.UpdateMemoryEmbedding(memoryID, model, vector); err != nil {
			log.Printf("[memory] async tier2 embedding persist failed id=%d: %v", memoryID, err)
		}
	}()
}

// UpdateMemoryEmbedding idempotently upserts embedding fields for a memory row.
func (e *Engine) UpdateMemoryEmbedding(memoryID int64, model string, vector []float32) error {
	if memoryID <= 0 {
		return fmt.Errorf("update memory embedding: invalid id %d", memoryID)
	}

	blob, err := EncodeVector(vector)
	if err != nil {
		return fmt.Errorf("update memory embedding: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, err := e.db.Exec(`
		UPDATE memories
		SET embedding = ?,
		    embedding_model = ?,
		    embedding_dim = ?,
		    embedding_updated_at = datetime('now'),
		    updated_at = datetime('now')
		WHERE id = ? AND tier = 2
	`, blob, strings.TrimSpace(model), len(vector), memoryID); err != nil {
		return fmt.Errorf("update memory embedding: %w", err)
	}
	return nil
}

// BackfillEmbeddings fills missing tier-2 embeddings in deterministic id order.
// It is idempotent: rows with existing embeddings are skipped.
func (e *Engine) BackfillEmbeddings(ctx context.Context, batchSize int) (int, error) {
	embedder, model, timeoutMs := e.embeddingSnapshot()
	if embedder == nil {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = config.DefaultMemoryEmbeddingBatchSize
	}

	totalUpdated := 0
	for {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return totalUpdated, ctx.Err()
			default:
			}
		}

		rows, err := e.queryTier2MissingEmbeddings(batchSize)
		if err != nil {
			return totalUpdated, err
		}
		if len(rows) == 0 {
			return totalUpdated, nil
		}

		texts := make([]string, 0, len(rows))
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
			texts = append(texts, row.Content)
		}

		embedCtx, cancel := withEmbeddingTimeout(ctx, timeoutMs)
		vectors, err := embedder.EmbedBatch(embedCtx, texts)
		cancel()
		if err != nil {
			return totalUpdated, fmt.Errorf("backfill embeddings: embed batch: %w", err)
		}
		if len(vectors) != len(ids) {
			return totalUpdated, fmt.Errorf("backfill embeddings: embed batch count mismatch: got %d want %d", len(vectors), len(ids))
		}

		for i, id := range ids {
			if err := e.UpdateMemoryEmbedding(id, model, vectors[i]); err != nil {
				return totalUpdated, fmt.Errorf("backfill embeddings: update id=%d: %w", id, err)
			}
			totalUpdated++
		}

		if len(rows) < batchSize {
			return totalUpdated, nil
		}
	}
}

func (e *Engine) queryTier2MissingEmbeddings(limit int) ([]missingEmbeddingRow, error) {
	if limit <= 0 {
		limit = config.DefaultMemoryEmbeddingBatchSize
	}

	rows, err := e.db.Query(`
		SELECT id, content
		FROM memories
		WHERE tier = 2
		  AND is_archived = 0
		  AND TRIM(content) != ''
		  AND (embedding IS NULL OR embedding_dim = 0)
		ORDER BY id ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query missing embeddings: %w", err)
	}
	defer rows.Close()

	result := make([]missingEmbeddingRow, 0, limit)
	for rows.Next() {
		var row missingEmbeddingRow
		if err := rows.Scan(&row.ID, &row.Content); err != nil {
			return nil, fmt.Errorf("scan missing embeddings row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate missing embeddings rows: %w", err)
	}
	return result, nil
}

func withEmbeddingTimeout(parent context.Context, timeoutMs int) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if timeoutMs <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, time.Duration(timeoutMs)*time.Millisecond)
}
