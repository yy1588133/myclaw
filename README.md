# myclaw

Personal AI assistant built on [agentsdk-go](https://github.com/cexll/agentsdk-go).

## Features

- **CLI Agent** - Single message or interactive REPL mode
- **Gateway** - Full orchestration: channels + cron + heartbeat
- **Telegram Channel** - Receive and send messages via Telegram bot (text + image + document)
- **Feishu Channel** - Receive and send messages via Feishu (Lark) bot
- **WeCom Channel** - Receive inbound messages and send markdown replies via WeCom intelligent bot API mode
- **WhatsApp Channel** - Receive and send messages via WhatsApp (QR code login)
- **Web UI** - Browser-based chat interface with WebSocket (responsive, PC + mobile)
- **Multi-Provider** - Support for Anthropic and OpenAI models
- **Multimodal** - Image recognition and document processing
- **Cron Jobs** - Scheduled tasks with JSON persistence
- **Heartbeat** - Periodic tasks from HEARTBEAT.md
- **Memory** - SQLite tiered memory (core profile + knowledge + events)
- **Skills** - Custom skill loading from workspace

## Quick Start

```bash
# Build
make build

# Interactive config setup
make setup

# Or initialize config and workspace manually
make onboard

# Set your API key
export MYCLAW_API_KEY=your-api-key

# Run agent (single message)
./myclaw agent -m "Hello"

# Run agent (REPL mode)
make run

# Start gateway (channels + cron + heartbeat)
make gateway
```

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make build` | Build binary |
| `make run` | Run agent REPL |
| `make gateway` | Start gateway (channels + cron + heartbeat) |
| `make onboard` | Initialize config and workspace |
| `make status` | Show myclaw status |
| `make setup` | Interactive config setup (generates `~/.myclaw/config.json`) |
| `make tunnel` | Start cloudflared tunnel for Feishu webhook |
| `make test` | Run tests |
| `make test-race` | Run tests with race detection |
| `make test-cover` | Run tests with coverage report |
| `make docker-up` | Docker build and start |
| `make docker-up-tunnel` | Docker start with cloudflared tunnel |
| `make docker-down` | Docker stop |
| `make lint` | Run golangci-lint |

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      CLI (cobra)                        │
│              agent | gateway | onboard | status          │
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
│  agentsdk-go │  │  │          Message Bus             │  │
│   Runtime    │◄─┤  │    Inbound ←── Channels          │  │
│              │  │  │    Outbound ──► Channels          │  │
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
                  │  │ (SQLite  │  │  (JSON + env vars) │  │
                  │  │  tiered) │  │                    │  │
                  │  └──────────┘  └────────────────────┘  │
                  └───────────────────────────────────────┘

Data Flow (Gateway Mode):
  Telegram/Feishu/WeCom/WhatsApp/WebUI ──► Channel ──► Bus.Inbound ──► processLoop
                                                                      │
                                                                      ▼
                                                               Runtime.Run()
                                                                      │
                                                                      ▼
                                       Bus.Outbound ──► Channel ──► Telegram/Feishu/WeCom/WhatsApp/WebUI
```

## Project Structure

```
cmd/myclaw/          CLI entry point (agent, gateway, onboard, status)
internal/
  bus/               Message bus (inbound/outbound channels)
  channel/           Channel interface + implementations
    telegram.go      Telegram bot (polling, text/image/document)
    feishu.go        Feishu/Lark bot (webhook)
    wecom.go         WeCom intelligent bot (webhook, encrypted)
    whatsapp.go      WhatsApp (whatsmeow, QR login)
    webui.go         Web UI (WebSocket, embedded HTML)
    static/          Embedded web UI assets
  config/            Configuration loading (JSON + env vars)
  cron/              Cron job scheduling with JSON persistence
  gateway/           Gateway orchestration (bus + runtime + channels)
  heartbeat/         Periodic heartbeat service
  memory/            Memory system (SQLite tiered memory)
  skills/            Custom skill loader
docs/
  telegram-setup.md  Telegram bot setup guide
  feishu-setup.md    Feishu bot setup guide
  wecom-setup.md     WeCom intelligent bot setup guide
scripts/
  setup.sh           Interactive config generator
workspace/
  AGENTS.md          Agent system prompt
  SOUL.md            Agent personality
```

## Configuration

Run `make setup` for interactive config, or copy `config.example.json` to `~/.myclaw/config.json`:

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

### Model Reasoning Effort

- Field locations:
  - Global default: `agent.modelReasoningEffort`
  - Memory override: `memory.modelReasoningEffort`
- Precedence: `memory.modelReasoningEffort` > `agent.modelReasoningEffort` > empty (omit reasoning parameter).
- Accepted values in this release: `low`, `medium`, `high`, `xhigh`.
- Fail-open behavior: if a provider/model does not support the reasoning parameter, myclaw logs a warning and retries once without the reasoning parameter.
- Environment variables: no env var support for this setting in this release.

### Provider Types

| Type | Config | Env Vars |
|------|--------|----------|
| `anthropic` (default) | `"type": "anthropic"` | `MYCLAW_API_KEY`, `ANTHROPIC_API_KEY` |
| `openai` | `"type": "openai"` | `OPENAI_API_KEY` |

When using OpenAI, set the model to an OpenAI model name (e.g., `gpt-4o`).

### Environment Variables

| Variable | Description |
|----------|-------------|
| `MYCLAW_API_KEY` | API key (any provider) |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `OPENAI_API_KEY` | OpenAI API key (auto-sets type to openai) |
| `MYCLAW_BASE_URL` | Custom API base URL |
| `MYCLAW_TELEGRAM_TOKEN` | Telegram bot token |
| `MYCLAW_FEISHU_APP_ID` | Feishu app ID |
| `MYCLAW_FEISHU_APP_SECRET` | Feishu app secret |
| `MYCLAW_WECOM_TOKEN` | WeCom intelligent bot callback token |
| `MYCLAW_WECOM_ENCODING_AES_KEY` | WeCom intelligent bot callback EncodingAESKey |
| `MYCLAW_WECOM_RECEIVE_ID` | Optional receive ID for strict decrypt validation |

> Prefer environment variables over config files for sensitive values like API keys.

## Memory Retrieval & Embedding Configuration

`memory` defaults are conservative for rollout safety:

- `memory.retrieval.mode = "classic"` (default)
- `memory.embedding.enabled = false` (default)
- `memory.rerank.enabled = false` (default)
- `enhanced` retrieval is opt-in

If `memory.retrieval.mode` is invalid, it is normalized to `classic`.

### Retrieval Modes

- `classic`: base Tier2 retrieval + FTS expansion/scoring.
- `enhanced`: query expansion + hybrid retrieval (FTS + vector) + optional rerank blend.

Fail-open and fallback behavior:

- Enhanced retrieval fallback: if `enhanced` retrieval errors, gateway logs warning and falls back to `classic` retrieval.
- If retrieval still errors, response generation continues without memory context injection (reply path is not blocked).

### Memory Config Example: local Ollama embedding + optional API rerank

```json
{
  "memory": {
    "enabled": true,
    "model": "gpt-4o-mini",
    "dbPath": "",
    "retrieval": {
      "mode": "enhanced",
      "strongSignalThreshold": 0.85,
      "strongSignalGap": 0.15,
      "candidateLimit": 40,
      "rerankLimit": 20
    },
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

### Memory Config Example: remote API embedding + remote API rerank

```json
{
  "memory": {
    "enabled": true,
    "model": "gpt-4o-mini",
    "provider": {
      "type": "openai",
      "apiKey": "${MEMORY_API_KEY}",
      "baseUrl": "https://api.example.com/v1"
    },
    "retrieval": {
      "mode": "enhanced"
    },
    "embedding": {
      "enabled": true,
      "provider": "api",
      "baseUrl": "https://api.example.com/v1",
      "apiKey": "${EMBEDDING_API_KEY}",
      "model": "text-embedding-3-large",
      "dimension": 3072,
      "timeoutMs": 30000,
      "batchSize": 16
    },
    "rerank": {
      "enabled": true,
      "provider": "api",
      "baseUrl": "https://api.example.com/v1",
      "apiKey": "${RERANK_API_KEY}",
      "model": "rerank-v1",
      "timeoutMs": 30000,
      "topN": 8
    }
  }
}
```

### Migration, Backfill, and Fail-Open Write Path

- **Migration**: on gateway startup, if SQLite memory DB is empty, myclaw runs one-time migration from legacy file memory (`workspace/memory/MEMORY.md` + daily `YYYY-MM-DD.md`).
- **Backfill**: `BackfillEmbeddings(ctx, batchSize)` fills missing Tier2 embeddings in deterministic `id ASC` order and is idempotent (already-embedded rows are skipped).
- **Write path fail-open**: Tier2 write persists first; embedding generation is async and non-blocking. If embedder is unavailable or embedding update fails, row write still succeeds.

Backfill is currently an engine operation (no built-in CLI command). Run it from a maintenance helper/one-off tool that initializes `Engine`, sets embedder config, then calls `BackfillEmbeddings`.

## Channel Setup

### Telegram

See [docs/telegram-setup.md](docs/telegram-setup.md) for detailed setup guide.

Quick steps:
1. Create a bot via [@BotFather](https://t.me/BotFather) on Telegram
2. Set `token` in config or `MYCLAW_TELEGRAM_TOKEN` env var
3. Run `make gateway`

### Feishu (Lark)

See [docs/feishu-setup.md](docs/feishu-setup.md) for detailed setup guide.

Quick steps:
1. Create an app at [Feishu Open Platform](https://open.feishu.cn/app)
2. Enable **Bot** capability
3. Add permissions: `im:message`, `im:message:send_as_bot`
4. Configure Event Subscription URL: `https://your-domain/feishu/webhook`
5. Subscribe to event: `im.message.receive_v1`
6. Set `appId`, `appSecret`, `verificationToken` in config
7. Run `make gateway` and `make tunnel` (for public webhook URL)

### WeCom

See [docs/wecom-setup.md](docs/wecom-setup.md) for detailed setup guide.

Quick steps:
1. Create a WeCom intelligent bot in API mode and get `token`, `encodingAESKey`
2. Configure callback URL: `https://your-domain/wecom/bot`
3. Set `token` and `encodingAESKey` in both WeCom console and myclaw config
4. Optionally set `receiveId` if you need strict decrypt receive-id validation
5. Optional: set `allowFrom` to your user ID(s) as whitelist (if unset/empty, inbound from all users is allowed)
6. Run `make gateway`

WeCom notes:
- Outbound uses `response_url` and sends `markdown` payloads
- `response_url` is short-lived (often single-use); delayed or repeated replies may fail
- Outbound markdown content over 20480 bytes is truncated

### WhatsApp

Quick steps:
1. Set `"whatsapp": {"enabled": true}` in config
2. Run `make gateway`
3. Scan the QR code displayed in terminal with your WhatsApp
4. Session is stored locally in SQLite (auto-reconnects on restart)

### Web UI

Quick steps:
1. Set `"webui": {"enabled": true}` in config
2. Run `make gateway`
3. Open `http://localhost:18790` in your browser (PC or mobile)

Features:
- Responsive design (PC + mobile)
- Dark mode (follows system preference)
- WebSocket real-time communication
- Markdown rendering (code blocks, bold, italic, links)
- Auto-reconnect on connection loss

## Docker Deployment

### Build and Run

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
# Create .env from example
cp .env.example .env
# Edit .env with your credentials

# Start gateway
docker compose up -d

# Start with cloudflared tunnel (for Feishu webhook)
docker compose --profile tunnel up -d

# View logs
docker compose logs -f myclaw
```

### Cloudflared Tunnel

For Feishu webhooks, you need a public URL:

```bash
# Temporary tunnel (dev)
make tunnel

# Or via docker compose
docker compose --profile tunnel up -d
docker compose logs tunnel | grep trycloudflare
```

Set the output URL + `/feishu/webhook` as your Feishu event subscription URL.

## Security

- `~/.myclaw/config.json` is set to `chmod 600` (owner read/write only)
- `.gitignore` excludes `config.json`, `.env`, and workspace memory files
- Use environment variables for sensitive values in CI/CD and production
- Never commit real API keys or tokens to version control

## Testing

```bash
make test            # Run all tests
make test-race       # Run with race detection
make test-cover      # Run with coverage report
make lint            # Run golangci-lint
```

## Contributing / CI

### Branch and hooks

- Use non-`main` branches (recommended: `autolab/*`)
- Install local git hooks:

```bash
scripts/autolab/setup-hooks.sh
```

- `.githooks/pre-commit` blocks commits on `main`
- `.githooks/pre-push` blocks pushes to `main` and runs `scripts/autolab/verify.sh` by default

### Local verification

Run strict local verification (same sequence as hooks):

```bash
scripts/autolab/verify.sh
```

Pipeline order:

1. `gofmt` (changed `.go` files only)
2. `go vet ./...`
3. `go test ./... -count=1`
4. `go test -race ./... -count=1`
5. `go build ./...`
6. Smoke (`myclaw onboard` + `myclaw status` with temp HOME)

### GitHub workflows

| Workflow | Trigger | Role |
|----------|---------|------|
| `pr-verify` | PR to `main`, manual | Strict PR gate: lint/vet/test/race/build/smoke |
| `secret-audit` | PR to `main`, manual | Secret scan across tracked files and git history |
| `ci` | push/PR to `main` | Basic test + build |
| `deploy-main` | push to `main`, manual | Self-hosted deploy via `/usr/local/bin/myclaw-deploy-run` |
| `release` | tag `v*` | GitHub release + multi-platform binaries + GHCR image |
| `rollback` | manual | Create rollback PR branch from target ref and trigger checks |

For merge readiness, treat `pr-verify` and `secret-audit` as the primary quality gates.

## License

MIT
