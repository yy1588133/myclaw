# channel — Messaging Channel Abstraction

## OVERVIEW
Implements the `Channel` interface for Telegram (long-polling) and Feishu (webhook HTTP server). All channels connect to the message bus for inbound/outbound routing.

## STRUCTURE
| File | Role |
|------|------|
| `base.go` | `Channel` interface + `BaseChannel` with allowFrom filtering |
| `manager.go` | `ChannelManager` — lifecycle, outbound subscriber wiring |
| `telegram.go` | `TelegramChannel` — polling via go-telegram-bot-api, proxy support, markdown→HTML |
| `feishu.go` | `FeishuChannel` — webhook HTTP server, tenant token caching with double-check lock |

## WHERE TO LOOK
| Task | Start Here |
|------|------------|
| Add new channel type | Implement `Channel` from `base.go`, register in `manager.go:NewChannelManager()` |
| Modify message filtering | `BaseChannel.IsAllowed()` in `base.go` — empty allowFrom = allow all |
| Change Telegram format | `toTelegramHTML()` in `telegram.go` — markdown→HTML converter |
| Fix Feishu token issues | `defaultFeishuClient.GetTenantAccessToken()` — double-check lock, 60s early expiry |
| Add webhook endpoints | `FeishuChannel.Start()` — `http.ServeMux` with `/feishu/webhook` |

## CONVENTIONS
- **Interface + factory for testing**: `TelegramBot`/`BotFactory`, `FeishuClient`/`FeishuClientFactory`. Use `New*WithFactory()` in tests.
- **BaseChannel embedding**: All channels embed `BaseChannel` for name + bus + allowFrom.
- **Inbound**: Channel receives → validates sender via `IsAllowed()` → pushes to `bus.Inbound`.
- **Outbound**: `ChannelManager` subscribes each channel via `bus.SubscribeOutbound()`.

## ANTI-PATTERNS
- **Never** call `Send()` before `Start()` — bot/client won't be initialized.
- **Never** skip `IsAllowed()` — it's the security boundary for sender filtering.
- Telegram 4096 char limit — `Send()` handles chunking at newline boundaries.
