# Changelog

All notable changes are documented here. Tags are SemVer (v0.X.Y).

## v0.16.2 — 2026-05-03

**Workflow YAML fix.**

- Removed `---` doc-separator from the release-notes heredoc (YAML parser
  was treating it as a new-document marker, failing every release run).
- Replaced heredoc with a plain `{ echo … } >>` block. CI now extracts
  the matching CHANGELOG.md section and appends install/verify/docs links.

## v0.16.1 — 2026-05-03

**CHANGELOG-driven release notes + go 1.25.**

- New `CHANGELOG.md` with the full version history.
- `.github/workflows/release.yml` now extracts the per-tag section from
  CHANGELOG.md and uses it as the release body.
- CI go-version bumped 1.21 → 1.25 to match go.mod.

## v0.16.0 — 2026-05-03

**Interactive setup wizard + docs site.**

- New `init` is interactive by default on a TTY. 9-step Bubble Tea wizard
  asks: gstack / backfill scope / MCP gateway / wiki / hermes / caveman level
  / doctor. After collecting choices, runs the full pipeline end-to-end.
- `init -y` skips the wizard and accepts every default (CI / scripted installs).
- README rewritten "what does it do in 5 seconds" — tagline + value table at the top.
- New `docs/` tree: getting-started, vps-setup, architecture, commands, LLM-INSTALL.
- AUTHORS + CODEOWNERS — TeamOYash Technologies / @oyash01 as sole author.

## v0.15.0 — 2026-05-03

**Hydration Hook — auto-recall on SessionStart.**

- New `mempalace hydrate` subcommand registered as a SessionStart hook.
  Top-5 project-scoped entries auto-pulled from the local mempalace and
  injected as hidden context every session start. No `/recall` needed.
- Recency fallback when query-driven recall under-fills: project entries
  that don't mention the project name verbatim still surface.

## v0.14.0 — 2026-05-03

**Graphify pro — Karpathy LLM Wiki generator.**

- Full page taxonomy: index.md (hub w/ infobox), architecture.md,
  MODULES.md, modules/<name>.md, symbols/<name>.md, DECISIONS.md,
  CONVENTIONS.md, DEPENDENCIES.md, GLOSSARY.md, STUB-PAGES.md,
  RECENT-CHANGES.md, what-links-here.md, _meta/citations.json.
- Wikilink renderer with stub detection. `graphify check` exits non-zero
  if any link is broken.
- Symbol extraction for Go / Python / TS / JS / Rust (regex-based).
- Dependency parser for go.mod / package.json / requirements.txt /
  Cargo.toml / pyproject.toml.
- ADR extractor scans git log for substantive commits with decision keywords.
- Bundled `/wiki` skill auto-installed via `init`.

## v0.13.0 — 2026-05-03

**Mempalace pro — dedupe + categorize + hybrid recall + decay + consolidate.**

- Schema migration adds: `category`, `source_machine`, `merged_into` columns.
- Dedupe on Remember: cosine ≥ 0.95 OR normalized-string match folds new
  insert into existing row.
- Heuristic categorizer: user_pref / observation / fact / decision / error /
  code_snippet / url / lesson / misc.
- Hybrid recall: cosine + BM25 → reciprocal rank fusion → time-decay.
- Consolidate: roll up old entries into a summary, archive originals.

## v0.12.0 — 2026-05-03

**Hermes pro — idempotency + DLQ + priorities + dependencies + cron + builtin MCP.**

- Schema adds: priority, idempotency_key, retry_count, max_retries,
  next_attempt_at, dependency_id, dlq_reason, scheduled_at.
- Idempotency keys dedupe within 7-day window.
- Exponential backoff (30s × 10^attempt, capped 8h, max 5 retries) → DLQ.
- Built-in 5-field cron parser. `hermes schedule add "<cron>"`.
- Concurrency: `--concurrency N` runs N parallel claude subprocesses.
- bifrost serves `hermes_enqueue` + `hermes_status` as builtin MCP tools
  — Mac client can queue background work from inside Claude Code.

## v0.11.0 — 2026-05-03

**Caveman pro — auto-compaction + tool-result truncation + prompt cache.**

- Auto context compaction at configurable threshold (default 100k tokens,
  2k summary target).
- PreToolUse hook truncates tool results > per-tool cap (Bash 4 KiB,
  Read 8 KiB, default 2 KiB), spills full output to /tmp/caveman/<sha8>.txt.
- Prompt cache marker (`cache_control: ephemeral`) for the static prompt prefix.
- Verbosity profile (terse / normal / verbose) independent of compression.

## v0.10.0 — 2026-05-02

**Security hardening — TLS + audit + rate limit + at-rest encryption + key rotation.**

- TLS via Let's Encrypt autocert OR self-signed (no Caddy required).
- JSONL audit log + middleware on bifrost + kintsugi.
- Per-key token-bucket rate limiter (200/min authed, 60/min unauth).
- At-rest sqlite encryption via age + HKDF (lock on stop, unlock on start).
- `secrets rotate` regenerates BIFROST_API_KEY + KINTSUGI_KEY, restarts services.
- VPS install opens UFW ports + prints client snippet.
- fail2ban template ships in `installers/`.

## v0.9.0 — 2026-05-02

**State-repo decoupled + always-on hardening.**

- Templates + caveman hooks now embedded in the binary via `go:embed`.
  No external state repo required for `init` to work.
- `STATE_REPO_URL` env var or `--state-repo` flag for the optional overlay.
- systemd hardening: `Restart=always`, `StartLimitIntervalSec=0`,
  `KillSignal=SIGTERM`, `TimeoutStopSec=10`.
- `enable` / `disable` subcommands wire/unwire all 4 services in one shot.
- Mac launchd + Windows scheduled-task fallbacks.

## v0.8.0 — 2026-05-02

**Past-session backfill + half-way workspace + interactive TUI.**

- Eager backfill: `~/.claude/projects/**/*.jsonl` packed + encrypted +
  uploaded to relay on first init. Idempotent ledger at
  `~/.yashigatakae/backfill.json`.
- Working-tree capture: kintsugi handoff now packs `git diff --binary` +
  untracked tar. Resume applies via `git apply --3way` with conflict picker.
- Bubble Tea TUI: root menu, sessions browser (fuzzy filter), conflict picker.

## v0.7.0 — 2026-05-01

Polish + vanity domain prep. Token rotation surface, README quickstart,
telemetry off-by-default.

## v0.6.0 — 2026-05-01

Self-update + drift. `upgrade` subcommand, `status` shows per-machine drift.

## v0.5.0 — 2026-05-01

Hermes worker + self-learning loop.

## v0.4.0 — 2026-05-01

Graphify v1: per-repo overview/index/recent/files.json.

## v0.3.0 — 2026-05-01

Kintsugi watcher + handoff/resume baseline.

## v0.2.0 — 2026-05-01

Mempalace + bifrost MCP gateway baseline.

## v0.1.0 — 2026-05-01

Skeleton + `init` (Mac / Linux / Windows). gstack wrap. caveman hooks.
