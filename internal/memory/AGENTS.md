# memory — SQLite Tiered Memory Engine

## OVERVIEW
Three-tier SQLite memory system: Tier1 (core profile → system prompt), Tier2 (knowledge facts → retrieval-augmented user prompt), Tier3 (daily event log → compression source). Includes async extraction pipeline and LLM-powered compression.

## STRUCTURE
| File | Role |
|------|------|
| `types.go` | All data types: `Memory`, `EventEntry`, `BufferMessage`, `FactEntry`, `ExtractionResult`, `CompressionResult`, `ProfileEntry`, `MemoryStats` |
| `engine.go` | `Engine` struct (SQLite + mutex), schema init, tier CRUD, buffer ops, FTS5 search, stats |
| `retrieval.go` | `Retrieve()` pipeline: keyword extraction → project matching → base query + FTS fallback → relevance scoring → top-5 |
| `extraction.go` | `ExtractionService` — async buffer + quiet-gap timer + token-cap flush + daily flush → LLM extract → Tier2/3 write |
| `compression.go` | `DailyCompress()` (yesterday's events → Tier2 facts), `WeeklyDeepCompress()` (merge per partition + refreshTier1 + cleanupDecayed) |
| `llm.go` | `LLMClient` interface + OpenAI-compatible HTTP client for Extract/Compress/UpdateProfile |
| `memory.go` | **Legacy** `MemoryStore` — file-based MEMORY.md + daily journal. Used by agent CLI mode only. |
| `migrate.go` | `MigrateFromFiles()` — one-time migration from legacy file memory to SQLite (Tier1 from MEMORY.md, Tier3 from daily journals) |

## SQLITE SCHEMA
```sql
-- Tier1 + Tier2 unified table (tier column distinguishes)
memories (
  id, tier, project, topic, category, content, importance,
  source, created_at, updated_at, last_accessed, access_count, is_archived
)
-- FTS5 virtual table synced via triggers (insert/update/delete)
memories_fts (content) USING fts5, tokenize='unicode61'

-- Tier3 daily event log
daily_events (id, event_date, channel, sender_id, summary, raw_tokens, is_compressed)

-- Extraction pipeline buffer
extraction_buffer (id, channel, sender_id, role, content, token_count, created_at)
```

**Pragmas**: WAL mode, busy_timeout=5000, foreign_keys=ON.

## WHERE TO LOOK
| Task | Start Here |
|------|------------|
| Change retrieval logic | `retrieval.go:Retrieve()` — keyword extraction + project match + base query + FTS |
| Tune relevance scoring | `retrieval.go:relevanceScore()` — category-based exponential decay curves |
| Modify extraction pipeline | `extraction.go:ExtractionService` — quiet gap, token cap, flush logic |
| Change compression schedule | `gateway.go:ensureInternalMemoryJobs()` — daily 03:00, weekly Mon 04:00 |
| Add memory category | `types.go:FactEntry.Category` — must also update `relevanceScore()` decay curve |
| Change LLM prompts | `llm.go` — `extractionPrompt`, `dailyCompressPrompt`, `weeklyCompressPrompt`, `profileUpdatePrompt` |
| Debug migration | `migrate.go:MigrateFromFiles()` — dedup checks via `source='migration'` |
| Add new tier | `engine.go` — follow WriteTier1/WriteTier2/WriteTier3 pattern, add schema in `initSchema()` |
| Investigate retrieval mode fallback | `gateway.go:retrieveMemories()` + `retrieval.go:Retrieve()` |
| Investigate embedding backfill | `backfill.go:BackfillEmbeddings()` + `queryTier2MissingEmbeddings()` |

## DATA FLOW
```
Inbound message
  ↓
ExtractionService.BufferMessage() → extraction_buffer table
  ↓ (quiet gap OR token cap OR daily flush)
ExtractionService.flush() → DrainBuffer() → LLM.Extract()
  ↓                                           ↓
Tier2 (WriteTier2 ← facts)            Tier3 (WriteTier3 ← event summary)
  ↓                                           ↓
DailyCompress (cron 03:00)              marks events compressed
  → LLM.Compress → more Tier2 facts
  ↓
WeeklyDeepCompress (cron Mon 04:00)
  → merge Tier2 partitions → archive old → write merged
  → refreshTier1 → LLM.UpdateProfile → replace Tier1
  → cleanupDecayed → archive temp/debug with score ≤ 0.001
```

## CONVENTIONS
- **Mutex discipline**: `Engine.mu` protects all write operations. Read queries are lock-free (SQLite WAL handles concurrency).
- **Extraction recoverability**: On LLM failure, drained buffer messages are re-queued via `WriteBuffer()`.
- **Categories**: `identity`, `config`, `credential`, `decision`, `solution`, `event`, `conversation`, `temp`, `debug` — each has a distinct decay curve.
- **Token estimation**: `estimateTokens()` uses heuristic: Chinese chars × 1.5 + English words × 0.75.
- **Retrieval gating**: `ShouldRetrieve()` skips short/code/ack messages and triggers on question/memory keywords (Chinese + English).

## OPERATOR RUNBOOK

### Retrieval rollout defaults
- Default mode is `memory.retrieval.mode=classic`.
- `enhanced` retrieval is opt-in and should be enabled intentionally per environment.
- Invalid/unknown mode values are normalized to `classic`.

### Configure embedding and rerank providers

Minimal local setup (Ollama embedding + optional API rerank):

```json
{
  "memory": {
    "retrieval": {"mode": "enhanced"},
    "embedding": {
      "enabled": true,
      "provider": "ollama",
      "baseUrl": "http://127.0.0.1:11434",
      "model": "nomic-embed-text",
      "dimension": 768,
      "timeoutMs": 30000,
      "batchSize": 16
    },
    "rerank": {
      "enabled": true,
      "provider": "api",
      "baseUrl": "https://rerank.example.com",
      "apiKey": "${RERANK_API_KEY}",
      "model": "bge-reranker-v2-m3",
      "timeoutMs": 30000,
      "topN": 8
    }
  }
}
```

Remote API setup (embedding + rerank):

```json
{
  "memory": {
    "retrieval": {"mode": "enhanced"},
    "embedding": {
      "enabled": true,
      "provider": "api",
      "baseUrl": "https://api.example.com/v1",
      "apiKey": "${EMBEDDING_API_KEY}",
      "model": "text-embedding-3-large"
    },
    "rerank": {
      "enabled": true,
      "provider": "api",
      "baseUrl": "https://api.example.com/v1",
      "apiKey": "${RERANK_API_KEY}",
      "model": "rerank-v1",
      "topN": 8
    }
  }
}
```

### Migration and backfill operations
- Legacy migration is automatic on gateway startup when the SQLite DB is empty (`MigrateFromFiles()` imports `MEMORY.md` and daily logs).
- `BackfillEmbeddings(ctx, batchSize)` is an idempotent operation that fills missing embeddings only (`embedding IS NULL OR embedding_dim = 0`) in deterministic `id ASC` order.
- Backfill is currently an engine-level maintenance operation; run from a helper/one-off program after configuring the embedder.

### Fail-open behavior (read and write paths)
- Retrieval fail-open: in `enhanced` mode, errors trigger fallback to `classic`; if retrieval still fails, runtime reply continues without memory context.
- Write fail-open: Tier2 insert succeeds even when embedding generation fails or embedder is unavailable.
- Embedding generation is non-blocking: write first, then async embed/update.

## ANTI-PATTERNS
- **Never** write to `memories` table without going through `Engine` methods — triggers must fire for FTS sync.
- **Never** modify `memories_fts` directly — managed entirely by `AFTER INSERT/UPDATE/DELETE` triggers.
- **Never** skip `is_archived` filter — archived records must remain invisible to queries.
- `memory.go` (`MemoryStore`) is **legacy** — do not extend. All new memory work uses `Engine` (SQLite).
