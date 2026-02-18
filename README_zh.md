# myclaw

基于 [agentsdk-go](https://github.com/cexll/agentsdk-go) 构建的个人 AI 助手。

## 功能特性

- **CLI Agent** - 支持单次消息模式与交互式 REPL 模式
- **Gateway** - 完整编排：消息通道 + 定时任务 + 心跳
- **Telegram 通道** - 通过 Telegram Bot 收发消息（文本 + 图片 + 文档）
- **Feishu 通道** - 通过 Feishu（Lark）Bot 收发消息
- **WeCom 通道** - 通过企业微信智能机器人 API 模式接收消息并回复 Markdown
- **WhatsApp 通道** - 通过 WhatsApp 收发消息（扫码登录）
- **Web UI** - 基于浏览器的 WebSocket 聊天界面（PC + 移动端自适应）
- **多 Provider** - 支持 Anthropic 和 OpenAI 模型
- **多模态** - 支持图像识别与文档处理
- **Cron 任务** - 支持 JSON 持久化的定时任务
- **Heartbeat** - 从 HEARTBEAT.md 周期触发任务
- **Memory** - 长期记忆（MEMORY.md）+ 每日日志记忆
- **Skills** - 从 workspace 加载自定义技能

## 快速开始

```bash
# 构建
make build

# 交互式配置向导
make setup

# 或手动初始化配置与 workspace
make onboard

# 设置 API Key
export MYCLAW_API_KEY=your-api-key

# 运行 agent（单次消息）
./myclaw agent -m "Hello"

# 运行 agent（REPL 模式）
make run

# 启动 gateway（通道 + cron + heartbeat）
make gateway
```

## Makefile 目标

| 目标 | 说明 |
|------|------|
| `make build` | 构建二进制 |
| `make run` | 运行 agent REPL |
| `make gateway` | 启动 gateway（通道 + cron + heartbeat） |
| `make onboard` | 初始化配置与 workspace |
| `make status` | 查看 myclaw 状态 |
| `make setup` | 交互式配置（生成 `~/.myclaw/config.json`） |
| `make tunnel` | 启动 cloudflared 隧道用于 Feishu webhook |
| `make test` | 运行测试 |
| `make test-race` | 运行 race 检测测试 |
| `make test-cover` | 运行测试并输出覆盖率 |
| `make docker-up` | Docker 构建并启动 |
| `make docker-up-tunnel` | Docker 启动（含 cloudflared 隧道） |
| `make docker-down` | Docker 停止 |
| `make lint` | 运行 golangci-lint |

## 架构

```
┌─────────────────────────────────────────────────────────┐
│                      CLI (cobra)                        │
│              agent | gateway | onboard | status         │
└──────┬──────────────────┬───────────────────────────────┘
       │                  │
       ▼                  ▼
┌──────────────┐  ┌───────────────────────────────────────┐
│  Agent Mode  │  │              Gateway                  │
│  (single /   │  │                                       │
│   REPL)      │  │  ┌─────────┐  ┌──────┐  ┌─────────┐  │
└──────┬───────┘  │  │ Channel │  │ Cron │  │Heartbeat│  │
       │          │  │ Manager │  │      │  │         │  │
       │          │  └────┬────┘  └──┬───┘  └────┬────┘  │
       │          │       │          │           │        │
       ▼          │       ▼          ▼           ▼        │
┌──────────────┐  │  ┌─────────────────────────────────┐  │
│  agentsdk-go │  │  │          Message Bus            │  │
│   Runtime    │◄─┤  │    Inbound ←── Channels         │  │
│              │  │  │    Outbound ──► Channels         │  │
└──────────────┘  │  └──────────────┬──────────────────┘  │
                  │                 │                      │
                  │                 ▼                      │
                  │  ┌──────────────────────────────────┐  │
                  │  │      agentsdk-go Runtime         │  │
                  │  │   (ReAct loop + tool execution)  │  │
                  │  └──────────────────────────────────┘  │
                  │                                       │
                  │  ┌──────────┐  ┌────────────────────┐  │
                  │  │  Memory  │  │      Config        │  │
                  │  │ (MEMORY  │  │  (JSON + env vars) │  │
                  │  │  + daily)│  │                    │  │
                  │  └──────────┘  └────────────────────┘  │
                  └───────────────────────────────────────┘

数据流（Gateway 模式）：
  Telegram/Feishu/WeCom/WhatsApp/WebUI ──► Channel ──► Bus.Inbound ──► processLoop
                                                                      │
                                                                      ▼
                                                               Runtime.Run()
                                                                      │
                                                                      ▼
                                       Bus.Outbound ──► Channel ──► Telegram/Feishu/WeCom/WhatsApp/WebUI
```

## 项目结构

```
cmd/myclaw/          CLI 入口（agent, gateway, onboard, status）
internal/
  bus/               消息总线（inbound/outbound channels）
  channel/           通道接口 + 实现
    telegram.go      Telegram Bot（轮询，文本/图片/文档）
    feishu.go        Feishu/Lark Bot（webhook）
    wecom.go         企业微信智能机器人（webhook，加密）
    whatsapp.go      WhatsApp（whatsmeow，扫码登录）
    webui.go         Web UI（WebSocket，内嵌 HTML）
    static/          内嵌 Web UI 静态资源
  config/            配置加载（JSON + 环境变量）
  cron/              定时任务调度（JSON 持久化）
  gateway/           Gateway 编排（bus + runtime + channels）
  heartbeat/         周期心跳服务
  memory/            记忆系统（长期 + 每日）
  skills/            自定义技能加载器
docs/
  telegram-setup.md  Telegram 配置指南
  feishu-setup.md    Feishu 配置指南
  wecom-setup.md     企业微信配置指南
scripts/
  setup.sh           交互式配置生成脚本
workspace/
  AGENTS.md          Agent 系统提示词
  SOUL.md            Agent 人格设定
```

## 配置

可以运行 `make setup` 进行交互式配置，或将 `config.example.json` 复制到 `~/.myclaw/config.json`：

```json
{
  "provider": {
    "type": "anthropic",
    "apiKey": "your-api-key",
    "baseUrl": ""
  },
  "agent": {
    "model": "claude-sonnet-4-5-20250929",
    "modelReasoningEffort": "medium"
  },
  "channels": {
    "telegram": {
      "enabled": true,
      "token": "your-bot-token",
      "allowFrom": ["123456789"]
    },
    "feishu": {
      "enabled": true,
      "appId": "cli_xxx",
      "appSecret": "your-app-secret",
      "verificationToken": "your-verification-token",
      "port": 9876,
      "allowFrom": []
    },
    "wecom": {
      "enabled": true,
      "token": "your-token",
      "encodingAESKey": "your-43-char-encoding-aes-key",
      "receiveId": "",
      "port": 9886,
      "allowFrom": ["zhangsan"]
    },
    "whatsapp": {
      "enabled": true,
      "allowFrom": []
    },
    "webui": {
      "enabled": true,
      "allowFrom": []
    }
  },
  "memory": {
    "enabled": true,
    "modelReasoningEffort": "high"
  }
}
```

### 模型推理强度（Reasoning Effort）

- 字段位置：
  - 全局默认值：`agent.modelReasoningEffort`
  - Memory 覆盖值：`memory.modelReasoningEffort`
- 优先级：`memory.modelReasoningEffort` > `agent.modelReasoningEffort` > 空（不传 reasoning 参数）。
- 本版本可选值：`low`、`medium`、`high`、`xhigh`。
- Fail-open 行为：若 provider/model 不支持 reasoning 参数，myclaw 会记录 warning，并在不带 reasoning 参数的情况下重试一次。
- 环境变量：本版本该设置不支持 env var。

### Provider 类型

| 类型 | 配置 | 环境变量 |
|------|------|----------|
| `anthropic`（默认） | `"type": "anthropic"` | `MYCLAW_API_KEY`, `ANTHROPIC_API_KEY` |
| `openai` | `"type": "openai"` | `OPENAI_API_KEY` |

使用 OpenAI 时，请将模型设置为 OpenAI 模型名（例如 `gpt-4o`）。

### 环境变量

| 变量 | 说明 |
|------|------|
| `MYCLAW_API_KEY` | API Key（任意 provider） |
| `ANTHROPIC_API_KEY` | Anthropic API Key |
| `OPENAI_API_KEY` | OpenAI API Key（会自动将 provider 类型设为 openai） |
| `MYCLAW_BASE_URL` | 自定义 API Base URL |
| `MYCLAW_TELEGRAM_TOKEN` | Telegram Bot Token |
| `MYCLAW_FEISHU_APP_ID` | Feishu App ID |
| `MYCLAW_FEISHU_APP_SECRET` | Feishu App Secret |
| `MYCLAW_WECOM_TOKEN` | 企业微信智能机器人回调 token |
| `MYCLAW_WECOM_ENCODING_AES_KEY` | 企业微信智能机器人回调 EncodingAESKey |
| `MYCLAW_WECOM_RECEIVE_ID` | 可选，严格解密校验 receive-id |

> 涉及 API Key 等敏感信息时，建议优先使用环境变量，而非写入配置文件。

## 通道配置

### Telegram

详见 [docs/telegram-setup.md](docs/telegram-setup.md)。

快速步骤：
1. 通过 Telegram 的 [@BotFather](https://t.me/BotFather) 创建 Bot
2. 在配置中设置 `token` 或使用环境变量 `MYCLAW_TELEGRAM_TOKEN`
3. 运行 `make gateway`

### Feishu (Lark)

详见 [docs/feishu-setup.md](docs/feishu-setup.md)。

快速步骤：
1. 在 [Feishu Open Platform](https://open.feishu.cn/app) 创建应用
2. 启用 **Bot** 能力
3. 添加权限：`im:message`, `im:message:send_as_bot`
4. 配置事件订阅 URL：`https://your-domain/feishu/webhook`
5. 订阅事件：`im.message.receive_v1`
6. 在配置中设置 `appId`、`appSecret`、`verificationToken`
7. 运行 `make gateway` 和 `make tunnel`（用于暴露 webhook 公网地址）

### WeCom

详见 [docs/wecom-setup.md](docs/wecom-setup.md)。

快速步骤：
1. 创建企业微信智能机器人（API 模式），获取 `token`、`encodingAESKey`
2. 配置回调地址：`https://your-domain/wecom/bot`
3. 在企业微信后台和 myclaw 配置中同步设置 `token` 与 `encodingAESKey`
4. 如需严格解密 receive-id 校验，可选设置 `receiveId`
5. 可选设置 `allowFrom` 作为白名单（未设置/为空时允许所有用户）
6. 运行 `make gateway`

WeCom 说明：
- 下行消息使用 `response_url`，并发送 `markdown` 负载
- `response_url` 生命周期短（通常单次可用）；延迟或重复回复可能失败
- 下行 Markdown 内容超过 20480 字节会被截断

### WhatsApp

快速步骤：
1. 在配置中设置 `"whatsapp": {"enabled": true}`
2. 运行 `make gateway`
3. 使用手机 WhatsApp 扫描终端显示的二维码
4. 会话会保存在本地 SQLite 中（重启后自动重连）

### Web UI

快速步骤：
1. 在配置中设置 `"webui": {"enabled": true}`
2. 运行 `make gateway`
3. 在浏览器打开 `http://localhost:18790`（PC 或移动端）

特性：
- 响应式布局（PC + 移动端）
- 深色模式（跟随系统偏好）
- WebSocket 实时通信
- Markdown 渲染（代码块、粗体、斜体、链接）
- 断线自动重连

## Docker 部署

### 构建与运行

```bash
docker build -t myclaw .

docker run -d \
  -e MYCLAW_API_KEY=your-api-key \
  -e MYCLAW_TELEGRAM_TOKEN=your-token \
  -p 18790:18790 \
  -p 9876:9876 \
  -p 9886:9886 \
  -v myclaw-data:/root/.myclaw \
  myclaw
```

### Docker Compose

```bash
# 从示例创建 .env
cp .env.example .env
# 编辑 .env，填入你的凭证

# 启动 gateway
docker compose up -d

# 启动并带 cloudflared 隧道（用于 Feishu webhook）
docker compose --profile tunnel up -d

# 查看日志
docker compose logs -f myclaw
```

### Cloudflared Tunnel

Feishu webhook 需要公网地址：

```bash
# 临时隧道（开发环境）
make tunnel

# 或使用 docker compose
docker compose --profile tunnel up -d
docker compose logs tunnel | grep trycloudflare
```

将输出的 URL 加上 `/feishu/webhook`，配置到 Feishu 事件订阅地址。

## 安全

- `~/.myclaw/config.json` 权限应为 `chmod 600`（仅所有者可读写）
- `.gitignore` 已排除 `config.json`、`.env` 与 workspace 记忆文件
- CI/CD 与生产环境建议通过环境变量注入敏感信息
- 不要将真实 API Key 或 token 提交到版本控制

## 测试

```bash
make test            # 运行全部测试
make test-race       # 运行 race 检测测试
make test-cover      # 运行测试并输出覆盖率
make lint            # 运行 golangci-lint
```

## 贡献 / CI

### 分支与 hooks

- 使用非 `main` 分支（建议：`autolab/*`）
- 安装本地 git hooks：

```bash
scripts/autolab/setup-hooks.sh
```

- `.githooks/pre-commit` 会阻止在 `main` 上提交
- `.githooks/pre-push` 会阻止直接推送到 `main`，并默认执行 `scripts/autolab/verify.sh`

### 本地校验

执行严格本地校验（与 hooks 一致）：

```bash
scripts/autolab/verify.sh
```

流程顺序：

1. `gofmt`（仅检查变更的 `.go` 文件）
2. `go vet ./...`
3. `go test ./... -count=1`
4. `go test -race ./... -count=1`
5. `go build ./...`
6. Smoke（临时 HOME 下执行 `myclaw onboard` + `myclaw status`）

### GitHub workflows

| Workflow | 触发方式 | 作用 |
|----------|----------|------|
| `pr-verify` | PR 到 `main`、手动触发 | 严格 PR 关卡：lint/vet/test/race/build/smoke |
| `secret-audit` | PR 到 `main`、手动触发 | 扫描已跟踪文件与 git 历史中的敏感信息 |
| `ci` | push/PR 到 `main` | 基础 test + build |
| `tag-main` | push 到 `main` | 自动创建下一个 `vX.Y.Z` 标签以触发发布流水线 |
| `release` | tag `v*` | 发布 GitHub Release + 多平台二进制 + GHCR 镜像 |
| `deploy-main` | release 成功后、手动触发 | 通过 `/usr/local/bin/myclaw-deploy-run` 在自托管 runner 部署 |
| `rollback` | 手动触发 | 从目标 ref 创建回滚 PR 分支并触发检查 |

`tag-main` 需要配置仓库 Secret：`RELEASE_TAG_PUSH_TOKEN`（具备 contents write 权限的 PAT），以便推送版本标签后能够触发 `release` 工作流。

对于合并可用性，建议将 `pr-verify` 与 `secret-audit` 视为主要质量门禁。

## 许可证

MIT
