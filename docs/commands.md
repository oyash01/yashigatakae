# Commands

Every CLI subcommand. `yashigatakae <subcommand> --help` for the full flag list at runtime.

## Top-level

| Command | What |
|---|---|
| `yashigatakae` | Open Bubble Tea root menu (no args) |
| `yashigatakae init [-y] [--vps] [--state-repo PATH]` | Bootstrap this machine |
| `yashigatakae doctor` | Run all health checks |
| `yashigatakae status` | Drift, version, last sync, VPS health |
| `yashigatakae enable` | Enable + start every service (Linux/systemd) |
| `yashigatakae disable` | Stop + disable every service |
| `yashigatakae upgrade` | Self-update via the install script |
| `yashigatakae sync` | Push/pull state repo |

## caveman

| Command | What |
|---|---|
| `yashigatakae caveman <lite\|full\|ultra\|off>` | Switch compression level |
| `yashigatakae caveman compact [--dry-run --transcript FILE --session ID]` | Show the compaction prompt that would inject |
| `yashigatakae caveman truncate --tool BASH` | Shrink stdin (called by PreToolUse hook) |
| `yashigatakae caveman cache ephemeral` | Print the cache_control marker |
| `yashigatakae caveman config get` | Print the merged caveman config |
| `yashigatakae caveman config set <key> <value>` | Update one config field |

## mempalace

| Command | What |
|---|---|
| `yashigatakae mempalace remember <text> [--project P --tags a,b]` | Store a memory |
| `yashigatakae mempalace recall <query> [--top N --category CAT --mode hybrid\|semantic\|keyword --half-life DAYS]` | Search memory |
| `yashigatakae mempalace hydrate [--top 5 --include-recent --cwd PATH]` | Top-N recall scoped to cwd (run by SessionStart hook) |
| `yashigatakae mempalace consolidate --project P --window 720h --batch 50 [--archive --dry-run]` | Roll up old entries into a summary |
| `yashigatakae mempalace forget <id>` | Delete an entry |
| `yashigatakae mempalace stats` | Counts + db path |
| `yashigatakae mempalace serve --addr :8765` | HTTP MCP server |

## bifrost

| Command | What |
|---|---|
| `yashigatakae bifrost serve --addr :8443 [--mempalace URL --api-key KEY --tls --tls-domain DOMAIN]` | Run the gateway |
| `yashigatakae bifrost tools` | List registered tools (proxied + builtin) |

## hermes

| Command | What |
|---|---|
| `yashigatakae hermes enqueue --project P --prompt "..." [--idempotency-key K --priority N --max-retries N --depends-on ID]` | Add a task |
| `yashigatakae hermes ls [--status pending\|running\|done\|failed\|cancelled\|dlq\|scheduled --limit 50]` | List tasks |
| `yashigatakae hermes logs <id>` | Stream the log for a task |
| `yashigatakae hermes cancel <id>` | Mark task cancelled |
| `yashigatakae hermes serve --poll 5s --concurrency 1 [--base-backoff 30s --max-backoff 8h]` | Run the worker loop |
| `yashigatakae hermes dlq ls` | List dead-lettered tasks |
| `yashigatakae hermes dlq retry <id>` | Move a DLQ task back to pending |
| `yashigatakae hermes schedule add "<cron>" --project P --prompt "..."` | Add a cron schedule |
| `yashigatakae hermes schedule ls` | List active schedules |
| `yashigatakae hermes schedule rm <id>` | Delete a schedule |

## kintsugi

| Command | What |
|---|---|
| `yashigatakae handoff [--note "..."]` | Pack + push current session, get resume code |
| `yashigatakae resume <code-or-name>` | Pull + restore a session on this machine |
| `yashigatakae sessions ls [--local --relay]` | List synced sessions |
| `yashigatakae sessions diff <id>` | Show what would change locally |
| `yashigatakae sessions abandon <id>` | Mark dead |
| `yashigatakae backfill [--dry-run --limit N --since 30d --force --skip-disk-check]` | Upload past transcripts |
| `yashigatakae kintsugi serve --addr :8444 [--tls --tls-domain DOMAIN]` | Relay server (VPS) |

## graphify

| Command | What |
|---|---|
| `yashigatakae graphify <repo> [--pro --refresh --out DIR]` | Generate the wiki |
| `yashigatakae graphify check <repo>` | Exit non-zero if wiki has broken wikilinks |

## secrets

| Command | What |
|---|---|
| `yashigatakae secrets rotate [--restart --keys K,K]` | Generate new keys, optionally restart services |

## db (at-rest encryption)

| Command | What |
|---|---|
| `yashigatakae db lock <path>` | Encrypt path → path.age, remove plaintext |
| `yashigatakae db unlock <path>` | Decrypt path.age → path |
| `yashigatakae db lock-all` | Lock both mempalace + hermes dbs |
| `yashigatakae db unlock-all` | Unlock both |

## state

| Command | What |
|---|---|
| `yashigatakae state render` | Re-render embedded templates into ~/.claude/ |
| `yashigatakae state pull` | git pull the state repo |
| `yashigatakae state create-repo --name N --owner OWNER` | Create a fresh state repo from the public template |

## hooks

| Command | What |
|---|---|
| `yashigatakae hooks autocommit` | PostToolUse: auto-commit changes (called by hook) |
| `yashigatakae hooks sweep` | SessionEnd: distill memories (called by hook) |
