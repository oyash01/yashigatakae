# yashigatakae

> Claude Code orchestrator + lifetime memory + cross-device continuity.
> One Go binary. Works on Mac, Windows, Linux, and your VPS.

`yashigatakae` keeps your Claude Code environment in sync across every machine you use, while giving Claude itself persistent memory, a single MCP gateway, and the ability to continue a conversation on a different device exactly where you left off.

**Coding stays local.** The VPS is a brain (memory, gateway, background agent, cross-device relay). Builds and tests run on your laptop or desktop, never on the VPS.

---

## Install

### Mac / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.sh | sh
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.ps1 | iex
```

Then on every machine:

```bash
yashigatakae init
yashigatakae doctor   # 22 health checks, expected: 22-24/24 pass
```

---

## What's in the binary

Six pillars + gstack wrap:

| Pillar | Job | Status |
|---|---|---|
| **gstack** | Wraps `garrytan/gstack` (`/browse`, `/qa`, `/ship`, etc.) installed under flat names | ✓ |
| **caveman** | Token-saving rules baked into SessionStart + UserPromptSubmit hooks | ✓ |
| **mempalace** | Lifetime semantic memory store (sqlite + cosine, brute-force ranked) | ✓ |
| **bifrost** | Single MCP HTTP endpoint that fans out to N downstream MCP servers | ✓ |
| **kintsugi** | Cross-device session continuity: handoff on one machine, resume on another | ✓ |
| **graphify** | Per-repo wiki: overview/recent/index/files.json so Claude reads a map | ✓ |
| **hermes** | Background self-learning agent: queue Claude tasks, distill lessons | ✓ |

---

## Architecture

```
                    ┌─────────────────────────────────┐
                    │   GitHub  oyash01/yashigatakae  │
                    │   GitHub  oyash01/yashi…-state  │
                    └────────────┬────────────────────┘
                                 │ release binaries + git pull
       ┌─────────────────────────┼──────────────────────────┐
       │                         │                          │
   ┌───▼───┐                ┌────▼────┐                ┌────▼─────┐
   │  Mac  │ ── handoff ──▶ │   VPS   │ ◀── resume ── │ Windows  │
   │       │                │  brain  │                │  laptop  │
   │   ┌───┘                │ :8443   │                └──┐       │
   │   │ Claude Code        │ :8444   │                   │       │
   │   │ caveman hooks      │  ┌──────▼──────┐            │       │
   │   │ kintsugi handoff   │  │ mempalace   │            │       │
   │   │ ────── /mcp ──────▶│  │ bifrost     │            │       │
   │   └───────────────────▶│  │ kintsugi    │            │       │
   │                        │  │ hermes 24/7 │            │       │
   │                        │  └─────────────┘            │       │
   └────────────────────────┴───────────┬─────────────────┘       │
                                        └──────── /mcp ────────── ┘
```

The Mac and Windows machines run Claude Code interactively. The VPS hosts a single
HTTP MCP endpoint (bifrost) that Claude Code connects to for memory tools, plus
the kintsugi relay for session blobs and a background hermes worker.

---

## Quickstart

### 1. On the VPS (Linux, root)

```bash
curl -fsSL https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.sh | sh
sudo yashigatakae init --vps
```

That installs four systemd units (mempalace, bifrost, kintsugi, hermes), generates
`BIFROST_API_KEY` + `KINTSUGI_KEY` in `~/.yashigatakae/secrets.env`, and prints:

```
URL:  http://<vps-ip>:8443/mcp
Auth: Authorization: Bearer <key>
```

### 2. On every client (Mac / Windows / fresh laptop)

Append to `~/.yashigatakae/secrets.env`:

```
BIFROST_URL=http://<vps-ip>:8443/mcp
BIFROST_API_KEY=<key from step 1>
KINTSUGI_KEY=<key from step 1>
# OPTIONAL: your personal state repo (skills + custom hooks + graphify wikis)
# STATE_REPO_URL=git@github.com:<you>/yashigatakae-state.git
```

Then run:

```bash
yashigatakae init
```

The init flow:
- Renders embedded templates (CLAUDE.md, settings.json, secrets.example.env) into `~/.claude/` and `~/.yashigatakae/`. **No private skills are pulled from the public yashigatakae repo** — the templates ship inside the binary.
- Installs the embedded caveman hooks into `~/.claude/hooks/`.
- If `STATE_REPO_URL` is set, clones your personal state repo and overlays its `skills/` + `hooks/` on top.
- Registers `bifrost` as the single MCP server in `~/.claude/settings.json` with the right `Authorization` header.

Inside Claude Code, you'll see `mempalace_recall`, `mempalace_remember`, `mempalace_forget`, `mempalace_stats` available as MCP tools.

### Optional: create your personal state repo

```bash
yashigatakae state init                    # creates <you>/yashigatakae-state private,
                                           # clones to ~/.yashigatakae/state,
                                           # writes STATE_REPO_URL into secrets.env
```

Requires the [`gh` CLI](https://cli.github.com) authenticated as the account you want the repo created under. The new repo is forked from the public template at `oyash01/yashigatakae-state-template`. Skill names, hook overrides, and graphify wikis live there — never in the public yashigatakae tool repo.

### 3. The travel scenario

```bash
# On Mac, mid-session in ~/Desktop/myproject:
yashigatakae handoff --note "switching to laptop"
# → resume code: 0efd264a7747

# On the laptop:
yashigatakae resume
# → ✓ resumed session abc-123
# → claude --continue abc-123
```

You're back at the same turn with the same project memory.

---

## Subcommand reference

```
yashigatakae init [--vps] [--state-repo PATH] [--skip-gstack]
yashigatakae doctor          # 22-24 health checks
yashigatakae status          # doctor + drift (binary vs latest, state-repo vs origin)
yashigatakae upgrade [--tag v0.x.y] [--state]
yashigatakae sync            # state pull + render
yashigatakae caveman <lite|full|ultra|off>

yashigatakae mempalace remember <text> [--project] [--tags] [--source]
yashigatakae mempalace recall   <query> [--top] [--project] [--json]
yashigatakae mempalace forget   <id>
yashigatakae mempalace stats    [--json]
yashigatakae mempalace serve    [--addr 127.0.0.1:8765]

yashigatakae bifrost serve [--addr :8443] [--mempalace URL] [--api-key K]

yashigatakae kintsugi serve [--addr :8444] [--data DIR] [--api-key K]
yashigatakae handoff [--note] [--memory] [--dry-run]
yashigatakae resume [session-id] [--cwd] [--dry-run]
yashigatakae sessions ls
yashigatakae sessions checkpoints <session-id>

yashigatakae graphify <repo> [--refresh] [--out DIR]

yashigatakae hermes enqueue --project X --prompt "..." [--cwd D]
yashigatakae hermes ls [--status pending|running|done|failed|cancelled] [--limit N]
yashigatakae hermes logs <id>
yashigatakae hermes cancel <id>
yashigatakae hermes serve [--poll DUR] [--claude PATH] [--no-lessons]

yashigatakae hooks sweep         # SessionEnd: parse transcript → mempalace
yashigatakae hooks autocommit    # PostToolUse: rsync state-repo → git add
yashigatakae secrets pull|push   # SSH-keyed sync of ~/.yashigatakae/secrets.env
```

---

## Configuration

Everything lives in three places:

```
~/.claude/                       # Claude Code's home (skills, hooks, settings, projects)
~/.yashigatakae/                 # yashigatakae's home
  state/                         # cloned yashigatakae-state repo
  secrets.env                    # BIFROST_URL, BIFROST_API_KEY, KINTSUGI_KEY
  mempalace.db                   # sqlite memory store
  hermes.db                      # sqlite task queue
  hermes/logs/                   # per-task log files
  caveman.json                   # current compression level
  kintsugi/                      # (VPS only) encrypted session blobs
github.com/oyash01/
  yashigatakae                   # binary source + GitHub Actions releases
  yashigatakae-state             # private — skills, hooks, templates, codebase wikis
```

---

## Always-on (Linux/systemd)

Once `init --vps` lands the four systemd units (`mempalace`, `bifrost`, `kintsugi`, `hermes`), make them perpetual with one command:

```bash
sudo yashigatakae enable     # systemctl enable --now all four units
```

Each unit ships with `Restart=always`, `RestartSec=3`, `StartLimitIntervalSec=0`, `TimeoutStopSec=10`, `KillSignal=SIGTERM`. The `StartLimitIntervalSec=0` line specifically disables systemd's default "5 starts in 10 s → mark failed" rate limit, so even a pathological tight crash-loop keeps retrying instead of going dormant. Reboots, OS upgrades, network drops, OOM kills, manual `systemctl stop` — none of it sticks. The services come back. They stop only when you explicitly run:

```bash
sudo yashigatakae disable    # systemctl disable --now all four units
```

(Mac/Win clients don't have always-on daemons in v0.9; the kintsugi watcher is slated for a future release.)

## Repo architecture (v0.9+)

```
oyash01/yashigatakae               PUBLIC   the Go binary + this README + installer
oyash01/yashigatakae-state-template PUBLIC  the empty scaffold for `state init`
<you>/yashigatakae-state           PRIVATE  YOUR skills, hook overrides, codebase wikis
                                            — created by `yashigatakae state init`
```

The public yashigatakae repo no longer ships any private skills. Every required template + caveman hook script is **embedded in the binary itself** at compile time (see `internal/state/embedded/`). A fresh `init` works without ever talking to GitHub for state. Your personal state repo overlays on top when configured, but is strictly optional.

## Roadmap

- **v0.1** ✓ Skeleton + `init` (Mac/Linux/Windows). gstack wrap, caveman hooks, state-repo render, doctor.
- **v0.2** ✓ mempalace + bifrost. Lifetime semantic memory store, single MCP gateway, SessionEnd auto-sweep, VPS systemd deploy.
- **v0.3** ✓ kintsugi (the travel scenario): age-encrypted handoff/resume.
- **v0.4** ✓ graphify minimal: per-repo `overview.md`, `recent.md`, `index.md`, `files.json`.
- **v0.5** ✓ hermes background self-learning agent (queue + worker + lesson distillation).
- **v0.6** ✓ self-update + drift-aware status.
- **v0.7** ✓ README polish + initial completion tag.
- **v0.8** ✓ past-session backfill + half-way workspace + Bubble Tea TUI.
- **v0.9** ← *we are here* — state-repo decoupled (embedded templates + per-user STATE_REPO_URL); always-on systemd hardening (`yashigatakae enable`).

Future polish:
- v0.4.1 LSP/tree-sitter for richer graphify output (call/import graph)
- v0.3.1 working-tree sync (uncommitted git changes packed into kintsugi blobs)
- v0.5.1 hermes HTTP API for remote enqueue, parallel workers
- v0.7.1 fsnotify watcher (kintsugi auto-checkpoint), vanity domain `get.yashigatakae.sh`

---

## License

MIT.

---

by [@OYash01](https://github.com/oyash01) · TeamOYash Technologies
