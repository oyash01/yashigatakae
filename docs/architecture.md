# Architecture

```
                    ┌─────────────────────────────────┐
                    │  GitHub  oyash01/yashigatakae   │
                    │     release binaries + docs     │
                    └───────────────┬─────────────────┘
                                    │ install.sh / install.ps1
       ┌────────────────────────────┼────────────────────────────┐
       │                            │                            │
   ┌───▼────┐                  ┌────▼────┐                  ┌────▼─────┐
   │  Mac   │ ── handoff ────▶ │   VPS   │ ◀── resume ──── │ Windows  │
   │        │                  │  brain  │                  │  laptop  │
   │ Claude │                  │  :8443  │                  │          │
   │ Code   │ ─── /mcp ───────▶│  :8444  │                  │          │
   │        │                  │         │                  │          │
   │  cave  │                  │ ┌─────┐ │                  │          │
   │  hyd   │                  │ │ MP  │ │                  │          │
   │  wiki  │                  │ │ BF  │ │                  │          │
   │  kint  │                  │ │ KT  │ │                  │          │
   │        │                  │ │ HM  │ │                  │          │
   │        │                  │ └─────┘ │                  │          │
   └────────┘                  └─────────┘                  └──────────┘
```

## The seven pillars

### caveman — token compression
- SessionStart hook (`caveman-activate.js`) injects the caveman ruleset
- UserPromptSubmit hook (`caveman-mode-tracker.js`) tracks the active level
- PreToolUse hook (`caveman-truncate.js`) shells `yashigatakae caveman truncate` for Bash/Read/WebFetch
  - Outputs >2 KiB get spilled to `/tmp/caveman/<sha8>.txt` and replaced with first-30/last-30-line summary + path marker
- Auto-compaction at 100k tokens (configurable in `~/.yashigatakae/caveman.json`)
- Prompt-cache markers around the static system prompt prefix

Result: ~50–60% token savings on long sessions.

### mempalace — lifetime memory
- sqlite + brute-force cosine (Voyage / OpenAI embeddings, optional)
- Hybrid recall: cosine + BM25 → reciprocal rank fusion → time-decay
- Auto-categorization (heuristic): user_pref / observation / fact / decision / error / code_snippet / url / lesson / misc
- Dedupe on Remember (cosine ≥ 0.95 OR normalized-string match → fold into existing row)
- Hydrate hook auto-pulls top-5 project-scoped entries on every SessionStart
- Consolidate: roll up old entries into summaries, archive originals to `mempalace-archive/<ts>.jsonl`
- Served as MCP tools via the bifrost gateway: `recall`, `remember`, `forget`, `stats`

### bifrost — single MCP gateway
- One HTTP endpoint clients register; bifrost fans out to N downstream MCP servers + adds builtin tools
- Bundled builtins: `hermes_enqueue`, `hermes_status`
- Bearer-token auth, per-key rate limit (200/min authed, 60/min unauth)
- TLS: Let's Encrypt autocert OR self-signed
- JSONL audit log captures every request including 429s

### kintsugi — cross-device session relay
- File-system watcher on `~/.claude/sessions/` + `~/.claude/projects/<encoded-cwd>/`
- Handoff packs: session JSONL + memory/ + MEMORY.md + subagents/ + todos/ + working-tree diff (tracked) + tar (untracked)
- Resume reverses everything; conflict picker (Bubble Tea) for divergent uncommitted edits
- Backfill walks every `~/.claude/projects/**/*.jsonl` and uploads (encrypted) on first init
- Idempotent ledger at `~/.yashigatakae/backfill.json` — re-runs skip already-uploaded transcripts

### graphify — Karpathy LLM Wiki
- Generator pipeline: inventory + symbols (regex per language) + deps (5 lockfile parsers) + ADRs (git log) + glossary
- Pages: `index.md` (hub w/ infobox), `architecture.md`, `MODULES.md`, `modules/<name>.md`, `symbols/<name>.md`, `DECISIONS.md`, `CONVENTIONS.md`, `DEPENDENCIES.md`, `GLOSSARY.md`, `STUB-PAGES.md`, `RECENT-CHANGES.md`, `what-links-here.md`, `_meta/citations.json`
- Wikilink syntax `[[Foo]]` rewrites to relative markdown links during render; unresolved → STUB-PAGES.md entry
- `graphify check <repo>` exits non-zero if any wikilink is broken (CI gate)
- Bundled `/wiki` skill auto-loads index.md before Claude answers anything about the repo

### hermes — background self-learning agent
- sqlite queue + worker loop (`Restart=always` systemd unit on VPS)
- Idempotency keys (7-day window) — same key returns existing task id
- Exponential backoff + DLQ (default 5 retries, 30s × 10^attempt, 8h cap)
- Priorities (1–10, default 5), dependencies (`--depends-on ID`)
- Built-in cron scheduler (5-field expression; no third-party dep)
- Concurrency: `--concurrency N` runs N parallel claude subprocesses
- After every successful task, captured output → mempalace as a "lesson" entry tagged `lesson,hermes,task:<id>`

### gstack — curated skills bundle
- Wraps `garrytan/gstack` with flat slash commands (`/qa`, `/browse`, `/ship`, …)
- Plus 7 of the operator's own skills: backup-sync, deploy-multi, env-verify, graphify, proxy-check, re-extract, caveman

## Data flow on a fresh `init`

```
yashigatakae init
   │
   ├─▶ extract embedded templates  → ~/.claude/{settings.json,CLAUDE.md}
   ├─▶ extract embedded hooks       → ~/.claude/hooks/caveman-*.js
   ├─▶ extract embedded skills      → ~/.claude/skills/wiki/SKILL.md
   ├─▶ optional state-repo overlay  → ~/.yashigatakae/state/  (git clone STATE_REPO_URL)
   ├─▶ install gstack               → ~/.claude/skills/gstack/  (./setup --no-prefix)
   ├─▶ register hooks in settings   → ~/.claude/settings.json (managed block)
   ├─▶ register bifrost as MCP      → ~/.claude/settings.json (managed block)
   ├─▶ append managed sections      → ~/.claude/CLAUDE.md
   │
   ├─▶ (wizard) backfill            → upload past sessions to relay
   ├─▶ (wizard) graphify --pro      → ~/.yashigatakae/state/codebase-wiki/<cwd>/
   ├─▶ (wizard) enable services     → systemctl enable --now yashigatakae-*
   └─▶ (wizard) doctor              → 22-24/24 health checks
```

## Repo layout

```
yashigatakae/
├── cmd/yashigatakae/main.go           # cobra root + every subcommand
├── internal/
│   ├── atrest/                        # age + HKDF for sqlite at-rest
│   ├── audit/                         # JSONL audit + middleware + ratelimit
│   ├── bifrost/                       # MCP gateway + builtins
│   ├── caveman/                       # config + compact + truncate + cache
│   ├── deps/                          # dependency check (git, curl, node, claude)
│   ├── doctor/                        # 22+ health checks
│   ├── graphify/                      # wiki generator + page renderers
│   ├── gstack/                        # gstack ./setup wrapper
│   ├── hermes/                        # store + worker + cron
│   ├── hooks/                         # autocommit + sweep + spec writers
│   ├── kintsugi/                      # handoff + resume + worktree + backfill
│   ├── mcp/                           # settings.json MCP server registration
│   ├── mempalace/                     # store + recall (hybrid) + hydrate + consolidate
│   ├── osdetect/                      # mac/linux/windows + ~/.yashigatakae path
│   ├── secrets/                       # rotate
│   ├── state/                         # init flow + embed FS + repo create
│   ├── tls/                           # autocert + self-signed
│   └── tui/                           # Bubble Tea: root menu + wizard + sessions browser
├── installers/
│   ├── install.sh / install.ps1
│   ├── yashigatakae.fail2ban.conf + .filter.conf
├── docs/
│   ├── getting-started.md
│   ├── vps-setup.md
│   ├── architecture.md
│   ├── commands.md
│   └── LLM-INSTALL.md
└── .github/workflows/release.yml      # cross-compile 5 platforms on every tag
```

## Why one binary

Three reasons:

1. **No runtime conflicts.** Python venvs, Node lockfiles, and Docker images are great until you have to keep five of them in sync across three machines.
2. **Cold-start latency = 0.** A statically linked Go binary opens in <50 ms. Hooks called on every prompt need that.
3. **Cross-compile.** One source tree, `GOOS=darwin GOARCH=arm64 go build` → ship. The release workflow does this five times per tag.

## Dependencies (top-level)

- `github.com/spf13/cobra` — CLI
- `github.com/charmbracelet/bubbletea` + bubbles + lipgloss — TUI
- `github.com/modelcontextprotocol/go-sdk` — MCP server + client
- `modernc.org/sqlite` — pure-Go sqlite (no CGO; cross-compile clean)
- `filippo.io/age` — at-rest encryption
- `golang.org/x/crypto/acme/autocert` — Let's Encrypt
- `github.com/fsnotify/fsnotify` — kintsugi watcher

Full list: `go.mod`.
