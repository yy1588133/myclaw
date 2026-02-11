# myclaw

Personal AI assistant built on [agentsdk-go](https://github.com/cexll/agentsdk-go).

## Features

- **CLI Agent** - Single message or interactive REPL mode
- **Gateway** - Full orchestration: channels + cron + heartbeat
- **Telegram Channel** - Receive and send messages via Telegram bot
- **Feishu Channel** - Receive and send messages via Feishu (Lark) bot
- **Multi-Provider** - Support for Anthropic and OpenAI models
- **Cron Jobs** - Scheduled tasks with JSON persistence
- **Heartbeat** - Periodic tasks from HEARTBEAT.md
- **Memory** - Long-term (MEMORY.md) + daily memories
- **Tiered Memory (optional)** - SQLite-based tiered memory with retrieval, extraction, and compression

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
                  │  │ (MEMORY  │  │  (JSON + env vars) │  │
                  │  │  + daily)│  │                    │  │
                  │  └──────────┘  └────────────────────┘  │
                  └───────────────────────────────────────┘

Data Flow (Gateway Mode):
  Telegram/Feishu ──► Channel ──► Bus.Inbound ──► processLoop
                                                       │
                                                       ▼
                                                Runtime.Run()
                                                       │
                                                       ▼
                                        Bus.Outbound ──► Channel ──► Telegram/Feishu
```

## Project Structure

```
cmd/myclaw/          CLI entry point (agent, gateway, onboard, status)
internal/
  bus/               Message bus (inbound/outbound channels)
  channel/           Channel interface + Telegram + Feishu implementations
  config/            Configuration loading (JSON + env vars)
  cron/              Cron job scheduling with JSON persistence
  gateway/           Gateway orchestration (bus + runtime + channels)
  heartbeat/         Periodic heartbeat service
  memory/            Memory system (long-term + daily)
docs/
  telegram-setup.md  Telegram bot setup guide
  feishu-setup.md    Feishu bot setup guide
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
    "model": "claude-sonnet-4-5-20250929"
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
    }
  }
}
```

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
| `MYCLAW_MEMORY_ENABLED` | Enable SQLite memory engine (`true`/`false`) |
| `MYCLAW_MEMORY_MODEL` | Optional dedicated model for memory tasks |
| `MYCLAW_MEMORY_API_KEY` | Optional dedicated API key for memory tasks |
| `MYCLAW_MEMORY_BASE_URL` | Optional dedicated base URL for memory tasks |
| `MYCLAW_MEMORY_DB_PATH` | Optional absolute DB path (defaults to `~/.myclaw/data/memory.db`) |
| `MYCLAW_MEMORY_MAX_TOKENS` | Optional max tokens for memory model |
| `MYCLAW_MEMORY_QUIET_GAP` | Extraction quiet gap (e.g. `3m`) |
| `MYCLAW_MEMORY_TOKEN_BUDGET` | Extraction token budget ratio (0-1) |
| `MYCLAW_MEMORY_DAILY_FLUSH` | Daily extraction flush time (`HH:MM`) |

> Prefer environment variables over config files for sensitive values like API keys.

### Memory Configuration

When `memory.enabled` is `true`, gateway uses the SQLite memory engine with:
- Tier 1 profile loading into system prompt
- Tier 2 retrieval injection for memory-like user queries
- Async extraction buffering after each dialog turn
- Daily and weekly internal memory compression cron jobs

Example:

```json
"memory": {
  "enabled": true,
  "model": "gpt-5",
  "maxTokens": 8192,
  "dbPath": "",
  "provider": {
    "type": "openai",
    "apiKey": "sk-xxx",
    "baseUrl": "https://example.com/v1"
  },
  "extraction": {
    "quietGap": "3m",
    "tokenBudget": 0.6,
    "dailyFlush": "03:00"
  }
}
```

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

## Docker Deployment

### Build and Run

```bash
docker build -t myclaw .

docker run -d \
  -e MYCLAW_API_KEY=your-api-key \
  -e MYCLAW_TELEGRAM_TOKEN=your-token \
  -p 18790:18790 \
  -p 9876:9876 \
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

| Package | Coverage |
|---------|----------|
| internal/bus | 100.0% |
| internal/heartbeat | 97.1% |
| internal/cron | 94.4% |
| internal/config | 91.2% |
| internal/channel | 90.5% |
| internal/gateway | 90.2% |
| internal/memory | 89.1% |
| cmd/myclaw | 82.3% |

## License

MIT
