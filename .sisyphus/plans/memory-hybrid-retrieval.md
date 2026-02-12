# qmd-Inspired Hybrid Retrieval for myclaw Memory (Go, TDD)

## TL;DR

> **Quick Summary**: Rebuild myclaw memory retrieval to follow qmd's 6-stage hybrid pipeline in pure Go, while preserving `CGO_ENABLED=0`, existing SQLite driver (`modernc.org/sqlite`), and backward compatibility.
>
> **Deliverables**:
> - End-to-end 6-stage retrieval pipeline: BM25 strong-signal, query expansion, parallel FTS+vector, RRF, LLM rerank, position-aware hybrid scoring
> - Configurable dual-mode inference: local Ollama or remote API for embedding and reranking
> - SQLite schema migration + embedding BLOB storage + backfill path for existing data
> - Feature flag rollout (`classic` default, `enhanced` opt-in)
>
> **Estimated Effort**: Large
> **Parallel Execution**: YES - 7 waves
> **Critical Path**: Task 0 -> 1 -> 4 -> 7 -> 9 -> 10

---

## Context

### Original Request
"借鉴 qmd 的检索思路，在 Go 内重建实现，补全并落地当前 myclaw 的记忆系统；并支持可自主选择本地模型或 API 接口调用嵌入模型和重排序模型。"

### Interview Summary
**Key decisions confirmed**:
- Vector storage: `memories.embedding` BLOB + pure-Go cosine (no CGO, no SQLite driver swap)
- Scope: full qmd-style 6-stage retrieval pipeline
- Test strategy: TDD (RED-GREEN-REFACTOR)
- Compatibility: keep legacy behavior available via feature flag (`classic` default)

**Current codebase facts**:
- `Engine` has no embed/rerank clients today: `internal/memory/engine.go:14`
- Current retrieval is basic keyword/project/FTS fallback flow: `internal/memory/retrieval.go:128`
- Gateway retrieval integration point: `internal/gateway/gateway.go:370`
- Config currently has no embedding/rerank/retrieval-mode sections: `internal/config/config.go:39`

### Metis Review (gaps resolved in this plan)
- Added explicit schema migration task (`PRAGMA user_version`) before any new columns
- Added guardrail to avoid HTTP calls under `Engine.mu`
- Added nullable embedding model (no data-loss when embedder unavailable)
- Added explicit fallback semantics for unembedded rows
- Added feature flag rollout plan (`classic` default)
- Added edge-case coverage for dimension mismatch, zero vectors, malformed expansion/rerank responses

---

## Work Objectives

### Core Objective
Implement a robust, qmd-inspired hybrid retrieval pipeline for Tier2 memories in Go, preserving myclaw runtime stability and backward compatibility.

### Concrete Deliverables
- New retrieval runtime mode: `memory.retrieval.mode = classic|enhanced`
- Embedding/rerank config and providers (Ollama + OpenAI-compatible API)
- Vector BLOB encode/decode + cosine search utility layer
- Schema versioning + migration to add embedding-related columns
- Full 6-stage retrieval implementation with deterministic fallback logic
- Backfill mechanism for pre-existing records
- Integration and benchmark coverage

### Definition of Done
- [ ] `go test ./internal/memory/... -count=1` passes with new tests
- [ ] `go test ./internal/gateway/... -count=1` passes
- [ ] Classic mode behavior remains unchanged unless enhanced mode explicitly enabled
- [ ] Enhanced mode returns ranked results using hybrid + rerank path when dependencies are configured

### Must Have
- Full qmd-style stage flow in Go (adapted to current architecture)
- No CGO migration and no SQLite driver replacement
- Embedding persistence in SQLite using BLOB, nullable and backward-safe
- Agent-executable verification only (no manual checks)

### Must NOT Have (Guardrails)
- No edits to legacy file-memory module: `internal/memory/memory.go`
- No direct writes to `memories_fts` (trigger-managed)
- No removal of `is_archived=0` guards in retrieval queries
- No long-running HTTP calls while holding `Engine.mu`
- No rollout as default behavior without feature flag opt-in

---

## Verification Strategy (MANDATORY)

> **UNIVERSAL RULE: ZERO HUMAN INTERVENTION**
>
> Every acceptance criterion must be verifiable by agent-executed commands/tests only.

### Test Decision
- **Infrastructure exists**: YES (`go test` suite already present)
- **Automated tests**: YES (TDD)
- **Framework**: Go built-in test framework + benchmarks

### TDD Workflow for Every Coding Task
1. **RED**: Add failing unit/integration test first
2. **GREEN**: Implement minimal code to pass
3. **REFACTOR**: Clean up while tests remain green

### Agent-Executed QA Scenarios
- Primary tool: `bash` (Go tests/benchmarks)
- Secondary: `interactive_bash` only if command interactivity is required
- Evidence: test and benchmark logs saved under `.sisyphus/evidence/`

---

## Execution Strategy

### Parallel Execution Waves

```text
Wave 0 (Start Immediately)
└── Task 0: Regression baseline gate

Wave 1 (After Wave 0)
├── Task 1: Schema migration framework + embedding columns
├── Task 2: Vector primitives + benchmark harness
└── Task 3: Config model + defaults + env overrides

Wave 2 (After Wave 1)
├── Task 4: Embedder client (Ollama/API)
├── Task 5: Reranker client (API + LLM scoring fallback)
└── Task 6: Query expansion client + Stage 1 strong-signal gate

Wave 3 (After Wave 2)
└── Task 7: Stages 3-6 hybrid retrieval (parallel search, RRF, rerank, hybrid blend)

Wave 4 (After Wave 2)
└── Task 8: Write-path embedding + safe async update + backfill

Wave 5 (After Wave 3 & 4)
└── Task 9: Gateway integration + mode routing + fail-open behavior

Wave 6 (After Wave 5)
└── Task 10: End-to-end integration tests + performance regression checks

Wave 7 (After Wave 6)
└── Task 11: Documentation and AGENTS knowledge updates
```

### Dependency Matrix

| Task | Depends On | Blocks | Can Parallelize With |
|------|------------|--------|----------------------|
| 0 | None | 1,2,3 | None |
| 1 | 0 | 4,7,8 | 2,3 |
| 2 | 0 | 7,8 | 1,3 |
| 3 | 0 | 4,5,6,9 | 1,2 |
| 4 | 1,3 | 7,8 | 5,6 |
| 5 | 3 | 7 | 4,6 |
| 6 | 3 | 7 | 4,5 |
| 7 | 1,2,4,5,6 | 9,10 | None |
| 8 | 1,2,4 | 9,10 | None |
| 9 | 3,7,8 | 10 | None |
| 10 | 9 | 11 | None |
| 11 | 10 | None | None |

### Agent Dispatch Summary

| Wave | Tasks | Recommended Agent Setup |
|------|-------|-------------------------|
| 0 | 0 | `task(category="quick", load_skills=["git-master"], run_in_background=false)` |
| 1 | 1,2,3 | Dispatch in parallel with `category="unspecified-high"` |
| 2 | 4,5,6 | Parallel dispatch, each with focused tests |
| 3-5 | 7,8,9 | Sequential/paired due dependency pressure |
| 6-7 | 10,11 | Integration then docs handoff |

---

## TODOs

- [ ] 0. Establish Regression Baseline Gate

  **What to do**:
  - Run existing memory/gateway tests as immutable baseline before any changes
  - Record baseline command outputs for post-change comparison

  **Must NOT do**:
  - Do not alter existing tests to "fit" new behavior before mode gating exists

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: command-only validation, no architecture decision yet
  - **Skills**: [`git-master`]
    - `git-master`: keep baseline and subsequent commits atomic/reversible
  - **Skills Evaluated but Omitted**:
    - `deep-research`: no external knowledge needed

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Sequential start gate
  - **Blocks**: 1, 2, 3
  - **Blocked By**: None

  **References**:
  - `internal/memory/retrieval_test.go:58` - existing retrieve behavior baseline
  - `internal/memory/engine_test.go:1` - storage/concurrency baseline patterns
  - `internal/gateway/gateway_test.go:1` - gateway integration baseline

  **Acceptance Criteria**:
  - [ ] `go test ./internal/memory/... -count=1` passes
  - [ ] `go test ./internal/gateway/... -count=1` passes
  - [ ] Baseline logs captured at `.sisyphus/evidence/task-0-baseline.log`

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: Baseline memory tests stay green
    Tool: Bash
    Preconditions: Repository dependencies already available
    Steps:
      1. Run: go test ./internal/memory/... -count=1 | tee .sisyphus/evidence/task-0-memory.log
      2. Assert: exit code is 0
    Expected Result: All memory package tests pass before feature work
    Failure Indicators: any FAIL line or non-zero exit
    Evidence: .sisyphus/evidence/task-0-memory.log

  Scenario: Baseline gateway tests stay green
    Tool: Bash
    Preconditions: Same as above
    Steps:
      1. Run: go test ./internal/gateway/... -count=1 | tee .sisyphus/evidence/task-0-gateway.log
      2. Assert: exit code is 0
    Expected Result: Gateway baseline remains green before edits
    Failure Indicators: package fail output or panic traces
    Evidence: .sisyphus/evidence/task-0-gateway.log
  ```

  **Commit**: NO

---

- [ ] 1. Add Schema Versioning and Embedding Migration

  **What to do**:
  - Introduce schema migration versioning via `PRAGMA user_version`
  - Keep current `CREATE TABLE IF NOT EXISTS` initialization, then apply incremental migrations
  - Add nullable embedding fields to `memories`:
    - `embedding BLOB NULL`
    - `embedding_model TEXT NOT NULL DEFAULT ''`
    - `embedding_dim INTEGER NOT NULL DEFAULT 0`
    - `embedding_updated_at TEXT NOT NULL DEFAULT ''`
  - Ensure migration is idempotent on existing databases

  **Must NOT do**:
  - Do not drop/recreate `memories` or `memories_fts`
  - Do not require manual DB intervention

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: persistent storage migration correctness is high risk
  - **Skills**: [`git-master`]
    - `git-master`: safe sequencing and rollback-friendly commit boundary
  - **Skills Evaluated but Omitted**:
    - `playwright`: not a browser workflow

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with 2,3)
  - **Blocks**: 4,7,8
  - **Blocked By**: 0

  **References**:
  - `internal/memory/engine.go:64` - current schema bootstrap point
  - `internal/memory/engine.go:66` - memories table definition to extend safely
  - `internal/memory/engine.go:90` - FTS trigger constraints to preserve
  - `internal/memory/AGENTS.md` - anti-patterns for FTS and write paths

  **Acceptance Criteria**:
  - [ ] `TestMigrateSchemaAddsEmbeddingColumns` added and passing
  - [ ] `TestMigrateSchemaIdempotent` added and passing
  - [ ] `TestMigrateSchemaFromLegacyDB` added and passing
  - [ ] `go test ./internal/memory/... -count=1 -run 'TestMigrateSchema'` passes

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: Legacy DB auto-migrates on NewEngine
    Tool: Bash
    Preconditions: Test creates pre-migration sqlite file fixture
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestMigrateSchemaFromLegacyDB | tee .sisyphus/evidence/task-1-migrate-legacy.log
      2. Assert: exit code is 0
      3. Assert: test verifies embedding columns exist after NewEngine
    Expected Result: Existing DB upgrades automatically without data loss
    Evidence: .sisyphus/evidence/task-1-migrate-legacy.log

  Scenario: Corrupt schema version is handled safely
    Tool: Bash
    Preconditions: Negative-path unit test exists (`TestMigrateSchemaRejectsInvalidState`)
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestMigrateSchemaRejectsInvalidState | tee .sisyphus/evidence/task-1-migrate-negative.log
      2. Assert: exit code is 0 (error path intentionally validated)
    Expected Result: Engine returns clear error rather than partial migration
    Evidence: .sisyphus/evidence/task-1-migrate-negative.log
  ```

  **Commit**: YES
  - Message: `feat(memory): add schema versioning and embedding migration`
  - Files: `internal/memory/engine.go`, `internal/memory/engine_test.go`
  - Pre-commit: `go test ./internal/memory/... -count=1 -run 'TestMigrateSchema'`

---

- [ ] 2. Build Vector Primitives (BLOB Codec + Cosine + Bench)

  **What to do**:
  - Add `internal/memory/vector.go` for:
    - `EncodeVector([]float32) ([]byte, error)`
    - `DecodeVector([]byte) ([]float32, error)`
    - `CosineSimilarity(a, b []float32) (float64, error)`
  - Add strict dimension and zero-norm protections
  - Add benchmark for 10K x 384 brute-force similarity scan

  **Must NOT do**:
  - Do not use CGO libraries or external ANN index in V1

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: numeric correctness and perf thresholds are critical
  - **Skills**: [`git-master`]
    - `git-master`: keeps perf-related changes isolated and reviewable
  - **Skills Evaluated but Omitted**:
    - `deep-research`: algorithm is straightforward and already specified

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with 1,3)
  - **Blocks**: 7,8
  - **Blocked By**: 0

  **References**:
  - `internal/memory/types.go:4` - memory model baseline for vector attachment
  - `internal/memory/retrieval_test.go:127` - benchmark style to follow
  - qmd RRF/vector approach from user analysis (source context)

  **Acceptance Criteria**:
  - [ ] `TestEncodeDecodeVectorRoundTrip` passes
  - [ ] `TestCosineSimilarityKnownCases` passes
  - [ ] `TestCosineSimilarityDimensionMismatch` passes
  - [ ] `BenchmarkVectorBruteForce10k384` added
  - [ ] `go test ./internal/memory/... -count=1 -run 'Test(EncodeDecodeVector|CosineSimilarity)'` passes

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: Vector codec and cosine math correctness
    Tool: Bash
    Preconditions: New vector tests implemented
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run 'Test(EncodeDecodeVector|CosineSimilarity)' | tee .sisyphus/evidence/task-2-vector-tests.log
      2. Assert: exit code is 0
      3. Assert: tests include orthogonal/same-vector/zero-vector coverage
    Expected Result: Deterministic vector math behavior
    Evidence: .sisyphus/evidence/task-2-vector-tests.log

  Scenario: Dimension mismatch path returns controlled error
    Tool: Bash
    Preconditions: Negative test `TestCosineSimilarityDimensionMismatch`
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestCosineSimilarityDimensionMismatch | tee .sisyphus/evidence/task-2-vector-negative.log
      2. Assert: exit code is 0
    Expected Result: Function returns explicit error, no panic
    Evidence: .sisyphus/evidence/task-2-vector-negative.log
  ```

  **Commit**: YES
  - Message: `feat(memory): add vector blob codec and cosine utilities`
  - Files: `internal/memory/vector.go`, `internal/memory/vector_test.go`
  - Pre-commit: `go test ./internal/memory/... -count=1 -run 'Test(EncodeDecodeVector|CosineSimilarity)'`

---

- [ ] 3. Extend Config for Retrieval Mode + Embedding + Rerank

  **What to do**:
  - Extend `MemoryConfig` with nested config blocks:
    - `RetrievalConfig` (`mode`, thresholds, candidate/rerank limits)
    - `EmbeddingConfig` (`enabled`, `provider`, `baseURL`, `apiKey`, `model`, `dimension`, `timeoutMs`, `batchSize`)
    - `RerankConfig` (`enabled`, `provider`, `baseURL`, `apiKey`, `model`, `timeoutMs`, `topN`, `mode`)
  - Set backward-compatible defaults:
    - `mode=classic`
    - embedding/rerank disabled by default
  - Add env overrides using `MYCLAW_MEMORY_*` prefix

  **Must NOT do**:
  - Do not break loading of existing config files missing new fields

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: config contracts affect startup/runtime across all modes
  - **Skills**: [`git-master`]
    - `git-master`: isolate config schema changes cleanly
  - **Skills Evaluated but Omitted**:
    - `playwright`: not relevant

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with 1,2)
  - **Blocks**: 4,5,6,9
  - **Blocked By**: 0

  **References**:
  - `internal/config/config.go:39` - current `MemoryConfig` definition
  - `internal/config/config.go:184` - default config initialization
  - `internal/config/config.go:218` - env override pattern style
  - `README.md` config section - user-facing config examples to update later

  **Acceptance Criteria**:
  - [ ] `TestLoadConfigBackwardCompatibleMemoryDefaults` passes
  - [ ] `TestLoadConfigMemoryRetrievalEnvOverrides` passes
  - [ ] `TestDefaultConfigMemoryRetrievalClassic` passes
  - [ ] `go test ./internal/config/... -count=1` passes

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: Old config file still loads with safe defaults
    Tool: Bash
    Preconditions: Test fixture without new memory fields exists
    Steps:
      1. Run: go test ./internal/config/... -count=1 -run TestLoadConfigBackwardCompatibleMemoryDefaults | tee .sisyphus/evidence/task-3-config-backward.log
      2. Assert: exit code is 0
      3. Assert: retrieval mode resolves to classic, embedding/rerank disabled
    Expected Result: Backward compatibility guaranteed
    Evidence: .sisyphus/evidence/task-3-config-backward.log

  Scenario: Invalid retrieval mode falls back safely
    Tool: Bash
    Preconditions: Negative test `TestLoadConfigInvalidRetrievalModeFallback`
    Steps:
      1. Run: go test ./internal/config/... -count=1 -run TestLoadConfigInvalidRetrievalModeFallback | tee .sisyphus/evidence/task-3-config-negative.log
      2. Assert: exit code is 0
    Expected Result: Invalid mode does not crash startup; fallback applied
    Evidence: .sisyphus/evidence/task-3-config-negative.log
  ```

  **Commit**: YES
  - Message: `feat(config): add memory retrieval embedding rerank settings`
  - Files: `internal/config/config.go`, `internal/config/config_test.go`
  - Pre-commit: `go test ./internal/config/... -count=1`

---

- [ ] 4. Implement Embedder Client (Ollama + OpenAI-Compatible API)

  **What to do**:
  - Add `internal/memory/embedder.go` with interface:
    - `Embed(ctx context.Context, text string) ([]float32, error)`
    - `EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)`
  - Implement OpenAI-compatible `/v1/embeddings` request/response handling
  - Support provider modes:
    - local (`ollama` base URL)
    - remote (`api` base URL + key)
  - Add timeouts and structured error wrapping

  **Must NOT do**:
  - Do not modify `LLMClient` interface in `llm.go`

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: network client behavior directly affects write/retrieval reliability
  - **Skills**: [`git-master`, `deep-research`]
    - `git-master`: isolate network client and tests in one atomic unit
    - `deep-research`: verify endpoint compatibility details when needed
  - **Skills Evaluated but Omitted**:
    - `playwright`: not browser-based

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with 5,6)
  - **Blocks**: 7,8
  - **Blocked By**: 1,3

  **References**:
  - `internal/memory/llm.go:55` - interface/factory style precedent
  - `internal/memory/llm.go:69` - client constructor pattern to mirror
  - `internal/memory/llm_test.go:1` - httptest mock style for HTTP clients
  - Ollama OpenAI-compatible embeddings endpoint docs: `https://github.com/ollama/ollama/blob/main/docs/openai.md`

  **Acceptance Criteria**:
  - [ ] `TestEmbedderEmbedSingleOpenAICompat` passes
  - [ ] `TestEmbedderEmbedBatchOpenAICompat` passes
  - [ ] `TestEmbedderHandlesTimeout` passes
  - [ ] `go test ./internal/memory/... -count=1 -run 'TestEmbedder'` passes

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: Embed single and batch through mocked OpenAI-compatible API
    Tool: Bash
    Preconditions: httptest-backed embedder tests implemented
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run 'TestEmbedder(EmbedSingleOpenAICompat|EmbedBatchOpenAICompat)' | tee .sisyphus/evidence/task-4-embedder-happy.log
      2. Assert: exit code is 0
      3. Assert: request payload includes model + input and parses vectors
    Expected Result: Embedder client returns consistent vectors for both modes
    Evidence: .sisyphus/evidence/task-4-embedder-happy.log

  Scenario: Embedder timeout/error path handled gracefully
    Tool: Bash
    Preconditions: Negative test with slow/failed mock server
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestEmbedderHandlesTimeout | tee .sisyphus/evidence/task-4-embedder-negative.log
      2. Assert: exit code is 0
    Expected Result: Controlled wrapped error, no panic
    Evidence: .sisyphus/evidence/task-4-embedder-negative.log
  ```

  **Commit**: YES
  - Message: `feat(memory): add embedding client for ollama and api`
  - Files: `internal/memory/embedder.go`, `internal/memory/embedder_test.go`
  - Pre-commit: `go test ./internal/memory/... -count=1 -run 'TestEmbedder'`

---

- [ ] 5. Implement Reranker Client (API Rerank + LLM Scoring Fallback)

  **What to do**:
  - Add `internal/memory/reranker.go` with interface:
    - `Rerank(ctx context.Context, query string, docs []string) ([]RerankScore, error)`
  - Implement provider strategy:
    - API rerank endpoint when available
    - LLM scoring fallback via strict JSON output prompt when rerank endpoint unavailable
  - Normalize outputs to stable score range for downstream blend

  **Must NOT do**:
  - Do not hard-fail retrieval if reranker unavailable; retrieval must degrade gracefully

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: ranking quality and failure handling directly affect user-visible answers
  - **Skills**: [`git-master`, `deep-research`]
    - `git-master`: preserves atomic changes around new ranking contract
    - `deep-research`: ensures endpoint compatibility expectations are correct
  - **Skills Evaluated but Omitted**:
    - `playwright`: not relevant

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with 4,6)
  - **Blocks**: 7
  - **Blocked By**: 3

  **References**:
  - `internal/memory/llm.go:61` - reusable HTTP client shape
  - `internal/memory/retrieval.go:158` - where rerank output will affect sorting
  - qmd rerank stage spec from user analysis (cross-encoder stage)

  **Acceptance Criteria**:
  - [ ] `TestRerankerAPIEndpoint` passes
  - [ ] `TestRerankerLLMFallback` passes
  - [ ] `TestRerankerMalformedResponse` passes
  - [ ] `go test ./internal/memory/... -count=1 -run 'TestReranker'` passes

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: API rerank endpoint returns normalized ranking scores
    Tool: Bash
    Preconditions: Mock rerank endpoint tests implemented
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestRerankerAPIEndpoint | tee .sisyphus/evidence/task-5-reranker-happy.log
      2. Assert: exit code is 0
      3. Assert: result count equals doc count and score ordering is deterministic
    Expected Result: Reranker returns stable score list
    Evidence: .sisyphus/evidence/task-5-reranker-happy.log

  Scenario: Malformed rerank payload triggers controlled fallback/error
    Tool: Bash
    Preconditions: Negative-path tests present
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestRerankerMalformedResponse | tee .sisyphus/evidence/task-5-reranker-negative.log
      2. Assert: exit code is 0
    Expected Result: No panic; clear error/fallback behavior validated
    Evidence: .sisyphus/evidence/task-5-reranker-negative.log
  ```

  **Commit**: YES
  - Message: `feat(memory): add reranker client with api and llm fallback`
  - Files: `internal/memory/reranker.go`, `internal/memory/reranker_test.go`
  - Pre-commit: `go test ./internal/memory/... -count=1 -run 'TestReranker'`

---

- [ ] 6. Add Query Expansion Client + Stage-1 Strong Signal Gate

  **What to do**:
  - Add `internal/memory/query_expander.go` with constrained JSON-output expansion
  - Add retrieval stage 1 logic: BM25 strong signal short-circuit
    - trigger when top score >= configured threshold and score gap >= configured gap
  - Add query sanitization for FTS (`MATCH`) safety

  **Must NOT do**:
  - Do not run expansion when strong signal gate already satisfied
  - Do not allow unsanitized expansion strings into `MATCH`

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: this stage controls cost/latency and correctness branching
  - **Skills**: [`git-master`]
    - `git-master`: keeps retrieval-branch logic changes reviewable
  - **Skills Evaluated but Omitted**:
    - `deep-research`: algorithm and thresholds already decided

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with 4,5)
  - **Blocks**: 7
  - **Blocked By**: 3

  **References**:
  - `internal/memory/retrieval.go:128` - current entry point for retrieval pipeline
  - `internal/memory/engine.go:241` - FTS search behavior and ordering baseline
  - qmd strong-signal and expansion rules from user analysis

  **Acceptance Criteria**:
  - [ ] `TestStrongSignalShortCircuit` passes
  - [ ] `TestQueryExpansionSanitization` passes
  - [ ] `TestStrongSignalSkipsExpansion` passes
  - [ ] `go test ./internal/memory/... -count=1 -run 'Test(StrongSignal|QueryExpansion)'` passes

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: Strong BM25 hit bypasses expansion and vector stages
    Tool: Bash
    Preconditions: Seeded test fixture with obvious lexical winner
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestStrongSignalShortCircuit | tee .sisyphus/evidence/task-6-strong-signal.log
      2. Assert: exit code is 0
      3. Assert: test confirms expansion client is not called
    Expected Result: Cost-saving short-circuit works exactly as designed
    Evidence: .sisyphus/evidence/task-6-strong-signal.log

  Scenario: Unsafe expansion tokens are sanitized before FTS MATCH
    Tool: Bash
    Preconditions: Negative test with malformed expansion content
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestQueryExpansionSanitization | tee .sisyphus/evidence/task-6-expansion-negative.log
      2. Assert: exit code is 0
    Expected Result: No SQL/FTS syntax failure from expansion outputs
    Evidence: .sisyphus/evidence/task-6-expansion-negative.log
  ```

  **Commit**: YES
  - Message: `feat(memory): add query expansion and bm25 strong-signal gate`
  - Files: `internal/memory/query_expander.go`, `internal/memory/retrieval_enhanced_stage1_test.go`
  - Pre-commit: `go test ./internal/memory/... -count=1 -run 'Test(StrongSignal|QueryExpansion)'`

---

- [ ] 7. Implement Enhanced Retrieval Stages 3-6 (Parallel Hybrid + RRF + Rerank + Position Blend)

  **What to do**:
  - Add enhanced retrieval implementation (recommended file: `internal/memory/pipeline.go`)
  - Stage 3: run FTS and vector candidate collection in parallel for original + expanded queries
  - Stage 4: fuse with RRF (`k=60`, original query x2, top-rank bonus)
  - Stage 5: rerank fused candidates via reranker client
  - Stage 6: apply position-aware hybrid blend by rank bucket
  - Keep classic retrieval path intact and callable

  **Must NOT do**:
  - Do not fetch BLOB embeddings in non-vector queries
  - Do not remove `TouchMemory` side effect on returned results

  **Recommended Agent Profile**:
  - **Category**: `ultrabrain`
    - Reason: multi-stage ranking math + concurrency + fallback matrix is logic-dense
  - **Skills**: [`git-master`]
    - `git-master`: keeps algorithmic changes in tightly scoped commit(s)
  - **Skills Evaluated but Omitted**:
    - `playwright`: no browser surface

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 3
  - **Blocks**: 9,10
  - **Blocked By**: 1,2,4,5,6

  **References**:
  - `internal/memory/retrieval.go:128` - existing retrieval function to preserve/classic fallback
  - `internal/memory/engine.go:511` - `scanMemories` behavior (avoid broad breakage)
  - `internal/memory/engine.go:250` - current FTS query baseline
  - `internal/memory/retrieval_test.go:58` - retrieval tests to extend for enhanced path

  **Acceptance Criteria**:
  - [ ] `TestEnhancedRetrieveHybridRRF` passes
  - [ ] `TestEnhancedRetrieveWithRerankBlend` passes
  - [ ] `TestEnhancedRetrieveFallbackWithoutEmbeddings` passes
  - [ ] `go test ./internal/memory/... -count=1 -run 'TestEnhancedRetrieve'` passes

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: Hybrid retrieval ranks semantic match above keyword-only noise
    Tool: Bash
    Preconditions: Integration test fixture with lexical distractors and semantic target
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestEnhancedRetrieveHybridRRF | tee .sisyphus/evidence/task-7-hybrid-happy.log
      2. Assert: exit code is 0
      3. Assert: top-ranked memory id matches semantic target fixture
    Expected Result: RRF + vector signal improves ranking quality
    Evidence: .sisyphus/evidence/task-7-hybrid-happy.log

  Scenario: Missing reranker/embedding gracefully degrades
    Tool: Bash
    Preconditions: Negative tests with clients disabled/unavailable
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestEnhancedRetrieveFallbackWithoutEmbeddings | tee .sisyphus/evidence/task-7-hybrid-negative.log
      2. Assert: exit code is 0
    Expected Result: Retrieval still returns results via safe fallback path
    Evidence: .sisyphus/evidence/task-7-hybrid-negative.log
  ```

  **Commit**: YES
  - Message: `feat(memory): implement enhanced hybrid retrieval pipeline`
  - Files: `internal/memory/pipeline.go`, `internal/memory/retrieval_enhanced_test.go`
  - Pre-commit: `go test ./internal/memory/... -count=1 -run 'TestEnhancedRetrieve'`

---

- [ ] 8. Integrate Embedding Write Path + Safe Async Update + Backfill

  **What to do**:
  - Preserve `WriteTier2` compatibility, add non-breaking path to capture inserted row id
  - Ensure write transaction completes first; embedding HTTP call runs after lock release
  - Add engine method to persist embedding by memory id (idempotent update)
  - Integrate into extraction/compression writes where available
  - Add backfill worker for rows with `embedding IS NULL OR embedding_dim=0`

  **Must NOT do**:
  - Do not make write fail if embedding generation fails
  - Do not hold `Engine.mu` while calling external embedding APIs

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: correctness across write lock discipline + async side effects
  - **Skills**: [`git-master`]
    - `git-master`: isolate write-path safety changes and rollback points
  - **Skills Evaluated but Omitted**:
    - `deep-research`: implementation is internal policy-driven

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 4
  - **Blocks**: 9,10
  - **Blocked By**: 1,2,4

  **References**:
  - `internal/memory/engine.go:177` - current `WriteTier2` lock and insert logic
  - `internal/memory/extraction.go:141` - extraction flush write path
  - `internal/memory/compression.go:10` - daily compression write path
  - `internal/memory/compression.go:40` - weekly compression write path

  **Acceptance Criteria**:
  - [ ] `TestWriteTier2WithoutEmbedderStillSucceeds` passes
  - [ ] `TestWriteTier2EmbeddingUpdateOutsideLock` passes
  - [ ] `TestBackfillEmbeddingsIdempotent` passes
  - [ ] `go test ./internal/memory/... -count=1 -run 'Test(WriteTier2.*Embed|BackfillEmbeddings)'` passes

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: Write succeeds even when embedding provider unavailable
    Tool: Bash
    Preconditions: Negative-path test with mock embedder outage
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestWriteTier2WithoutEmbedderStillSucceeds | tee .sisyphus/evidence/task-8-write-negative.log
      2. Assert: exit code is 0
      3. Assert: written row exists and embedding remains NULL
    Expected Result: No memory loss on embedder outage
    Evidence: .sisyphus/evidence/task-8-write-negative.log

  Scenario: Backfill fills missing embeddings deterministically
    Tool: Bash
    Preconditions: Fixture DB with mixed embedded/unembedded rows
    Steps:
      1. Run: go test ./internal/memory/... -count=1 -run TestBackfillEmbeddingsIdempotent | tee .sisyphus/evidence/task-8-backfill-happy.log
      2. Assert: exit code is 0
      3. Assert: second backfill run makes zero additional updates
    Expected Result: Backfill is safe and idempotent
    Evidence: .sisyphus/evidence/task-8-backfill-happy.log
  ```

  **Commit**: YES
  - Message: `feat(memory): add safe embedding write path and backfill worker`
  - Files: `internal/memory/engine.go`, `internal/memory/extraction.go`, `internal/memory/compression.go`, `internal/memory/backfill.go`, tests
  - Pre-commit: `go test ./internal/memory/... -count=1 -run 'Test(WriteTier2.*Embed|BackfillEmbeddings)'`

---

- [ ] 9. Wire Retrieval Mode and Clients into Gateway

  **What to do**:
  - Add embedder/reranker/expander initialization in gateway setup
  - Route retrieval by mode in `processLoop`:
    - `classic` -> existing retrieval path
    - `enhanced` -> new hybrid pipeline path
  - Ensure fail-open behavior: if enhanced path errors, fallback to classic retrieval and continue response generation

  **Must NOT do**:
  - Do not block agent reply when enhanced retrieval fails

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: runtime orchestration path with user-facing reliability impact
  - **Skills**: [`git-master`]
    - `git-master`: keeps integration edges and fallback policy traceable
  - **Skills Evaluated but Omitted**:
    - `playwright`: not UI interaction

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 5
  - **Blocks**: 10
  - **Blocked By**: 3,7,8

  **References**:
  - `internal/gateway/gateway.go:96` - Gateway struct fields to extend
  - `internal/gateway/gateway.go:115` - initialization flow for memory services
  - `internal/gateway/gateway.go:370` - retrieval invocation point in message loop

  **Acceptance Criteria**:
  - [ ] `TestGatewayClassicModeUsesRetrieve` passes
  - [ ] `TestGatewayEnhancedModeUsesHybridRetrieve` passes
  - [ ] `TestGatewayEnhancedFailureFallsBackClassic` passes
  - [ ] `go test ./internal/gateway/... -count=1 -run 'TestGateway(ClassicMode|EnhancedMode|EnhancedFailureFallsBackClassic)'` passes

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: Enhanced mode retrieval path is invoked when configured
    Tool: Bash
    Preconditions: Gateway tests with retrieval mode fixtures
    Steps:
      1. Run: go test ./internal/gateway/... -count=1 -run TestGatewayEnhancedModeUsesHybridRetrieve | tee .sisyphus/evidence/task-9-gateway-happy.log
      2. Assert: exit code is 0
      3. Assert: test verifies enhanced path invocation counter > 0
    Expected Result: Gateway routes correctly by mode
    Evidence: .sisyphus/evidence/task-9-gateway-happy.log

  Scenario: Enhanced errors degrade to classic without breaking reply flow
    Tool: Bash
    Preconditions: Test injects enhanced retrieval error
    Steps:
      1. Run: go test ./internal/gateway/... -count=1 -run TestGatewayEnhancedFailureFallsBackClassic | tee .sisyphus/evidence/task-9-gateway-negative.log
      2. Assert: exit code is 0
      3. Assert: outbound response still emitted
    Expected Result: Fail-open reliability guaranteed
    Evidence: .sisyphus/evidence/task-9-gateway-negative.log
  ```

  **Commit**: YES
  - Message: `feat(gateway): route memory retrieval by classic and enhanced modes`
  - Files: `internal/gateway/gateway.go`, `internal/gateway/gateway_test.go`
  - Pre-commit: `go test ./internal/gateway/... -count=1`

---

- [ ] 10. Run Full Verification + Performance Guardrails

  **What to do**:
  - Execute full memory/gateway test suites
  - Run targeted benchmark(s) for vector scan and retrieve latency
  - Compare against baseline logs from Task 0
  - Verify classic mode parity and enhanced mode expected improvements on semantic fixtures

  **Must NOT do**:
  - Do not waive regressions without explicit documented rationale

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: command-heavy verification and evidence collection
  - **Skills**: [`git-master`]
    - `git-master`: stage and commit only after full green verification
  - **Skills Evaluated but Omitted**:
    - `deep-research`: no new external info required

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 6
  - **Blocks**: 11
  - **Blocked By**: 9

  **References**:
  - `.sisyphus/evidence/task-0-memory.log` - baseline comparison source
  - `internal/memory/retrieval_test.go:127` - benchmark precedent
  - `scripts/autolab/verify.sh` - project verification policy alignment

  **Acceptance Criteria**:
  - [ ] `go test ./internal/memory/... -count=1` passes
  - [ ] `go test ./internal/gateway/... -count=1` passes
  - [ ] `go test ./internal/memory/... -bench BenchmarkRetrieve -benchtime=3s -run '^$'` executes
  - [ ] `go test ./internal/memory/... -bench BenchmarkVectorBruteForce10k384 -benchtime=3s -run '^$'` executes
  - [ ] Evidence logs stored under `.sisyphus/evidence/task-10-*.log`

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: Full package verification green
    Tool: Bash
    Preconditions: All prior tasks merged locally
    Steps:
      1. Run: go test ./internal/memory/... -count=1 | tee .sisyphus/evidence/task-10-memory-full.log
      2. Run: go test ./internal/gateway/... -count=1 | tee .sisyphus/evidence/task-10-gateway-full.log
      3. Assert: both exit codes are 0
    Expected Result: No functional regression in core paths
    Evidence: .sisyphus/evidence/task-10-memory-full.log, .sisyphus/evidence/task-10-gateway-full.log

  Scenario: Performance guardrails remain acceptable
    Tool: Bash
    Preconditions: Benchmark targets implemented
    Steps:
      1. Run: go test ./internal/memory/... -bench BenchmarkVectorBruteForce10k384 -benchtime=3s -run '^$' | tee .sisyphus/evidence/task-10-bench-vector.log
      2. Assert: benchmark output present and no runtime errors
      3. Optionally enforce threshold in benchmark test helper
    Expected Result: Brute-force vector scan remains within agreed budget
    Evidence: .sisyphus/evidence/task-10-bench-vector.log
  ```

  **Commit**: YES
  - Message: `test(memory): add integration and performance verification for hybrid retrieval`
  - Files: test and benchmark artifacts only
  - Pre-commit: `go test ./internal/memory/... -count=1 && go test ./internal/gateway/... -count=1`

---

- [ ] 11. Update Knowledge and Operator Docs

  **What to do**:
  - Update memory module guidance and project docs for new retrieval modes/config
  - Document migration behavior, fallback strategy, and recommended models
  - Add operator notes for backfill execution and troubleshooting

  **Must NOT do**:
  - Do not leave undocumented config keys that impact runtime behavior

  **Recommended Agent Profile**:
  - **Category**: `writing`
    - Reason: clarity and operational correctness in docs
  - **Skills**: [`git-master`]
    - `git-master`: keep doc updates in dedicated final commit
  - **Skills Evaluated but Omitted**:
    - `deep-research`: content derives from implemented behavior

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 7
  - **Blocks**: None
  - **Blocked By**: 10

  **References**:
  - `internal/memory/AGENTS.md` - module conventions and anti-patterns
  - `README.md` memory/config sections - user-facing setup
  - `internal/config/config.go:39` - authoritative config structure

  **Acceptance Criteria**:
  - [ ] Docs include retrieval mode (`classic`/`enhanced`) and default behavior
  - [ ] Docs include embedding/rerank config examples for Ollama and API modes
  - [ ] Docs include backfill and failure fallback notes
  - [ ] Markdown lint/readability check passes (if project has markdown checks)

  **Agent-Executed QA Scenarios**:

  ```bash
  Scenario: Documentation includes all newly introduced config fields
    Tool: Bash
    Preconditions: Docs updated
    Steps:
      1. Run: grep -n "retrieval" README.md internal/memory/AGENTS.md | tee .sisyphus/evidence/task-11-docs-retrieval.log
      2. Run: grep -n "embedding" README.md internal/memory/AGENTS.md | tee .sisyphus/evidence/task-11-docs-embedding.log
      3. Assert: expected sections exist and are non-empty
    Expected Result: Operators can configure and run enhanced retrieval from docs alone
    Evidence: .sisyphus/evidence/task-11-docs-retrieval.log, .sisyphus/evidence/task-11-docs-embedding.log

  Scenario: Docs capture fallback and failure behavior
    Tool: Bash
    Preconditions: Failure-mode documentation added
    Steps:
      1. Run: grep -n "fallback" README.md internal/memory/AGENTS.md | tee .sisyphus/evidence/task-11-docs-fallback.log
      2. Assert: at least one section describes enhanced->classic fallback and embedder outage behavior
    Expected Result: Operational playbook includes negative scenarios
    Evidence: .sisyphus/evidence/task-11-docs-fallback.log
  ```

  **Commit**: YES
  - Message: `docs(memory): document enhanced retrieval modes config and backfill`
  - Files: `README.md`, `internal/memory/AGENTS.md` (and related markdown only)
  - Pre-commit: N/A (markdown-only verification)

---

## Commit Strategy

| After Task | Message | Files | Verification |
|------------|---------|-------|--------------|
| 1 | `feat(memory): add schema versioning and embedding migration` | engine + migration tests | targeted migrate tests |
| 2 | `feat(memory): add vector blob codec and cosine utilities` | vector utilities/tests | vector tests |
| 3 | `feat(config): add memory retrieval embedding rerank settings` | config + config tests | config tests |
| 4 | `feat(memory): add embedding client for ollama and api` | embedder client/tests | embedder tests |
| 5 | `feat(memory): add reranker client with api and llm fallback` | reranker client/tests | reranker tests |
| 6 | `feat(memory): add query expansion and bm25 strong-signal gate` | stage1/expander code/tests | stage1 tests |
| 7 | `feat(memory): implement enhanced hybrid retrieval pipeline` | pipeline + tests | enhanced retrieval tests |
| 8 | `feat(memory): add safe embedding write path and backfill worker` | write-path/backfill/tests | write+backfill tests |
| 9 | `feat(gateway): route memory retrieval by classic and enhanced modes` | gateway + tests | gateway tests |
| 10 | `test(memory): add integration and performance verification for hybrid retrieval` | integration/bench tests | full test + bench |
| 11 | `docs(memory): document enhanced retrieval modes config and backfill` | markdown docs | doc checks |

---

## Success Criteria

### Verification Commands

```bash
go test ./internal/memory/... -count=1
go test ./internal/gateway/... -count=1
go test ./internal/memory/... -bench BenchmarkRetrieve -benchtime=3s -run '^$'
go test ./internal/memory/... -bench BenchmarkVectorBruteForce10k384 -benchtime=3s -run '^$'
```

### Final Checklist

- [ ] All tasks completed with evidence logs in `.sisyphus/evidence/`
- [ ] Classic mode remains behavior-compatible with current production behavior
- [ ] Enhanced mode works with both local Ollama and remote API providers
- [ ] Migration works for fresh and existing DBs without manual intervention
- [ ] No AGENTS.md anti-pattern violations
- [ ] No human/manual verification steps required
