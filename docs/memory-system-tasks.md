# myclaw 记忆系统开发任务计划

> **版本**: v1.0  
> **日期**: 2026-02-11  
> **关联文档**: [技术方案](./memory-system-design.md)  
> **代码库**: main (commit 9381b3d)

---

## 1. 开发阶段总览

| 阶段 | 名称 | 核心交付 | 预估工时 | 依赖 |
|------|------|----------|----------|------|
| P0 | 基础设施 | SQLite Engine + 表结构 + 配置扩展 | 2-3 天 | 无 |
| P1 | 存储层 | Tier 1-3 CRUD + FTS5 + 数据迁移 | 2-3 天 | P0 |
| P2 | 检索层 | 预判规则 + 分区检索 + 衰减排序 | 2 天 | P1 |
| P3 | 提取层 | LLM 客户端 + 缓冲区 + 异步提取 | 2-3 天 | P1 |
| P4 | 压缩层 | 日压缩 + 周深度压缩 + Tier 1 刷新 | 2 天 | P1, P3 |
| P5 | 集成层 | Gateway 集成 + Cron 注册 + 端到端测试 | 2-3 天 | P2, P3, P4 |
| P6 | 验收 | 冒烟测试 + 性能验证 + 文档更新 | 1-2 天 | P5 |

**总预估**: 13-19 天（单人开发）

### 依赖关系图

```
P0 (基础设施)
 ├──→ P1 (存储层)
 │     ├──→ P2 (检索层) ──────────┐
 │     ├──→ P3 (提取层) ──────────┤
 │     └──→ P4 (压缩层) ──────────┤
 │           ↑                    │
 │           └── P3 (LLM客户端)   │
 │                                ▼
 └────────────────────────────── P5 (集成层)
                                  │
                                  ▼
                                P6 (验收)
```

**可并行**: P2 和 P3 可以并行开发（均只依赖 P1）。

---

## 2. P0: 基础设施（2-3 天）

### 目标
搭建记忆系统的骨架：SQLite 连接管理、表结构初始化、配置扩展。

### 任务清单

#### T0.1 新增 SQLite 依赖

| 项目 | 内容 |
|------|------|
| 文件 | `go.mod` |
| 操作 | `go get modernc.org/sqlite` |
| 验证 | `go build ./...` 通过 |
| 说明 | 纯 Go 实现，无 CGO 依赖，跨平台兼容 |

#### T0.2 创建 `internal/memory/types.go`

定义所有记忆系统类型：

```go
// 需要定义的类型:
type Memory struct { ... }           // Tier 1/2 记忆条目
type EventEntry struct { ... }       // Tier 3 事件日志
type BufferMessage struct { ... }    // 提取缓冲区消息
type FactEntry struct { ... }        // LLM 提取结果条目
type ExtractionResult struct { ... } // LLM 提取结果
type CompressionResult struct { ... }// LLM 压缩结果
type ProfileResult struct { ... }    // LLM 画像更新结果
type ProfileEntry struct { ... }     // 画像条目
```

| 项目 | 内容 |
|------|------|
| 文件 | `internal/memory/types.go` |
| 验证 | `go vet ./internal/memory/...` 通过 |

#### T0.3 创建 `internal/memory/engine.go`

SQLite 连接管理 + 表结构初始化：

```go
// 需要实现:
type Engine struct { db *sql.DB }
func NewEngine(dbPath string) (*Engine, error)  // 打开/创建 DB + 初始化表
func (e *Engine) Close() error                  // 关闭连接
func (e *Engine) initSchema() error             // 创建 4 张表 + 索引 + FTS5 + 触发器
```

| 项目 | 内容 |
|------|------|
| 文件 | `internal/memory/engine.go` |
| 验证 | 单元测试：创建临时 DB → 验证表存在 → 关闭 |
| 注意 | 使用 `IF NOT EXISTS`，支持重复调用（幂等） |

#### T0.4 扩展 `internal/config/config.go`

新增 `MemoryConfig` 和 `ExtractionConfig` 结构体，扩展 `Config`：

| 项目 | 内容 |
|------|------|
| 文件 | `internal/config/config.go` |
| 新增类型 | `MemoryConfig`, `ExtractionConfig` |
| 修改类型 | `Config` 新增 `Memory MemoryConfig` 字段 |
| 修改函数 | `DefaultConfig()` 添加 Memory 默认值 |
| 修改函数 | `LoadConfig()` 添加 `MYCLAW_MEMORY_*` 环境变量覆盖 |
| 验证 | 现有 config 测试全部通过 + 新增 Memory 配置测试 |

#### T0.5 编写 P0 测试

| 文件 | 测试内容 |
|------|----------|
| `engine_test.go` | `TestNewEngine` — 创建/关闭/重复打开 |
| `engine_test.go` | `TestInitSchema` — 验证表结构、索引、触发器 |
| `config_test.go` | 新增 Memory 配置解析 + 环境变量覆盖测试 |

### P0 完成标准

- [x] `go build ./...` 通过
- [x] `go test ./internal/memory/...` 通过
- [x] `go test ./internal/config/...` 通过（含新增测试）
- [x] `go vet ./...` 无警告

---

## 3. P1: 存储层（2-3 天）

### 目标
实现 Tier 1-3 的完整 CRUD 操作、FTS5 全文搜索、数据迁移工具。

### 任务清单

#### T1.1 Engine CRUD 方法

在 `engine.go` 中实现核心读写方法：

```go
// Tier 1 操作
func (e *Engine) LoadTier1() (string, error)                    // 加载核心画像，拼接为 markdown
func (e *Engine) WriteTier1(entry ProfileEntry) error           // 写入单条画像

// Tier 2 操作
func (e *Engine) WriteTier2(fact FactEntry) error               // 写入知识条目
func (e *Engine) QueryTier2(project, topic string, limit int) ([]Memory, error)  // 分区查询
func (e *Engine) SearchFTS(keywords string, limit int) ([]Memory, error)         // FTS5 搜索
func (e *Engine) ArchiveMemory(id int64) error                  // 软删除（归档）
func (e *Engine) TouchMemory(id int64) error                    // 更新 last_accessed + access_count

// Tier 3 操作
func (e *Engine) WriteTier3(event EventEntry) error             // 写入事件日志
func (e *Engine) QueryEvents(date string, compressed bool) ([]EventEntry, error) // 按日期查询
func (e *Engine) MarkEventsCompressed(date string) error        // 标记已压缩

// 缓冲区操作
func (e *Engine) WriteBuffer(msg BufferMessage) error           // 写入提取缓冲
func (e *Engine) DrainBuffer(limit int) ([]BufferMessage, error)// 读取并清空缓冲
func (e *Engine) BufferTokenCount() (int, error)                // 缓冲区 token 总数
```

| 项目 | 内容 |
|------|------|
| 文件 | `internal/memory/engine.go` |
| 验证 | 每个方法对应单元测试 |

#### T1.2 FTS5 集成验证

| 项目 | 内容 |
|------|------|
| 测试 | 写入多条记忆 → FTS5 搜索 → 验证结果正确性 |
| 测试 | 更新记忆内容 → FTS5 索引同步更新 |
| 测试 | 删除记忆 → FTS5 索引同步删除 |
| 测试 | 中文分词搜索验证（unicode61 tokenizer） |

#### T1.3 创建 `internal/memory/migrate.go`

从旧文件系统迁移到新 SQLite：

```go
func MigrateFromFiles(workspace string, engine *Engine) error
// 1. MEMORY.md → Tier 1 (按行解析)
// 2. YYYY-MM-DD.md → Tier 3 (按文件归档)
```

| 项目 | 内容 |
|------|------|
| 文件 | `internal/memory/migrate.go` |
| 验证 | 创建临时目录 + 模拟旧文件 → 迁移 → 验证 DB 内容 |
| 注意 | 幂等：重复迁移不产生重复数据 |

#### T1.4 编写 P1 测试

| 文件 | 测试内容 |
|------|----------|
| `engine_test.go` | 所有 CRUD 方法的正向/边界测试 |
| `engine_test.go` | FTS5 搜索准确性测试 |
| `engine_test.go` | 并发读写安全性测试 |
| `migrate_test.go` | 迁移正确性 + 幂等性测试 |

### P1 完成标准

- [x] 所有 CRUD 方法实现且测试通过
- [x] FTS5 搜索功能正常（含中文）
- [x] 迁移工具可正确迁移旧数据
- [x] `go test -race ./internal/memory/...` 通过

---

## 4. P2: 检索层（2 天）

### 目标
实现从用户消息到记忆注入的完整检索链路：预判规则 → 关键词提取 → 分区查询 → 衰减排序 → Top-5 输出。

### 任务清单

#### T2.1 创建 `internal/memory/retrieval.go`

```go
// 预判规则
func shouldRetrieve(msg string) bool                           // 纯规则，~10ms
func isMainlyCode(msg string) bool                             // 判断消息是否主要是代码

// 关键词提取（纯规则，不用 LLM）
func extractKeywords(msg string) []string                      // 从消息中提取搜索关键词
func matchProject(msg string, projects []string) string        // 匹配已知项目名

// 衰减评分
func relevanceScore(mem Memory, daysSinceAccess float64) float64  // 分类衰减函数

// 检索主函数
func (e *Engine) Retrieve(msg string) ([]Memory, error)        // 分区查询 + FTS5 补充 + 排序
func (e *Engine) SetKnownProjects(projects []string)           // 设置已知项目列表（从 Tier 1 加载）

// 格式化
func formatMemories(memories []Memory) string                  // 格式化为上下文注入文本
```

| 项目 | 内容 |
|------|------|
| 文件 | `internal/memory/retrieval.go` |
| 验证 | 单元测试覆盖所有函数 |

#### T2.2 预判规则测试矩阵

| 输入 | 预期 | 说明 |
|------|------|------|
| `"我之前的 myclaw 配置是什么？"` | `true` | 人称代词 + 项目名 |
| `"帮我写个排序函数"` | `false` | 纯指令 |
| `"```go\nfunc main() {...}\n```"` | `false` | 代码块 |
| `"好的"` | `false` | 系统命令 |
| `"上次部署遇到的问题怎么解决的？"` | `true` | 时间引用 |
| `"你记得我喜欢用什么编辑器吗？"` | `true` | 偏好查询 |

#### T2.3 衰减函数测试

| 类别 | 0天 | 7天 | 30天 | 180天 | 地板值 |
|------|-----|-----|------|-------|--------|
| identity | 1.0 | 1.0 | 1.0 | 1.0 | 1.0 |
| decision (imp=0.8) | 0.8 | 0.78 | 0.72 | 0.43 | 0.24 |
| event (imp=0.5) | 0.5 | 0.43 | 0.25 | 0.08 | 0.05 |
| temp (imp=0.3) | 0.3 | 0.15 | 0.03 | ~0 | 0 |

#### T2.4 编写 P2 测试

| 文件 | 测试内容 |
|------|----------|
| `retrieval_test.go` | `TestShouldRetrieve` — 预判规则矩阵 |
| `retrieval_test.go` | `TestExtractKeywords` — 关键词提取 |
| `retrieval_test.go` | `TestRelevanceScore` — 衰减函数精度 |
| `retrieval_test.go` | `TestRetrieve` — 端到端检索（写入数据 → 检索 → 验证排序） |

### P2 完成标准

- [x] 预判规则覆盖所有测试用例
- [x] 衰减函数数值精度验证通过
- [x] 端到端检索测试通过（含 FTS5 补充路径）
- [x] `go test -race ./internal/memory/...` 通过

---

## 5. P3: 提取层（2-3 天）

### 目标
实现从对话到记忆的自动提取链路：LLM 客户端 → 缓冲区管理 → 触发机制 → 异步批量提取。

### 任务清单

#### T3.1 创建 `internal/memory/llm.go`

LLM 客户端接口 + OpenAI 兼容实现：

```go
// 接口
type LLMClient interface {
    Extract(conversation string) (*ExtractionResult, error)
    Compress(prompt, content string) (*CompressionResult, error)
    UpdateProfile(currentProfile, newFacts string) (*ProfileResult, error)
}

// 实现
type llmClient struct { apiKey, baseURL, model string; maxTokens int }
func NewLLMClient(cfg *config.Config) LLMClient
func (c *llmClient) complete(prompt string) (string, error)  // OpenAI 兼容 HTTP 调用
```

| 项目 | 内容 |
|------|------|
| 文件 | `internal/memory/llm.go` |
| 依赖 | 仅标准库 `net/http` + `encoding/json` |
| 验证 | Mock 测试（不依赖真实 API） |
| 注意 | `temperature: 0.3`（低温度保证提取一致性）；`response_format: json_object` |

#### T3.2 创建 `internal/memory/extraction.go`

提取服务：缓冲区管理 + 触发机制 + 批量提取：

```go
type ExtractionService struct { ... }
func NewExtractionService(engine *Engine, llm LLMClient, cfg config.ExtractionConfig) *ExtractionService
func (s *ExtractionService) BufferMessage(channel, senderID, role, content string)
func (s *ExtractionService) Start(ctx context.Context)   // 启动静默计时器 + 日终清扫
func (s *ExtractionService) Stop()                       // 停止并 flush 剩余缓冲
func (s *ExtractionService) flush()                      // 执行批量提取
```

| 项目 | 内容 |
|------|------|
| 文件 | `internal/memory/extraction.go` |
| 验证 | 单元测试覆盖三种触发条件 |

#### T3.3 提取 Prompt 模板

| Prompt | 用途 | 输出格式 |
|--------|------|----------|
| `extractionPrompt` | 从对话提取事实 + 摘要 | `ExtractionResult` JSON |
| `dailyCompressPrompt` | 日压缩归纳 | `CompressionResult` JSON |
| `weeklyCompressPrompt` | 周深度压缩去重 | `CompressionResult` JSON |
| `profileUpdatePrompt` | 画像更新 | `ProfileResult` JSON |

所有 Prompt 定义在 `llm.go` 中，作为 `const` 字符串。

#### T3.4 Token 估算工具

```go
func estimateTokens(text string) int
// 简单估算: 中文约 1.5 token/字, 英文约 0.75 token/word
// 不需要精确，用于缓冲区预算控制
```

#### T3.5 编写 P3 测试

| 文件 | 测试内容 |
|------|----------|
| `llm_test.go` | Mock HTTP Server → 验证请求格式 + 响应解析 |
| `llm_test.go` | Provider 选择逻辑（独立 provider vs 复用主 provider） |
| `extraction_test.go` | `TestBufferMessage` — 写入缓冲区 |
| `extraction_test.go` | `TestFlushOnQuietGap` — 静默超时触发 |
| `extraction_test.go` | `TestFlushOnTokenBudget` — Token 预算触发 |
| `extraction_test.go` | `TestFlushOnDailySchedule` — 日终清扫触发 |
| `extraction_test.go` | `TestExtractionResultParsing` — LLM 返回结果解析 |

### P3 完成标准

- [x] LLM 客户端可正确发送 OpenAI 兼容请求
- [x] 三种触发条件均有测试覆盖
- [x] 提取结果正确写入 Tier 2 和 Tier 3
- [x] `go test -race ./internal/memory/...` 通过

---

## 6. P4: 压缩层（2 天）

### 目标
实现日压缩（Tier 3→2）和周深度压缩（Tier 2 去重 + Tier 1 画像更新）。

### 任务清单

#### T4.1 创建 `internal/memory/compression.go`

```go
// 日压缩: Tier 3 前一天事件 → Tier 2 知识条目
func (e *Engine) DailyCompress(llm LLMClient) error

// 周深度压缩: Tier 2 按分区去重合并 + Tier 1 画像刷新
func (e *Engine) WeeklyDeepCompress(llm LLMClient) error

// 内部: 从 Tier 2 高重要性条目归纳更新 Tier 1
func (e *Engine) refreshTier1(llm LLMClient) error

// 内部: 清理衰减至 0 的 temp/debug 条目
func (e *Engine) cleanupDecayed() error
```

| 项目 | 内容 |
|------|------|
| 文件 | `internal/memory/compression.go` |
| 验证 | Mock LLM + 预置数据 → 验证压缩结果 |

#### T4.2 日压缩流程

```
1. 查询 daily_events WHERE event_date = 昨天 AND is_compressed = 0
2. 拼接所有摘要文本
3. 调用 LLM Compress(dailyCompressPrompt, text)
4. 将返回的 facts 写入 Tier 2
5. 标记事件为 is_compressed = 1
```

| 边界条件 | 处理 |
|----------|------|
| 昨天无事件 | 跳过，返回 nil |
| LLM 调用失败 | 记录日志，不标记已压缩（下次重试） |
| 返回空 facts | 仅标记已压缩，不写入 Tier 2 |

#### T4.3 周深度压缩流程

```
1. 查询所有 Tier 2 分区 (DISTINCT project, topic)
2. 对每个分区:
   a. 条目数 < 10 → 跳过
   b. 读取全部条目，调用 LLM Compress(weeklyCompressPrompt, entries)
   c. 归档旧条目 (is_archived = 1)
   d. 写入合并后的新条目
3. 调用 refreshTier1() 更新核心画像
4. 调用 cleanupDecayed() 清理衰减至 0 的条目
```

| 边界条件 | 处理 |
|----------|------|
| 分区条目 < 10 | 跳过该分区 |
| LLM 调用失败 | 跳过该分区，记录日志 |
| Tier 1 更新失败 | 保留旧 Tier 1，记录日志 |

#### T4.4 编写 P4 测试

| 文件 | 测试内容 |
|------|----------|
| `compression_test.go` | `TestDailyCompress` — 正常流程 + 空数据 + LLM 失败 |
| `compression_test.go` | `TestWeeklyDeepCompress` — 多分区去重 + 小分区跳过 |
| `compression_test.go` | `TestRefreshTier1` — 画像更新正确性 |
| `compression_test.go` | `TestCleanupDecayed` — 衰减条目清理 |

### P4 完成标准

- [x] 日压缩正确将 Tier 3 事件归纳为 Tier 2 知识
- [x] 周压缩正确去重合并 + 更新 Tier 1
- [x] 所有边界条件有测试覆盖
- [x] `go test -race ./internal/memory/...` 通过

---

## 7. P5: 集成层（2-3 天）

### 目标
将记忆系统集成到 Gateway 主流程，注册压缩 Cron Job，实现端到端功能。

### 任务清单

#### T5.1 修改 `internal/gateway/gateway.go` — 结构体扩展

```go
// Gateway 结构体新增字段
type Gateway struct {
    // ... 现有字段 ...
    memEngine   *memory.Engine             // 新记忆引擎（memory.enabled=true 时）
    extraction  *memory.ExtractionService  // 提取服务
}
```

| 项目 | 内容 |
|------|------|
| 文件 | `internal/gateway/gateway.go` |
| 变更 | Gateway 结构体 + import |

#### T5.2 修改 `NewWithOptions()` — 初始化逻辑

| 变更点 | 内容 |
|--------|------|
| 记忆引擎初始化 | `cfg.Memory.Enabled` → 创建 Engine + ExtractionService |
| 回退逻辑 | `!cfg.Memory.Enabled` → 保留现有 MemoryStore |
| LLM 客户端 | 创建记忆专用 LLMClient |
| Cron 注册 | 注册日压缩 (`0 0 3 * * *`) + 周压缩 (`0 0 4 * * 1`) |
| Cron OnJob | 新增 `__internal:memory:*` 命令分发 |

#### T5.3 修改 `buildSystemPrompt()` — Tier 1 注入

| 变更点 | 内容 |
|--------|------|
| 新系统 | `memEngine.LoadTier1()` → 注入 `# Core Memory` 段 |
| 旧系统 | 保留 `mem.GetMemoryContext()` 作为回退 |

#### T5.4 修改 `processLoop()` — 检索 + 异步提取

| 变更点 | 内容 |
|--------|------|
| 检索注入 | `shouldRetrieve()` → `memEngine.Retrieve()` → 格式化注入 prompt |
| 异步提取 | `go extraction.BufferMessage(...)` — 用户消息 + 助手回复 |

#### T5.5 修改 `Shutdown()` — 优雅关闭

```go
func (g *Gateway) Shutdown() error {
    // ... 现有关闭逻辑 ...
    if g.extraction != nil {
        g.extraction.Stop() // flush 剩余缓冲
    }
    if g.memEngine != nil {
        g.memEngine.Close() // 关闭 SQLite
    }
    return nil
}
```

#### T5.6 编写集成测试

| 文件 | 测试内容 |
|------|----------|
| `gateway_test.go` | `TestGatewayWithMemory` — memory.enabled=true 时完整流程 |
| `gateway_test.go` | `TestGatewayWithoutMemory` — memory.enabled=false 回退到旧系统 |
| `gateway_test.go` | `TestMemoryRetrievalInProcessLoop` — 消息 → 检索 → 注入 → 回复 |
| `gateway_test.go` | `TestMemoryExtractionAsync` — 回复后异步提取不阻塞 |
| `gateway_test.go` | `TestMemoryCronJobs` — 日压缩/周压缩 Cron 触发 |
| `gateway_test.go` | `TestGatewayGracefulShutdown` — 关闭时 flush 缓冲 + 关闭 DB |

### P5 完成标准

- [x] Gateway 在 `memory.enabled=true` 时正确初始化记忆系统
- [x] Gateway 在 `memory.enabled=false` 时回退到旧文件方案
- [x] processLoop 正确执行检索 + 异步提取
- [x] Cron Job 正确注册并可触发压缩
- [x] 优雅关闭时 flush 缓冲 + 关闭 DB
- [x] 现有 gateway 测试全部通过（无回归）
- [x] `go test -race ./internal/gateway/...` 通过

> 验证说明（2026-02-11）：Windows 本地受上游依赖平台差异影响，已在 Linux 容器中执行 `go test -race ./internal/gateway/...` 与 `scripts/autolab/verify.sh`，验证通过。

---

## 8. P6: 验收（1-2 天）

### 目标
端到端冒烟测试、性能验证、文档更新、CI 适配。

### 任务清单

#### T6.1 冒烟测试（手动）

| 场景 | 步骤 | 预期 |
|------|------|------|
| 首次启动 | `memory.enabled=true`，无旧数据 | DB 创建成功，表结构正确 |
| 数据迁移 | 有旧 MEMORY.md + 日志文件 | 迁移完成，Tier 1/3 有数据 |
| 对话检索 | 发送 "我之前的 myclaw 配置是什么？" | 检索到相关记忆并注入上下文 |
| 对话跳过 | 发送 "帮我写个排序函数" | 不触发检索，直接回复 |
| 异步提取 | 连续对话 → 等待 3 分钟 | 缓冲区 flush，Tier 2/3 有新数据 |
| 日压缩 | 手动触发 daily compress | Tier 3 事件压缩为 Tier 2 知识 |
| 周压缩 | 手动触发 weekly compress | Tier 2 去重，Tier 1 更新 |
| 回退 | 设置 `memory.enabled=false` | 回退到旧文件方案，功能正常 |
| 优雅关闭 | 发送 SIGTERM | 缓冲区 flush，DB 正常关闭 |

#### T6.2 性能验证

| 指标 | 目标 | 验证方式 |
|------|------|----------|
| 预判延迟 | < 10ms | `shouldRetrieve()` benchmark |
| 检索延迟 | < 20ms（含 FTS5） | `Retrieve()` benchmark，2000 条数据 |
| 提取不阻塞 | 0ms（异步） | 验证 `BufferMessage()` 立即返回 |
| DB 文件大小 | < 10MB（5000 条） | 写入 5000 条 → 检查文件大小 |
| 内存占用 | < 50MB 增量 | Gateway 启动前后对比 |

```go
// benchmark 示例
func BenchmarkShouldRetrieve(b *testing.B) { ... }
func BenchmarkRetrieve(b *testing.B) { ... }
func BenchmarkRelevanceScore(b *testing.B) { ... }
```

#### T6.3 CI 适配

| 文件 | 变更 |
|------|------|
| `.github/workflows/pr-verify.yml` | 确认 SQLite 测试在 CI 环境通过 |
| `Makefile` | 确认 `make test` 包含新测试 |
| `scripts/autolab/verify.sh` | 确认本地验证脚本兼容 |

#### T6.4 文档更新

| 文件 | 变更 |
|------|------|
| `README.md` | 新增 Memory 配置说明段 |
| `workspace/AGENTS.md` | 更新 OVERVIEW、STRUCTURE、WHERE TO LOOK、CODE MAP |
| `config.example.json`（如有） | 新增 `memory` 配置示例 |

### P6 完成标准

- [x] 所有冒烟测试场景通过
- [x] 性能指标达标
- [x] CI 流水线绿色
- [x] 文档已更新

---

## 9. 风险评估

| 风险 | 影响 | 概率 | 缓解措施 |
|------|------|------|----------|
| SQLite 并发写入冲突 | 提取/压缩同时写入导致锁等待 | 中 | 使用 WAL 模式 (`PRAGMA journal_mode=WAL`)，写操作加 mutex |
| LLM 提取结果格式异常 | JSON 解析失败，记忆丢失 | 中 | 严格 JSON schema 校验 + 失败重试 + 原始数据保留在缓冲区 |
| FTS5 中文分词精度不足 | 检索召回率低 | 低 | unicode61 tokenizer 对中文按字切分，可接受；预留 jieba 扩展点 |
| 记忆模型 API 不可用 | 提取/压缩任务堆积 | 低 | 缓冲区持久化在 SQLite，服务恢复后自动重试 |
| 迁移数据量过大 | 首次启动慢 | 低 | 迁移为一次性操作，日志文件通常 < 1000 个 |
| 衰减参数不合理 | 重要记忆过早消失或垃圾记忆堆积 | 中 | 衰减参数可配置化，上线后根据实际数据调优 |

---

## 10. 文件变更总览

### 新增文件

| 文件 | 阶段 | 说明 |
|------|------|------|
| `internal/memory/types.go` | P0 | 类型定义 |
| `internal/memory/engine.go` | P0+P1 | SQLite Engine + CRUD |
| `internal/memory/retrieval.go` | P2 | 检索逻辑 |
| `internal/memory/extraction.go` | P3 | 提取服务 |
| `internal/memory/llm.go` | P3 | LLM 客户端 |
| `internal/memory/compression.go` | P4 | 压缩管线 |
| `internal/memory/migrate.go` | P1 | 数据迁移 |
| `internal/memory/engine_test.go` | P0+P1 | Engine 测试 |
| `internal/memory/retrieval_test.go` | P2 | 检索测试 |
| `internal/memory/extraction_test.go` | P3 | 提取测试 |
| `internal/memory/llm_test.go` | P3 | LLM 客户端测试 |
| `internal/memory/compression_test.go` | P4 | 压缩测试 |
| `internal/memory/migrate_test.go` | P1 | 迁移测试 |

### 修改文件

| 文件 | 阶段 | 变更 |
|------|------|------|
| `go.mod` | P0 | 新增 `modernc.org/sqlite` 依赖 |
| `internal/config/config.go` | P0 | 新增 `MemoryConfig` + `ExtractionConfig` + 环境变量覆盖 |
| `internal/gateway/gateway.go` | P5 | Gateway 结构体 + 初始化 + processLoop + buildSystemPrompt + Shutdown |
| `internal/config/config_test.go` | P0 | 新增 Memory 配置测试 |
| `internal/gateway/gateway_test.go` | P5 | 新增记忆集成测试 |
| `README.md` | P6 | 新增 Memory 配置说明 |
| `workspace/AGENTS.md` | P6 | 更新项目知识库 |

### 保留不变

| 文件 | 说明 |
|------|------|
| `internal/memory/memory.go` | 现有 MemoryStore，作为 `memory.enabled=false` 的回退方案 |
| `internal/memory/memory_test.go` | 现有测试，确保回退方案不回归 |
