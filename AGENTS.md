# PROJECT KNOWLEDGE BASE

**Updated:** 2026-02-12 (Asia/Shanghai)
**Commit:** 45941a4
**Branch:** main

## OVERVIEW
myclaw is a personal AI assistant built on `agentsdk-go` (Go 1.24). It supports CLI (`agent`, `gateway`, `onboard`, `status`) and Gateway orchestration mode. Gateway routes inbound/outbound messages across Telegram, Feishu, WeCom, WhatsApp, and WebUI channels through an internal message bus, then invokes the runtime with prompt + multimodal content blocks.

## STRUCTURE
```
.
├── cmd/myclaw/          # Cobra CLI entry: agent, gateway, onboard, status
├── internal/
│   ├── bus/             # Message bus (InboundMessage / OutboundMessage + outbound subscribers)
│   ├── channel/         # Channel interface + Telegram + Feishu + WeCom + WhatsApp + WebUI
│   ├── config/          # ~/.myclaw/config.json loader + env var overrides
│   ├── cron/            # cron/every/at scheduler with JSON persistence
│   ├── gateway/         # Runtime orchestration: bus + channels + cron + heartbeat + memory
│   ├── heartbeat/       # Periodic HEARTBEAT.md trigger (default every 30m)
│   ├── memory/          # SQLite tiered memory engine + migration from legacy files
│   └── skills/          # Workspace skill loader and registration
├── scripts/autolab/     # Branch, verify, submit, promote, rollback, deploy scripts
├── .githooks/           # local branch policy + pre-push verification gate
├── .github/workflows/   # ci, pr-verify, deploy-main, rollback, release, secret-audit
├── workspace/           # Prompt assets synced to ~/.myclaw/workspace
├── Dockerfile           # Multi-stage image, defaults to `myclaw gateway`
└── docker-compose.yml   # myclaw service + optional cloudflared tunnel profile
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add CLI command | `cmd/myclaw/main.go` | Register via `rootCmd.AddCommand()` in `init()` |
| Add messaging channel | `internal/channel/` | Implement `Channel` in `base.go`, register in `manager.go` |
| Change config schema | `internal/config/config.go` | Add struct field + env var override in `LoadConfig()` |
| Add cron schedule type | `internal/cron/types.go` + `service.go` | New `Schedule.Kind` + handle in `tickLoop()` |
| Modify system prompt | `cmd/myclaw/main.go` or `gateway/gateway.go` | Both have `buildSystemPrompt()` — agent vs gateway mode |
| Add memory features | `internal/memory/engine.go` + `internal/memory/*` | SQLite tiers (1/2/3), retrieval, extraction, compression |
| Modify CI checks | `.github/workflows/pr-verify.yml` + `.github/workflows/ci.yml` | PR strict verify + basic CI |
| Change local verification | `scripts/autolab/verify.sh` | Called by pre-push hook |
| Modify deployment | `.github/workflows/deploy-main.yml` + `scripts/autolab/deploy-main.sh` | Relies on external `/usr/local/bin/myclaw-deploy-run` |

## CODE MAP

### Key Interfaces
| Interface | Package | Implementations | Purpose |
|-----------|---------|-----------------|---------|
| `Channel` | channel | `TelegramChannel`, `FeishuChannel`, `WeComChannel`, `WhatsAppChannel`, `WebUIChannel` | Messaging channel abstraction |
| `Runtime` | cmd/myclaw, gateway | `runtimeWrapper`, `runtimeAdapter` | Agent runtime (enables test mocking) |
| `TelegramBot` | channel | `tgBotWrapper` | Telegram bot API abstraction |
| `FeishuClient` | channel | `defaultFeishuClient` | Feishu API abstraction |
| `WeComClient` | channel | `defaultWeComClient` | WeCom response_url sender with retry logic |

### Key Types
| Type | Package | Role |
|------|---------|------|
| `Gateway` | gateway | Central orchestrator — owns bus, runtime, channels, cron, heartbeat, memory |
| `MessageBus` | bus | Buffered pub/sub: `Inbound` + `Outbound` chans with subscriber dispatch |
| `ChannelManager` | channel | Channel lifecycle + outbound subscriber wiring |
| `Service` (cron) | cron | Cron/interval/one-shot scheduler with JSON persistence |
| `Service` (heartbeat) | heartbeat | Periodic HEARTBEAT.md reader -> agent invocation |
| `Engine` | memory | SQLite memory engine: Tier1 profile + Tier2 knowledge + Tier3 events |
| `Config` | config | Full app config: agent/provider/channels/tools/skills/hooks/mcp/gateway/memory |

### Data Flow (Gateway)
```
Telegram/Feishu/WeCom/WhatsApp/WebUI -> Channel.Start() -> bus.Inbound -> gateway.processLoop()
                                                                          ↓
                                                                     runtime.Run()
                                                                          ↓
                                          bus.Outbound -> bus.DispatchOutbound() -> Channel.Send()
```

## CONVENTIONS
- **Env over config**: `LoadConfig()` applies env var overrides after JSON parse. Priority: `MYCLAW_*` > `ANTHROPIC_*` > `OPENAI_*`.
- **Interface + factory testing**: External dependencies are abstracted via interfaces/factories (`TelegramBot`, `FeishuClient`, `WeComClient`, `Runtime`) and `New*WithFactory()` / `NewWithOptions()` constructors.
- **Test isolation**: `t.TempDir()` for filesystem, `t.Setenv()` for env vars, temporary HOME dirs for config. No `t.Parallel()` - explicit context cancellation/timeouts instead.
- **Deterministic tests**: Always `-count=1` in Makefile, CI, and local hooks.
- **Branching**: `autolab/*` branches only. Git hooks enforce: no commits on main, no pushes to main.
- **Verification pipeline**: gofmt (changed files) -> go vet -> test -> race -> build -> smoke. Identical in CI and local hooks.
- **Logging**: `log.Printf("[component] message")` with bracketed component prefix.
- **Error wrapping**: `fmt.Errorf("context: %w", err)` consistently.

## ANTI-PATTERNS (THIS PROJECT)
- **Never** commit directly to `main` - pre-commit hook blocks it.
- **Never** push directly to `main` - pre-push hook blocks it.
- **Never** commit API keys/tokens - `.gitignore` excludes config.json, .env, workspace memory.
- **Never** deploy from non-`main` branches.
- **Never** bypass verification - `SKIP_VERIFY=1` exists but is for emergencies only.
- **Never** auto-approve merge/deploy without explicit user approval.
- **Never** use `t.Parallel()` - project uses explicit cancellation patterns.

## UNIQUE STYLES
- **Dual Runtime interface**: Both `cmd/myclaw` and `internal/gateway` define their own `Runtime` interface wrapping `api.Runtime`. Same shape, different packages - intentional for package isolation.
- **Dual buildSystemPrompt**: Agent mode and gateway mode each have `buildSystemPrompt()` - both read AGENTS.md + SOUL.md + memory context.
- **Factory-based test injection**: Simple function types (`RuntimeFactory`, `BotFactory`, `FeishuClientFactory`, `WeComClientFactory`) with `WithFactory`/`WithOptions` constructors - not DI containers.
- **Feishu double-check locking**: `defaultFeishuClient.GetTenantAccessToken()` uses RLock -> RUnlock -> Lock pattern for token caching.
- **Telegram HTML fallback**: `Send()` tries HTML parse mode first, falls back to plain text on error.
- **Gateway multimodal workaround**: `gateway.runAgent()` prepends text prompt into `ContentBlocks` when media exists due SDK behavior, then clears `Prompt` to avoid duplication.
- **Cron triple-schedule**: Supports `cron` (robfig/cron), `every` (interval ms), and `at` (one-shot timestamp) - `tickLoop` handles non-cron types.
- **Signal injection**: `Gateway.signalChan` is injectable for testing graceful shutdown.

## COMMANDS
```bash
# Build binary
make build

# Run agent REPL
make run

# Onboard and status
make onboard
make status

# Start gateway (channels + cron + heartbeat)
make gateway

# Run all tests
make test

# Run tests with race detection
make test-race

# Run tests with coverage report
make test-cover

# Run golangci-lint
make lint

# Docker compose
make docker-up
make docker-up-tunnel
make docker-down

# Start cloudflared tunnel for Feishu webhook
make tunnel
```

## Go 1.24 | Key deps: agentsdk-go v0.8.2, cobra, robfig/cron/v3, go-telegram-bot-api/v5

## NOTES
- **License Discrepancy**: `README.md` claims an MIT license, but no `LICENSE` file exists at the root. One should be added for clarity.
- **External CI/CD Dependency**: The CI/CD deployment process relies on an external host-side wrapper script (`/usr/local/bin/myclaw-deploy-run`), which is not part of the repository and couples deployments to the specific host environment.
- **Configuration Location**: Project configuration is split between `config.json` (usually in `~/.myclaw/`) and in-repo `workspace/` files (`AGENTS.md`, `SOUL.md`), which can be less typical than fully in-repo config.
- **Makefile Inconsistencies**: `.PHONY` declarations in `Makefile` are incomplete, and install hints (`brew install ...`) are macOS-centric.
- **Workflow Split**: Both `ci.yml` and `pr-verify.yml` run overlapping checks (tests/build), so contributor docs should reference the stricter `pr-verify.yml` as merge gate.
- **Custom Secret Scan**: The `secret-audit.yml` workflow uses an inline custom Python scanner for full git history scanning, rather than a maintained open-source solution.
