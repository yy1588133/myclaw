# Draft: myclaw 记忆检索管道增强（qmd 思路移植）

## Requirements (confirmed)
- 目标: 借鉴 qmd 的本地模型检索管道，在 Go 内重建，增强 myclaw 现有记忆系统的检索能力
- 用户明确不希望使用 API 接口调用嵌入模型和重排序模型
- 保留 myclaw 现有的提取/分层/衰减/压缩能力，只增强检索管道

## Research Findings

### myclaw 当前检索管道
- `retrieval.go` — 240 行
- 流程: `extractKeywords()` (正则) → `matchProject()` → `queryRetrieveBase()` (SQL, importance DESC LIMIT 20) → FTS回退 (keywords OR, LIMIT 10) → `relevanceScore()` (衰减排序) → Top 5
- 无向量搜索、无查询扩展、无重排序
- 使用 `modernc.org/sqlite` (纯Go，CGO_ENABLED=0)
- Dockerfile: `CGO_ENABLED=0 GOOS=linux go build`

### qmd 检索管道 (完整实现细节)
1. BM25 探测 (20 results) → 强信号检测 (score≥0.85, gap≥0.15)
2. 查询扩展 (fine-tuned 1.7B model, grammar-constrained, lex/vec/hyde)
3. 类型路由: lex→FTS, vec/hyde→vector
4. RRF 融合 (k=60, 原始查询2x权重, top-rank bonus +0.05/#1 +0.02/#2-3)
5. Top 40 候选 → chunk (800 tokens, 15% overlap) → 最佳chunk选择
6. Rerank (qwen3-reranker-0.6b, cross-encoder)
7. 位置感知混合 (1-3: 75/25, 4-10: 60/40, 11+: 40/60)
8. 去重 + minScore 过滤

### Go 库景观
- **GGUF 推理**: go-skynet/go-llama.cpp (869★, 2026-02-03活跃, CGO必须)
- **ONNX 推理**: yalue/onnxruntime_go (540★, v1.25.0, CGO必须)
- **向量搜索**: sqlite-vec CGO绑定 (6880★) 或 coder/hnsw (208★, 纯Go)
- **模型**: bge-m3 GGUF (1024维, 100+语言) / embeddinggemma-300M (768维) / nomic-embed-text-v1.5 (768维)
- **重排序**: qwen3-reranker-0.6b (无GGUF!只有ONNX/PyTorch)

### 关键发现
1. **CGO 不可避免**: 所有高性能本地模型推理方案都需要 CGO（go-llama.cpp 或 onnxruntime_go）
2. **当前项目 CGO_ENABLED=0**: 引入 CGO 是重大架构变更，影响 Dockerfile、CI/CD、交叉编译
3. **Sidecar 替代方案**: llama-server 作为本地 HTTP sidecar，不需要 CGO，但增加部署复杂度
4. **Reranker GGUF 缺失**: Qwen3-Reranker 没有官方 GGUF，需用 ONNX 或 sidecar
5. **sqlite-vec 有纯 Go 方案**: WASM 绑定 (ncruces/go-sqlite3) 可保持无 CGO

## Critical Decisions Needed
1. **模型推理架构**: CGO in-process vs llama-server sidecar vs 其他
2. **嵌入模型选择**: bge-m3 (中英双语) vs embeddinggemma-300M (qmd默认)
3. **检索管道范围**: 完整管道 vs 简化版(只加向量搜索)
4. **查询扩展策略**: 本地小模型 vs 复用现有 LLM API vs 不做
5. **重排序策略**: 本地模型 vs sidecar vs 不做
6. **Docker/部署影响**: 如何处理 CGO 变更

## User Decisions (confirmed 2026-02-12)
1. **模型推理架构**: Sidecar 模式 — llama-server 作为独立进程，Go 程序保持 CGO_ENABLED=0
2. **嵌入模型**: bge-m3 (GGUF, 1024维, 100+语言, 中英双语最佳)
3. **实现范围**: 完整管道一次实现 (向量搜索 + BM25强信号 + 查询扩展 + RRF + 重排序 + 位置感知混合)
4. **查询扩展**: 通过配置文件可选择：(1) 复用 LLM API (2) 本地小模型 via sidecar
5. **部署环境**: 通过配置文件可选择 GPU 和 CPU 方案
6. **测试策略**: 待确认 (项目已有完善测试基础设施，_test.go 模式)

## Technical Design (based on decisions)
- **Sidecar**: llama-server 作为 Docker sidecar 容器或同机进程，提供 OpenAI-compatible /embedding 和 reranking 端点
- **向量存储**: 在现有 SQLite (modernc.org/sqlite) 中存储 embedding BLOB，Go 内做 brute-force cosine similarity（myclaw 规模 <10K 记忆条目完全够用）
- **RRF**: 纯 Go 实现，借鉴 qmd 的 k=60 + top-rank bonus 公式
- **BM25 强信号检测**: 复用现有 FTS5，增加 score 归一化 + 强信号判断逻辑
- **查询扩展**: 新增 `QueryExpander` 接口，支持 LLM API 和 sidecar 两种后端
- **重排序**: 通过 sidecar 的 reranking 端点实现
- **配置**: 扩展 MemoryConfig，新增 retrieval 子配置 (sidecar URL, 模型路径, GPU/CPU 选项等)

## Open Questions (resolved by defaults)
- 向量维度: 1024 (bge-m3)
- 向量存储方式: SQLite BLOB + Go 内存 cosine (规模不需要 HNSW)
- sidecar 部署: docker-compose 独立容器 (可选同容器启动)

## Scope Boundaries
- INCLUDE: 检索管道增强 (向量搜索 + 查询扩展 + 重排序 + RRF 融合)
- INCLUDE: Schema 迁移 (新增 embedding 列/表)
- INCLUDE: WriteTier2 时的自动 embedding 生成
- INCLUDE: Dockerfile / Makefile 更新
- EXCLUDE: 提取管道 (extraction.go) 不变
- EXCLUDE: 压缩管道 (compression.go) 不变
- EXCLUDE: 分层模型 (Tier1/2/3) 不变
- EXCLUDE: 衰减曲线不变
