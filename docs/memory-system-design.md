# myclaw 记忆系统技术方案

> **版本**: v1.0  
> **日期**: 2026-02-11  
> **状态**: 设计阶段  
> **分支**: main (commit 9381b3d)

---

## 1. 概述

### 1.1 背景

当前 myclaw 的记忆系统基于文件存储（`internal/memory/memory.go`），采用 `MEMORY.md`（长期记忆）+ 日期文件（`YYYY-MM-DD.md`，每日日志）的方式。该方案存在以下局限：

- **无结构化检索**：只能全量读取，无法按 project/topic 分区查询
- **无容量控制**：日志文件无限增长，无压缩/归纳机制
- **无遗忘机制**：所有记忆权重相同，无法区分重要性和时效性
- **无智能提取**：依赖外部手动写入，无自动从对话中提取事实的能力
- **上下文注入粗糙**：`GetMemoryContext()` 将全部长期记忆 + 最近 7 天日志一次性注入 system prompt

### 1.2 目标

设计并实现一套四层分级记忆系统，具备：

| 能力 | 描述 |
|------|------|
| 分层存储 | Tier 0-3 四层，按容量、检索方式、更新频率分级 |
| 结构化检索 | 按 project/topic 分区索引 + FTS5 全文搜索 |
| 智能提取 | 从对话中自动提取事实，异步写入知识库 |
| 遗忘机制 | 分类衰减权重，防止记忆无限膨胀 |
| 压缩管线 | 日压缩（Tier 3→2）+ 周深度压缩（Tier 2 去重 + Tier 1 更新） |
| 模型分离 | 记忆提取/压缩使用独立轻量模型，不影响主对话链路 |
| 零阻塞 | 记忆写入完全异步，检索通过规则预判 + SQL 查询实现（无 LLM 调用） |

### 1.3 设计原则

1. **渐进式迁移**：新系统与现有 `MemoryStore` 接口兼容，可逐步切换
2. **最小依赖**：仅新增 SQLite（`modernc.org/sqlite`，纯 Go 实现，无 CGO）
3. **配置驱动**：`memory.enabled` 控制开关，关闭时回退到现有文件方案
4. **可观测性**：关键操作均有 `[memory]` 前缀日志

---

## 2. 整体架构

```
┌─────────────────────────────────────────────────────────┐
│                     myclaw Gateway                       │
│                                                         │
│  User Msg ─→ ① 提取 project/topic 标签                  │
│              ② 分区检索 Tier 1+2 相关记忆                │
│              ③ 注入上下文 → LLM 回复                     │
│              ④ 后台: 写入 Tier 3 事件日志                │
│              ⑤ 后台: 提取即时事实 → Tier 2 (如有)        │
│                                                         │
│  Daily Cron ─→ Tier 3 日志压缩 → Tier 2 知识             │
│  Weekly Cron ─→ Tier 2 去重合并 → Tier 1 画像更新         │
│                                                         │
├─────────────────────────────────────────────────────────┤
│                   Memory Engine                          │
│                                                         │
│  Tier 0: 工作记忆      (~20条, 在 session history 中)    │
│  Tier 1: 核心画像      (~100条, 永驻 system prompt)      │
│  Tier 2: 知识库        (~2000条, 分区索引, 定期压缩)     │
│  Tier 3: 事件日志      (无限, 按日归档, 定期压缩进T2)    │
│                                                         │
│  Storage: SQLite (memories + fts + daily_events)         │
│  Retrieval: 分区SQL(主) → FTS5(辅)                      │
│  Compression: Daily + Weekly LLM batch                   │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

### 2.1 各层概览

| 层级 | 名称 | 容量 | 检索方式 | 更新频率 | 存储位置 |
|------|------|------|----------|----------|----------|
| Tier 0 | 工作记忆 | ~20条 | 永驻上下文 | 每轮对话 | agentsdk-go session history |
| Tier 1 | 核心画像 | ~100条 | 永驻上下文 | 每日更新 | SQLite `memories` 表 (tier=1) |
| Tier 2 | 知识库 | ~2000条 | 结构化检索 | 持续写入+定期压缩 | SQLite `memories` 表 (tier=2) + FTS5 |
| Tier 3 | 事件日志 | 无限 | 按需搜索 | 每日归档 | SQLite `daily_events` 表 |

### 2.2 数据流全景

```
用户在 Telegram/Feishu 发送消息
  │
  ▼
┌──────────────────────────────┐
│ Channel (Telegram/Feishu)    │
│ handleMessage()              │
└──────────┬───────────────────┘
           │ bus.InboundMessage
           ▼
┌──────────────────────────────┐
│ Gateway processLoop          │
│                              │
│ ┌──────────────────────────┐ │
│ │ 预判: shouldRetrieve()   │ │  ← ~10ms 纯规则
│ └──────────┬───────────────┘ │
│       Yes  │  No             │
│            │   │             │
│   ┌────────▼┐  │             │
│   │SQL 检索 │  │             │  ← ~5ms
│   │Top-5记忆│  │             │
│   └────────┬┘  │             │
│            │   │             │
│   ┌────────▼───▼───────────┐ │
│   │ 构建上下文:            │ │
│   │ SystemPrompt (含Tier1) │ │
│   │ + 检索到的记忆 (Tier2) │ │
│   │ + 用户消息             │ │
│   └────────┬───────────────┘ │
│            ▼                 │
│   runAgent() → LLM 回复      │  ← ~3秒 (仅1轮)
│            │                 │
│            ├──→ 发送回复      │
│            │                 │
│            └──→ go Extract() │  ← 后台异步，不阻塞
│                              │
└──────────────────────────────┘
```

---

## 3. 存储设计

### 3.1 存储选型

| 方案 | 优势 | 劣势 | 结论 |
|------|------|------|------|
| 文件系统（现状） | 简单、无依赖 | 无索引、无事务、无 FTS | ❌ 不满足需求 |
| SQLite | 零部署、嵌入式、FTS5、事务 | 单写者锁 | ✅ 最佳选择 |
| PostgreSQL/MySQL | 功能强大 | 需要外部服务、运维成本 | ❌ 过度设计 |

**选定方案**: SQLite，使用 `modernc.org/sqlite`（纯 Go 实现，无 CGO 依赖）

**数据库文件位置**: `~/.myclaw/data/memory.db`

### 3.2 表结构

#### 3.2.1 memories 表（Tier 1 + Tier 2）

```sql
CREATE TABLE IF NOT EXISTS memories (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    tier        INTEGER NOT NULL DEFAULT 2,          -- 1=核心画像, 2=知识库
    project     TEXT    NOT NULL DEFAULT '_global',   -- 分区键: 项目名或 '_global'
    topic       TEXT    NOT NULL DEFAULT '_general',  -- 二级分区: 话题标签
    category    TEXT    NOT NULL DEFAULT 'event',     -- 衰减类别: identity/config/credential/decision/solution/event/conversation/temp/debug
    content     TEXT    NOT NULL,                     -- 记忆内容
    importance  REAL    NOT NULL DEFAULT 0.5,         -- 重要性 [0.0, 1.0]
    source      TEXT    NOT NULL DEFAULT 'extraction',-- 来源: extraction/compression/manual/migration
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    last_accessed TEXT  NOT NULL DEFAULT (datetime('now')),
    access_count INTEGER NOT NULL DEFAULT 0,
    is_archived INTEGER NOT NULL DEFAULT 0            -- 软删除标记
);

-- 分区索引（主检索路径）
CREATE INDEX IF NOT EXISTS idx_memories_partition
    ON memories(tier, project, topic, is_archived);

-- 类别索引（衰减计算）
CREATE INDEX IF NOT EXISTS idx_memories_category
    ON memories(category, last_accessed);

-- 时间索引（压缩任务）
CREATE INDEX IF NOT EXISTS idx_memories_created
    ON memories(created_at);
```

#### 3.2.2 memories_fts 虚拟表（全文搜索）

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    content,
    content='memories',
    content_rowid='id',
    tokenize='unicode61'
);

-- 同步触发器
CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, content) VALUES (new.id, new.content);
END;

CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, content) VALUES('delete', old.id, old.content);
END;

CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, content) VALUES('delete', old.id, old.content);
    INSERT INTO memories_fts(rowid, content) VALUES (new.id, new.content);
END;
```

#### 3.2.3 daily_events 表（Tier 3 事件日志）

```sql
CREATE TABLE IF NOT EXISTS daily_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_date  TEXT    NOT NULL,                     -- 'YYYY-MM-DD'
    channel     TEXT    NOT NULL DEFAULT 'unknown',   -- telegram/feishu/system
    sender_id   TEXT    NOT NULL DEFAULT '',
    summary     TEXT    NOT NULL,                     -- 对话摘要
    raw_tokens  INTEGER NOT NULL DEFAULT 0,           -- 原始 token 数（用于预算控制）
    is_compressed INTEGER NOT NULL DEFAULT 0,         -- 是否已压缩进 Tier 2
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_events_date
    ON daily_events(event_date, is_compressed);
```

#### 3.2.4 extraction_buffer 表（提取缓冲区）

```sql
CREATE TABLE IF NOT EXISTS extraction_buffer (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    channel     TEXT    NOT NULL,
    sender_id   TEXT    NOT NULL DEFAULT '',
    role        TEXT    NOT NULL,                     -- 'user' | 'assistant'
    content     TEXT    NOT NULL,
    token_count INTEGER NOT NULL DEFAULT 0,           -- 估算 token 数
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_buffer_created
    ON extraction_buffer(created_at);
```

---

## 4. 各层详细设计

### 4.1 Tier 0: 工作记忆

| 属性 | 值 |
|------|-----|
| 容量 | ~20 条消息 |
| 存储 | agentsdk-go session history（内存） |
| 检索 | 永驻上下文，自动包含在 LLM 请求中 |
| 更新 | 每轮对话自动更新 |
| 实现变更 | **无**——由 agentsdk-go `api.Request.SessionID` 自动管理 |

Tier 0 是 LLM 的原生对话历史，由 `agentsdk-go` 的 session 机制维护。当前 Gateway 的 `processLoop` 已通过 `msg.SessionKey()` 传递 session ID，无需额外实现。

### 4.2 Tier 1: 核心画像

| 属性 | 值 |
|------|-----|
| 容量 | ~100 条 |
| 存储 | SQLite `memories` 表 (tier=1) |
| 检索 | 永驻 system prompt，Gateway 启动时加载 + 每日刷新 |
| 更新 | Weekly Cron 从 Tier 2 归纳更新 |
| 类别 | identity, config, credential（永不衰减） |

**内容示例**：
- 身份信息："用户名 yangyang，全栈工程师，偏好 Go/TypeScript"
- 项目列表："活跃项目: myclaw (Go), stellarlink (React)"
- 行为指令："回复偏好中文，技术讨论用英文术语"
- 技术栈："主力: Go 1.24, React 19, PostgreSQL; 部署: Docker + GitHub Actions"

**加载机制**：
```go
// Gateway 启动时 + 每日定时刷新
func (e *Engine) LoadTier1() (string, error) {
    rows, err := e.db.Query(`
        SELECT content FROM memories
        WHERE tier = 1 AND is_archived = 0
        ORDER BY importance DESC
        LIMIT 100
    `)
    // 拼接为 markdown 格式注入 system prompt
}
```

### 4.3 Tier 2: 知识库

| 属性 | 值 |
|------|-----|
| 容量 | ~2000 条（压缩后控制在 5000 以内） |
| 存储 | SQLite `memories` 表 (tier=2) + FTS5 索引 |
| 检索 | 分区 SQL 查询（主）→ FTS5 全文搜索（辅） |
| 更新 | 实时提取写入 + Daily Cron 从 Tier 3 压缩写入 |
| 类别 | decision, solution, event, conversation, temp, debug |

**分区策略**：
```
memories (tier=2)
  ├── project="myclaw"
  │   ├── topic="architecture"
  │   ├── topic="deployment"
  │   └── topic="debugging"
  ├── project="stellarlink"
  │   ├── topic="frontend"
  │   └── topic="api"
  └── project="_global"
      ├── topic="preferences"
      ├── topic="tools"
      └── topic="_general"
```

**容量控制**：通过压缩机制，即使日均 2000 条对话，Tier 2 知识库也能控制在 5000 条以内。在这个规模下，分区索引 + FTS5 完全够用，不需要 embedding 语义搜索。只有当单个分区超过 500 条时，才值得在该分区内引入 embedding 辅助（预留扩展点，v1 不实现）。

### 4.4 Tier 3: 事件日志

| 属性 | 值 |
|------|-----|
| 容量 | 无限 |
| 存储 | SQLite `daily_events` 表 |
| 检索 | 按需搜索（按日期 + 关键词） |
| 更新 | 每轮对话后异步写入摘要 |
| 压缩 | Daily Cron 将前一天日志压缩为 Tier 2 知识条目 |

**写入内容**：每轮对话的摘要（非原始消息），包含：
- 日期、渠道、发送者
- 对话主题概要
- 关键决策/结论
- 估算 token 数（用于提取预算控制）

---

## 5. 检索策略

### 5.1 分层检索流程

```
用户发送消息
  ↓
━━━ 阶段 0: 零成本层（永远执行）━━━

  Tier 1 核心画像已在 system prompt 中
  无决策，无延迟，无成本

━━━ 阶段 1: 轻量预判（~10ms，无 LLM 调用）━━━

  规则引擎快速判断: 这条消息是否可能需要记忆？

  触发条件 (命中任一则检索):
  ├── 包含人称代词: "我的"、"我之前"、"你记得"
  ├── 包含时间引用: "上次"、"之前"、"昨天"
  ├── 包含项目名词: 匹配已知 project 列表
  ├── 是提问句式: "什么"、"怎么"、"为什么"、"?"
  └── 包含偏好/配置词: "喜欢"、"设置"、"配置"、"密码"

  不触发 (直接跳过检索):
  ├── 纯指令: "帮我写个函数"、"翻译这段话"
  ├── 代码内容: 消息主体是代码块
  └── 系统命令: "继续"、"好的"、"确认"

━━━ 阶段 2: 快速检索（仅当阶段1触发）━━━

  方式: 分区 SQL 查询 (无 LLM 调用, ~5ms)

  从消息中提取关键词 (纯规则, 不用LLM)
  → SQL: WHERE project=? OR content MATCH ?
  → 按 relevanceScore 排序
  → Top-5 注入上下文
```

### 5.2 预判规则引擎

```go
// shouldRetrieve 纯规则判断，无 LLM 调用
func shouldRetrieve(msg string) bool {
    // 短消息直接跳过
    if len(msg) < 5 {
        return false
    }

    // 代码块占主体则跳过
    if isMainlyCode(msg) {
        return false
    }

    // 系统命令跳过
    skipPatterns := []string{"继续", "好的", "确认", "ok", "yes", "no"}
    msgLower := strings.ToLower(strings.TrimSpace(msg))
    for _, p := range skipPatterns {
        if msgLower == p {
            return false
    }
}
```

---

## 11. LLM 客户端设计

### 11.1 接口定义

记忆系统的 LLM 调用链路完全独立于主对话链路，只需单次结构化补全。

```go
// LLMClient 记忆专用 LLM 客户端接口
type LLMClient interface {
    // Extract 从对话中提取事实和摘要
    Extract(conversation string) (*ExtractionResult, error)
    // Compress 压缩/归纳记忆条目
    Compress(prompt, content string) (*CompressionResult, error)
    // UpdateProfile 更新核心画像
    UpdateProfile(currentProfile, newFacts string) (*ProfileResult, error)
}

type ExtractionResult struct {
    Facts   []FactEntry `json:"facts"`
    Summary string      `json:"summary"`
}

type CompressionResult struct {
    Facts []FactEntry `json:"facts"`
}

type ProfileResult struct {
    Entries []ProfileEntry `json:"entries"`
}

type FactEntry struct {
    Content    string  `json:"content"`
    Project    string  `json:"project"`
    Topic      string  `json:"topic"`
    Category   string  `json:"category"`
    Importance float64 `json:"importance"`
}

type ProfileEntry struct {
    Content  string `json:"content"`
    Category string `json:"category"`
}
```

### 11.2 实现

```go
// llmClient 使用 OpenAI 兼容 API 的简单实现
type llmClient struct {
    apiKey    string
    baseURL   string
    model     string
    maxTokens int
}

func NewLLMClient(cfg *config.Config) LLMClient {
    c := &llmClient{}

    // 优先使用 memory 专用 provider
    if cfg.Memory.Provider != nil {
        c.apiKey = cfg.Memory.Provider.APIKey
        c.baseURL = cfg.Memory.Provider.BaseURL
    } else {
        c.apiKey = cfg.Provider.APIKey
        c.baseURL = cfg.Provider.BaseURL
    }

    // 优先使用 memory 专用 model
    if cfg.Memory.Model != "" {
        c.model = cfg.Memory.Model
    } else {
        c.model = cfg.Agent.Model
    }

    if cfg.Memory.MaxTokens > 0 {
        c.maxTokens = cfg.Memory.MaxTokens
    } else {
        c.maxTokens = cfg.Agent.MaxTokens
    }

    return c
}

// Extract 调用 LLM 提取事实
func (c *llmClient) Extract(conversation string) (*ExtractionResult, error) {
    prompt := fmt.Sprintf(extractionPrompt, conversation)
    resp, err := c.complete(prompt)
    if err != nil {
        return nil, fmt.Errorf("extraction: %w", err)
    }

    var result ExtractionResult
    if err := json.Unmarshal([]byte(resp), &result); err != nil {
        return nil, fmt.Errorf("parse extraction result: %w", err)
    }
    return &result, nil
}

// complete 发送单次补全请求（OpenAI 兼容）
func (c *llmClient) complete(prompt string) (string, error) {
    body := map[string]interface{}{
        "model": c.model,
        "messages": []map[string]string{
            {"role": "user", "content": prompt},
        },
        "max_tokens":  c.maxTokens,
        "temperature": 0.3, // 低温度保证提取一致性
        "response_format": map[string]string{
            "type": "json_object",
        },
    }
    // POST to baseURL + "/chat/completions"
    // 解析 response.choices[0].message.content
    // ...
}
```

---

## 12. 迁移方案

### 12.1 迁移策略：渐进式切换

新旧系统并存，通过 `memory.enabled` 配置开关控制：

```
memory.enabled = false  →  使用现有 MemoryStore（文件方案）
memory.enabled = true   →  使用新 Engine（SQLite 方案）
```

### 12.2 数据迁移

现有数据需要一次性迁移到新系统：

| 源 | 目标 | 迁移方式 |
|----|------|----------|
| `MEMORY.md` | Tier 1 `memories` 表 (tier=1) | 按行解析，每行一条记忆 |
| `YYYY-MM-DD.md` 日志文件 | Tier 3 `daily_events` 表 | 按文件归档，每文件一条事件 |

```go
// MigrateFromFiles 从旧文件系统迁移到新 SQLite
func MigrateFromFiles(workspace string, engine *Engine) error {
    memDir := filepath.Join(workspace, "memory")

    // 1. 迁移 MEMORY.md → Tier 1
    if data, err := os.ReadFile(filepath.Join(memDir, "MEMORY.md")); err == nil {
        lines := strings.Split(string(data), "\n")
        for _, line := range lines {
            line = strings.TrimSpace(line)
            if line == "" || strings.HasPrefix(line, "#") {
                continue
            }
            engine.db.Exec(`
                INSERT INTO memories (tier, project, topic, category, content, importance, source)
                VALUES (1, '_global', '_profile', 'identity', ?, 1.0, 'migration')
            `, line)
        }
    }

    // 2. 迁移日期文件 → Tier 3
    entries, _ := os.ReadDir(memDir)
    for _, e := range entries {
        name := e.Name()
        if !strings.HasSuffix(name, ".md") || name == "MEMORY.md" {
            continue
        }
        date := strings.TrimSuffix(name, ".md")
        data, err := os.ReadFile(filepath.Join(memDir, name))
        if err != nil {
            continue
        }
        content := strings.TrimSpace(string(data))
        if content == "" {
            continue
        }
        engine.db.Exec(`
            INSERT INTO daily_events (event_date, channel, summary, is_compressed)
            VALUES (?, 'migration', ?, 0)
        `, date, content)
    }

    log.Printf("[memory] migration complete")
    return nil
}
```

### 12.3 回退方案

如果新系统出现问题：
1. 设置 `memory.enabled = false`
2. Gateway 自动回退到 `MemoryStore` 文件方案
3. SQLite 数据库保留，不删除
4. 修复后重新启用即可

---

## 13. 包结构设计

```
internal/memory/
├── memory.go           # 现有 MemoryStore（保留，作为回退）
├── engine.go           # Engine: SQLite 连接 + 表初始化 + CRUD
├── retrieval.go        # 检索逻辑: shouldRetrieve + Retrieve + relevanceScore
├── extraction.go       # 提取服务: ExtractionService + 缓冲区 + 触发器
├── compression.go      # 压缩管线: DailyCompress + WeeklyDeepCompress
├── llm.go              # LLM 客户端: LLMClient 接口 + OpenAI 兼容实现
├── types.go            # 类型定义: Memory, FactEntry, ExtractionResult 等
├── migrate.go          # 迁移工具: MigrateFromFiles
├── memory_test.go      # 现有测试（保留）
├── engine_test.go      # Engine 单元测试
├── retrieval_test.go   # 检索逻辑测试
├── extraction_test.go  # 提取服务测试
├── compression_test.go # 压缩管线测试
└── llm_test.go         # LLM 客户端测试（mock）
```

---

## 14. 设计总结

| 设计维度 | 方案 |
|----------|------|
| 整体架构 | 四层记忆 (Tier 0-3)，分级存储与检索 |
| 存储方案 | SQLite (`modernc.org/sqlite`) + 分区索引 + FTS5 |
| 提取策略 | 静默窗口批量 + Token 预算 + 日终清扫 |
| 检索策略 | 规则预判（~10ms）+ 分区 SQL（~5ms）+ FTS5 补充 |
| 遗忘机制 | 分类衰减 + 地板值 + 访问复习效应 |
| 模型分离 | 对话模型 / 记忆轻量模型，链路完全独立 |
| 压缩管线 | 日压缩 (Tier 3→2) + 周深度压缩 (Tier 2 去重 + Tier 1 更新) |
| 配置设计 | 扩展 `config.json`，支持独立 provider + 环境变量覆盖 |
| 集成方式 | Gateway `processLoop` 钩子，零侵入式 |
| 迁移方案 | 渐进式切换，`memory.enabled` 开关，支持回退 |

### 5.3 分区检索查询

```go
func (e *Engine) Retrieve(msg string) ([]Memory, error) {
    // 1. 从消息中提取关键词（纯规则）
    keywords := extractKeywords(msg)
    project := matchProject(msg, e.knownProjects)

    // 2. 分区查询
    query := `
        SELECT id, tier, project, topic, category, content,
               importance, last_accessed, access_count, created_at
        FROM memories
        WHERE tier = 2 AND is_archived = 0
    `
    args := []interface{}{}

    if project != "" {
        query += ` AND (project = ? OR project = '_global')`
        args = append(args, project)
    }

    query += ` ORDER BY importance DESC LIMIT 20`

    rows, err := e.db.Query(query, args...)
    // ...

    // 3. 如果分区查询结果不足，FTS5 补充
    if len(results) < 5 && len(keywords) > 0 {
        ftsQuery := strings.Join(keywords, " OR ")
        ftsRows, _ := e.db.Query(`
            SELECT m.* FROM memories m
            JOIN memories_fts f ON m.id = f.rowid
            WHERE memories_fts MATCH ?
              AND m.tier = 2 AND m.is_archived = 0
            LIMIT 10
        `, ftsQuery)
        // 合并去重
    }

    // 4. 按 relevanceScore 排序，取 Top-5
    sort.Slice(results, func(i, j int) bool {
        return relevanceScore(results[i]) > relevanceScore(results[j])
    })
    if len(results) > 5 {
        results = results[:5]
    }

    // 5. 更新 last_accessed
    for _, m := range results {
        e.touchMemory(m.ID)
    }

    return results, nil
}
```

---

## 6. 提取策略

### 6.1 提取流程概览

记忆提取是**完全异步**的——不阻塞用户对话，用户无感知。

```
对话消息 → 写入 extraction_buffer → [触发条件满足] → LLM 批量提取 → 写入 Tier 2/3
```

### 6.2 缓冲区机制

每轮对话的用户消息和助手回复都写入 `extraction_buffer` 表，累积到触发条件后批量提取。

**触发条件**（任一命中即触发）：

| 条件 | 阈值 | 说明 |
|------|------|------|
| 静默超时 | 3 分钟无新消息 | 最常见触发方式 |
| Token 预算 | 缓冲区接近 6000 tokens | 防止单批过大影响提取质量 |
| 日终清扫 | 每日 03:00 | 兜底，清空当日剩余缓冲 |

**上限 6000 tokens 的原因**：即使模型上下文支持 128K，一次性输入太多对话给轻量模型做提取，提取质量会下降——关键事实容易被淹没。控制每批 3K-6K tokens 是提取精度和效率的最佳平衡点。

### 6.3 提取实现

```go
// ExtractionService 管理提取缓冲和触发
type ExtractionService struct {
    engine    *Engine
    llm       LLMClient          // 记忆专用模型
    quietGap  time.Duration      // 静默超时（默认 3m）
    tokenCap  int                // token 预算（默认 6000）
    mu        sync.Mutex
    timer     *time.Timer        // 静默计时器
}

// BufferMessage 将消息写入缓冲区
func (s *ExtractionService) BufferMessage(channel, senderID, role, content string) {
    tokenCount := estimateTokens(content)
    s.engine.db.Exec(`
        INSERT INTO extraction_buffer (channel, sender_id, role, content, token_count)
        VALUES (?, ?, ?, ?, ?)
    `, channel, senderID, role, content, tokenCount)

    // 重置静默计时器
    s.resetQuietTimer()

    // 检查 token 预算
    if s.bufferTokenCount() >= s.tokenCap {
        go s.flush()
    }
}

// flush 执行批量提取
func (s *ExtractionService) flush() {
    s.mu.Lock()
    defer s.mu.Unlock()

    // 1. 读取缓冲区
    messages := s.drainBuffer()
    if len(messages) == 0 {
        return
    }

    // 2. 拼接对话文本
    conversation := formatConversation(messages)

    // 3. 调用记忆模型提取
    extracted, err := s.llm.Extract(conversation)
    if err != nil {
        log.Printf("[memory] extraction error: %v", err)
        return
    }

    // 4. 写入 Tier 2 知识条目
    for _, fact := range extracted.Facts {
        s.engine.WriteTier2(fact)
    }

    // 5. 写入 Tier 3 事件摘要
    s.engine.WriteTier3(EventEntry{
        Date:    time.Now().Format("2006-01-02"),
        Channel: messages[0].Channel,
        Summary: extracted.Summary,
        Tokens:  totalTokens(messages),
    })

    // 6. 清空已处理的缓冲区
    s.clearBuffer(messages)

    log.Printf("[memory] extracted %d facts, 1 event summary from %d messages",
        len(extracted.Facts), len(messages))
}
```

### 6.4 LLM 提取 Prompt

```go
const extractionPrompt = `你是一个记忆提取引擎。从以下对话中提取值得长期记住的事实。

规则：
1. 只提取明确陈述的事实，不要推测
2. 每条事实独立成句，不超过 100 字
3. 为每条事实标注 project、topic、category、importance
4. category 可选: identity/config/credential/decision/solution/event/conversation/temp/debug
5. importance 范围 [0.0, 1.0]，身份信息=1.0，技术决策=0.8，一般事件=0.5，临时信息=0.2
6. 同时生成一段不超过 200 字的对话摘要

输出 JSON 格式：
{
  "facts": [
    {"content": "...", "project": "...", "topic": "...", "category": "...", "importance": 0.8}
  ],
  "summary": "..."
}

对话内容：
%s`
```

---

## 7. 压缩管线

### 7.1 压缩概览

| 任务 | 频率 | 触发方式 | 输入 | 输出 |
|------|------|----------|------|------|
| 日压缩 | 每日 03:00 | Cron Job | Tier 3 前一天未压缩事件 | Tier 2 知识条目 |
| 周深度压缩 | 每周一 04:00 | Cron Job | Tier 2 全量（按分区） | Tier 2 去重合并 + Tier 1 画像更新 |

### 7.2 日压缩（Tier 3 → Tier 2）

```go
// DailyCompress 将前一天的事件日志压缩为知识条目
func (e *Engine) DailyCompress(llm LLMClient) error {
    yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

    // 1. 读取前一天未压缩的事件
    events, err := e.db.Query(`
        SELECT id, summary FROM daily_events
        WHERE event_date = ? AND is_compressed = 0
    `, yesterday)

    // 2. 拼接所有摘要
    allSummaries := joinSummaries(events)
    if allSummaries == "" {
    return nil
}
```

---

## 8. 遗忘机制：分类衰减

### 8.1 设计理念

不采用经典艾宾浩斯遗忘曲线，而是按记忆类别设定不同的衰减速率和地板值（最低保留权重），防止重要记忆完全消失。

### 8.2 衰减函数

```go
func relevanceScore(mem Memory, daysSinceAccess float64) float64 {
    switch mem.Category {

    case "identity", "config", "credential":
        // 身份/配置/凭证: 永不衰减
        return mem.Importance

    case "decision", "solution":
        // 技术决策/解决方案: 极慢衰减 (半衰期 180 天)
        decay := math.Exp(-0.004 * daysSinceAccess)
        return mem.Importance * (0.3 + 0.7*decay) // 最低保留30%

    case "event", "conversation":
        // 事件/对话摘要: 中等衰减 (半衰期 30 天)
        decay := math.Exp(-0.023 * daysSinceAccess)
        return mem.Importance * (0.1 + 0.9*decay) // 最低保留10%

    case "temp", "debug":
        // 临时/调试信息: 快速衰减 (半衰期 7 天)
        decay := math.Exp(-0.099 * daysSinceAccess)
        return mem.Importance * decay // 可衰减至0
    }

    return mem.Importance
}
```

### 8.3 衰减曲线可视化

```
权重
1.0 |■ ■ ■ ■ ■ ■ ■ ■ ■ ■ ■ ■ ■ ■ ■ ■   identity/config (不衰减)
    |
0.8 |● ● ● ●                               decision/solution
    |          ● ● ●
0.6 |                ● ●
    |                    ● ●
0.4 |◆ ◆                    ● ● ● ● ● ●   ← 最低30%
    |    ◆ ◆                                event/conversation
0.2 |        ◆ ◆
    |△ △        ◆ ◆ ◆ ◆ ◆ ◆               ← 最低10%
0.0 |    △ △ △                              temp/debug (可归零)
    └────────────────────────────────────→ 天数
     0    7   14   30   60   90  120  180
```

### 8.4 集成位置

衰减机制仅影响 Tier 2 检索排序，修改点唯一：

```
当前: ORDER BY importance DESC
改为: ORDER BY relevanceScore(importance, category, days_since_access) DESC
```

附加行为：
- 每次记忆被检索并实际使用时，更新 `last_accessed` 和 `access_count`
- 这自然实现了"越常用越记得牢"的复习效应
- 衰减至 0 的 `temp/debug` 条目在周压缩时自动归档

---

## 9. 配置设计

### 9.1 Config 结构扩展

在现有 `config.Config` 中新增 `Memory` 字段：

```go
// config.go 新增

type MemoryConfig struct {
    Enabled    bool                `json:"enabled"`
    Model      string              `json:"model,omitempty"`      // 记忆模型，省略时用主模型
    MaxTokens  int                 `json:"maxTokens,omitempty"`  // 记忆模型 maxTokens
    Provider   *ProviderConfig     `json:"provider,omitempty"`   // 可选独立 provider，省略时复用主 provider
    Extraction ExtractionConfig    `json:"extraction"`
}

type ExtractionConfig struct {
    QuietGap    string `json:"quietGap,omitempty"`    // 静默超时，默认 "3m"
    TokenBudget float64 `json:"tokenBudget,omitempty"` // token 预算比例 (0-1)，默认 0.6 → 约 6000 tokens
    DailyFlush  string `json:"dailyFlush,omitempty"`  // 日终清扫时间，默认 "03:00"
}

// Config 结构扩展
type Config struct {
    Agent    AgentConfig    `json:"agent"`
    Channels ChannelsConfig `json:"channels"`
    Provider ProviderConfig `json:"provider"`
    Tools    ToolsConfig    `json:"tools"`
    Gateway  GatewayConfig  `json:"gateway"`
    Memory   MemoryConfig   `json:"memory"`    // ← 新增
}
```

### 9.2 配置示例

```json
{
  "provider": {
    "type": "openai",
    "apiKey": "sk-xxx",
    "baseUrl": "https://right.codes/codex/v1"
  },
  "agent": {
    "model": "gpt-5.3-codex",
    "maxTokens": 128000
  },
  "memory": {
    "enabled": true,
    "model": "gpt-5",
    "maxTokens": 128000,
    "provider": {
      "type": "openai",
      "apiKey": "sk-xxx",
      "baseUrl": "https://cpa.maoyuos.eu.cc/v1"
    },
    "extraction": {
      "quietGap": "3m",
      "tokenBudget": 0.6,
      "dailyFlush": "03:00"
    }
  }
}
```

### 9.3 设计要点

| 要点 | 说明 |
|------|------|
| `memory.provider` 可选 | 省略时复用主 `provider`，只换 `model` |
| 零基建变更 | 大多数 OpenAI 兼容 API 网关同一 endpoint 支持多模型，只需切换 model 名称 |
| 链路独立 | 记忆模型调用极简——单次结构化补全，不需要工具调用、流式输出、多轮对话 |
| 可迁移 | 模型可随时切换，未来可迁移到本地模型实现零成本记忆 |

### 9.4 环境变量覆盖

| 变量 | 说明 |
|------|------|
| `MYCLAW_MEMORY_ENABLED` | 启用/禁用记忆系统 (`true`/`false`) |
| `MYCLAW_MEMORY_MODEL` | 记忆模型名称 |
| `MYCLAW_MEMORY_API_KEY` | 记忆模型 API Key（独立于主 provider） |
| `MYCLAW_MEMORY_BASE_URL` | 记忆模型 API Base URL |

---

## 10. 集成设计

### 10.1 集成点总览

新记忆系统需要在以下位置与现有代码集成：

| 集成点 | 文件 | 函数 | 变更类型 |
|--------|------|------|----------|
| Gateway 初始化 | `internal/gateway/gateway.go` | `NewWithOptions()` | 新增 Engine 初始化 |
| System Prompt 构建 | `internal/gateway/gateway.go` | `buildSystemPrompt()` | 替换 `mem.GetMemoryContext()` 为 Tier 1 加载 |
| 消息处理循环 | `internal/gateway/gateway.go` | `processLoop()` | 新增预判+检索+异步提取 |
| Cron 注册 | `internal/gateway/gateway.go` | `NewWithOptions()` | 注册日压缩/周压缩 Cron Job |
| 配置加载 | `internal/config/config.go` | `LoadConfig()` | 新增 Memory 配置 + 环境变量覆盖 |

### 10.2 Gateway 初始化变更

```go
// gateway.go NewWithOptions() 中新增

func NewWithOptions(cfg *config.Config, opts Options) (*Gateway, error) {
    g := &Gateway{cfg: cfg}

    // Message bus
    g.bus = bus.NewMessageBus(config.DefaultBufSize)

    // Memory — 新系统
    if cfg.Memory.Enabled {
        dbPath := filepath.Join(config.ConfigDir(), "data", "memory.db")
        engine, err := memory.NewEngine(dbPath)
        if err != nil {
            return nil, fmt.Errorf("create memory engine: %w", err)
        }
        g.memEngine = engine

        // 记忆模型客户端
        memLLM := memory.NewLLMClient(cfg)
        g.extraction = memory.NewExtractionService(engine, memLLM, cfg.Memory.Extraction)
    } else {
        // 回退到旧文件方案
        g.mem = memory.NewMemoryStore(cfg.Agent.Workspace)
    }

    // ... 其余初始化不变 ...
}
```

### 10.3 processLoop 变更

```go
func (g *Gateway) processLoop(ctx context.Context) {
    for {
        select {
        case msg := <-g.bus.Inbound:
            log.Printf("[gateway] inbound from %s/%s: %s",
                msg.Channel, msg.SenderID, truncate(msg.Content, 80))

            // ===== 新增: 记忆检索 =====
            var memoryContext string
            if g.memEngine != nil && shouldRetrieve(msg.Content) {
                memories, err := g.memEngine.Retrieve(msg.Content)
                if err != nil {
                    log.Printf("[memory] retrieve error: %v", err)
                } else if len(memories) > 0 {
                    memoryContext = formatMemories(memories)
                }
            }

            // 构建带记忆的 prompt
            prompt := msg.Content
            if memoryContext != "" {
                prompt = fmt.Sprintf("[相关记忆]\n%s\n\n[用户消息]\n%s",
                    memoryContext, msg.Content)
            }

            result, err := g.runAgent(ctx, prompt, msg.SessionKey())
            if err != nil {
                log.Printf("[gateway] agent error: %v", err)
                result = "Sorry, I encountered an error processing your message."
            }

            if result != "" {
                g.bus.Outbound <- bus.OutboundMessage{
                    Channel: msg.Channel,
                    ChatID:  msg.ChatID,
                    Content: result,
                }
            }

            // ===== 新增: 异步记忆提取 =====
            if g.extraction != nil {
                go func(m bus.InboundMessage, reply string) {
                    g.extraction.BufferMessage(m.Channel, m.SenderID, "user", m.Content)
                    if reply != "" {
                        g.extraction.BufferMessage(m.Channel, "", "assistant", reply)
                    }
                }(msg, result)
            }

        case <-ctx.Done():
            return
        }
    }
}
```

### 10.4 buildSystemPrompt 变更

```go
func (g *Gateway) buildSystemPrompt() string {
    var sb strings.Builder

    // AGENTS.md + SOUL.md（不变）
    if data, err := os.ReadFile(filepath.Join(g.cfg.Agent.Workspace, "AGENTS.md")); err == nil {
        sb.Write(data)
        sb.WriteString("\n\n")
    }
    if data, err := os.ReadFile(filepath.Join(g.cfg.Agent.Workspace, "SOUL.md")); err == nil {
        sb.Write(data)
        sb.WriteString("\n\n")
    }

    // ===== 变更: 记忆上下文 =====
    if g.memEngine != nil {
        // 新系统: 加载 Tier 1 核心画像
        if profile, err := g.memEngine.LoadTier1(); err == nil && profile != "" {
            sb.WriteString("# Core Memory\n")
            sb.WriteString(profile)
            sb.WriteString("\n\n")
        }
    } else if g.mem != nil {
        // 旧系统: 回退
        if memCtx := g.mem.GetMemoryContext(); memCtx != "" {
            sb.WriteString(memCtx)
        }
    }

    return sb.String()
}
```

### 10.5 Cron Job 注册

```go
// 在 NewWithOptions() 中，cron 初始化之后新增：

if g.memEngine != nil {
    // 日压缩: 每天 03:00
    g.cron.AddJob("memory-daily-compress", cron.Schedule{
        Kind: "cron",
        Expr: "0 0 3 * * *",
    }, cron.Payload{
        Message: "__internal:memory:daily_compress",
    })

    // 周深度压缩: 每周一 04:00
    g.cron.AddJob("memory-weekly-compress", cron.Schedule{
        Kind: "cron",
        Expr: "0 0 4 * * 1",
    }, cron.Payload{
        Message: "__internal:memory:weekly_compress",
    })
}

// 在 cron.OnJob 中新增内部命令处理：
g.cron.OnJob = func(job cron.CronJob) (string, error) {
    switch job.Payload.Message {
    case "__internal:memory:daily_compress":
        return "ok", g.memEngine.DailyCompress(memLLM)
    case "__internal:memory:weekly_compress":
        return "ok", g.memEngine.WeeklyDeepCompress(memLLM)
    default:
        // 原有逻辑
        result, err := runAgent(job.Payload.Message)
        // ...
    }
}
```

### 7.3 周深度压缩（Tier 2 去重 + Tier 1 更新）

```go
// WeeklyDeepCompress 按分区去重合并 Tier 2，更新 Tier 1 画像
func (e *Engine) WeeklyDeepCompress(llm LLMClient) error {
    // 1. 获取所有分区
    partitions, _ := e.db.Query(`
        SELECT DISTINCT project, topic FROM memories
        WHERE tier = 2 AND is_archived = 0
    `)

    for _, p := range partitions {
        // 2. 读取分区内所有条目
        entries, _ := e.db.Query(`
            SELECT id, content, category, importance, created_at
            FROM memories
            WHERE tier = 2 AND project = ? AND topic = ? AND is_archived = 0
            ORDER BY importance DESC
        `, p.Project, p.Topic)

        if len(entries) < 10 {
            continue // 条目太少，不需要压缩
        }

        // 3. 调用 LLM 去重合并
        merged, _ := llm.Compress(weeklyCompressPrompt, formatEntries(entries))

        // 4. 归档旧条目
        for _, old := range entries {
            e.db.Exec(`UPDATE memories SET is_archived = 1 WHERE id = ?`, old.ID)
        }

        // 5. 写入合并后的新条目
        for _, fact := range merged.Facts {
            e.WriteTier2(fact)
        }
    }

    // 6. 更新 Tier 1 画像
    return e.refreshTier1(llm)
}

// refreshTier1 从 Tier 2 归纳更新核心画像
func (e *Engine) refreshTier1(llm LLMClient) error {
    // 读取当前 Tier 1
    currentProfile, _ := e.LoadTier1()

    // 读取 Tier 2 中高重要性条目
    highImportance, _ := e.db.Query(`
        SELECT content, category FROM memories
        WHERE tier = 2 AND importance >= 0.7 AND is_archived = 0
        ORDER BY importance DESC LIMIT 200
    `)

    // 调用 LLM 归纳更新画像
    newProfile, _ := llm.UpdateProfile(currentProfile, formatEntries(highImportance))

    // 归档旧 Tier 1，写入新 Tier 1
    e.db.Exec(`UPDATE memories SET is_archived = 1 WHERE tier = 1`)
    for _, entry := range newProfile.Entries {
        e.db.Exec(`
            INSERT INTO memories (tier, project, topic, category, content, importance, source)
            VALUES (1, '_global', '_profile', ?, ?, 1.0, 'compression')
        `, entry.Category, entry.Content)
    }

    return nil
}
```
