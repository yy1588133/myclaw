# myclaw

Personal AI assistant built on [agentsdk-go](https://github.com/cexll/agentsdk-go).

## Features

- **CLI Agent** - Single message or interactive REPL mode
- **Gateway** - Full orchestration: channels + cron + heartbeat
- **Telegram Channel** - Receive and send messages via Telegram bot
- **Cron Jobs** - Scheduled tasks with JSON persistence
- **Heartbeat** - Periodic tasks from HEARTBEAT.md
- **Memory** - Long-term (MEMORY.md) + daily memories

## Quick Start

```bash
# Initialize config and workspace
go run ./cmd/myclaw onboard

# Set your API key
export MYCLAW_API_KEY=sk-ant-...

# Run agent in single message mode
go run ./cmd/myclaw agent -m "Hello"

# Run agent in REPL mode
go run ./cmd/myclaw agent

# Start the full gateway
go run ./cmd/myclaw gateway

# Check status
go run ./cmd/myclaw status
```

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
  Telegram ──► Channel ──► Bus.Inbound ──► processLoop
                                               │
                                               ▼
                                        Runtime.Run()
                                               │
                                               ▼
                                        Bus.Outbound ──► Channel ──► Telegram
```

## Project Structure

```
cmd/myclaw/          CLI entry point (agent, gateway, onboard, status)
internal/
  bus/               Message bus (inbound/outbound channels)
  channel/           Channel interface + Telegram implementation
  config/            Configuration loading (JSON + env vars)
  cron/              Cron job scheduling with JSON persistence
  gateway/           Gateway orchestration (bus + runtime + channels)
  heartbeat/         Periodic heartbeat service
  memory/            Memory system (long-term + daily)
workspace/
  AGENTS.md          Agent system prompt
  SOUL.md            Agent personality
```

## Configuration

Copy `config.example.json` to `~/.myclaw/config.json` and edit:

```json
{
  "provider": {
    "api_key": "sk-ant-...",
    "base_url": "https://api.anthropic.com"
  },
  "agent": {
    "model": "claude-sonnet-4-20250514"
  },
  "channels": {
    "telegram": {
      "enabled": true,
      "token": "your-bot-token",
      "allowed_ids": [123456]
    }
  }
}
```

Environment variables override config: `MYCLAW_API_KEY`, `ANTHROPIC_API_KEY`.

## Test Coverage

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

```bash
# Run tests
go test ./...

# Run with race detection
go test -race ./...

# Run with coverage
go test -cover ./...
```

## License

MIT
