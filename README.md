<div align="center">

# yashigatakae

**Claude Code, supercharged. One Go binary.**

Lifetime memory · Codebase wikis · Cross-device session continuity · Background agents · MCP gateway · Token compression

[![release](https://img.shields.io/github/v/release/oyash01/yashigatakae)](https://github.com/oyash01/yashigatakae/releases)
[![go](https://img.shields.io/badge/go-1.23-blue)](https://go.dev)
[![license](https://img.shields.io/badge/license-MIT-green)](LICENSE)

</div>

---

## What it does, in 5 seconds

> Open Claude Code on **any** of your machines and pick up the same conversation, with the same memory, the same uncommitted code, and the same background agents — automatically.

```bash
curl -fsSL https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.sh | sh
yashigatakae init
```

That's it. The interactive wizard asks you 7 yes/no questions and wires everything up.

---

## What you get after `init`

| Pillar | What it does | Token saved per session |
|---|---|---:|
| **caveman** | Auto-compresses every response, truncates huge tool outputs | ~50–60% |
| **mempalace** | Lifetime memory; `recall` / `remember` MCP tools; auto-recalls top-5 entries on session start | ~10K |
| **bifrost** | One MCP gateway in front of N tool servers (no token bloat from re-registering) | ~5K |
| **graphify** | Karpathy-style LLM Wiki for any repo: index + module pages + symbol pages + ADRs + glossary | ~10K |
| **kintsugi** | Hand off a session on one device, resume it on another — transcript + memory + uncommitted code | ∞ |
| **hermes** | Background self-learning agent. Queue overnight tasks; lessons land in mempalace for next session | ∞ |
| **gstack** | Wraps `garrytan/gstack` (`/qa`, `/browse`, `/ship`, …) with flat slash commands | — |

All seven boot from one binary. No Docker, no Python venvs, no Node lockfiles.

---

## Install

### Mac / Linux / VPS
```bash
curl -fsSL https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.sh | sh
yashigatakae init           # interactive wizard
```

### Windows
```powershell
irm https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.ps1 | iex
yashigatakae init
```

### VPS (always-on brain)
```bash
sudo yashigatakae init --vps    # systemd units + ufw + TLS + audit + rate limit
sudo yashigatakae enable        # restart forever; never goes down
```
Then on every Mac/Win/Linux client:
```bash
echo "BIFROST_URL=https://your-vps:8443/mcp"  >> ~/.yashigatakae/secrets.env
echo "BIFROST_API_KEY=<paste from VPS output>" >> ~/.yashigatakae/secrets.env
echo "KINTSUGI_KEY=<paste from VPS output>"   >> ~/.yashigatakae/secrets.env
yashigatakae init
```

CI / scripted installs: `yashigatakae init -y` skips the wizard and accepts every default.

---

## The travel scenario

```bash
# On your Mac, mid-session in some repo with uncommitted changes
yashigatakae handoff --note "leaving for trip"
# → handoff code: ABC-123

# On your Windows desktop (or fresh laptop)
yashigatakae resume ABC-123
claude --continue
```

The Claude Code session continues at the exact turn it left off. `git status` shows the same uncommitted edits. Project memory + todos restored. No "let me re-read the codebase" loop.

---

## Generate the codebase wiki

```bash
cd ~/your-repo
yashigatakae graphify . --pro
```

Produces `~/.yashigatakae/state/codebase-wiki/your-repo/` containing:

- `index.md` — hub page with infobox (HEAD, file count, primary lang, etc.)
- `architecture.md` — module table with sizes + symbol counts
- `modules/<name>.md` — one page per module
- `symbols/<name>.md` — one page per public symbol with signature + doc + backlinks
- `DECISIONS.md` — ADRs auto-extracted from substantive git commits
- `GLOSSARY.md` — domain terms + where they're defined
- `STUB-PAGES.md` — concepts referenced but not yet documented
- `_meta/citations.json` — provenance for every fact

The bundled `/wiki` skill auto-instructs Claude to read the wiki before answering anything about the repo. Saves Claude ~10K tokens per session.

---

## Use from inside Claude Code

```
/recall ghostnode proxy
/wiki                            # auto-loads index.md for cwd
/mcp_call hermes_enqueue '{"project":"X","prompt":"audit proxy latency over 1h"}'
```

Or from the shell:
```bash
yashigatakae mempalace remember "decided to use age encryption for at-rest" --project yashigatakae
yashigatakae mempalace recall "encryption" --category decision
yashigatakae mempalace hydrate --top 5
yashigatakae hermes enqueue --project ghostnode --prompt "..." --idempotency-key nightly-audit
yashigatakae hermes schedule add "0 9 * * 1-5" --project metrics --prompt "morning report"
```

---

## How it stays online

- **systemd `Restart=always`** + `StartLimitIntervalSec=0` — services restart forever
- **TLS** via Let's Encrypt autocert (or self-signed fallback)
- **Per-key rate limiting** + JSONL audit log + fail2ban template
- **At-rest encryption** for mempalace.db / hermes.db (age + HKDF, locked when stopped)
- **`yashigatakae secrets rotate --restart`** — one command rotates BIFROST_API_KEY + KINTSUGI_KEY and restarts everything

See [`docs/vps-setup.md`](docs/vps-setup.md) for the full hardening guide.

---

## Architecture (one-liner)

```
GitHub  →  Mac / Win / Linux clients  ⇄  VPS brain (mempalace, bifrost, kintsugi, hermes)
            (Claude Code runs locally)        (memory + relay only — never runs your code)
```

Coding stays on your laptop. The VPS is the brain that remembers everything for you.

Full design: [`docs/architecture.md`](docs/architecture.md).

---

## Documentation

- [`docs/getting-started.md`](docs/getting-started.md) — first-time install on a fresh box
- [`docs/vps-setup.md`](docs/vps-setup.md) — production VPS install (TLS, ufw, fail2ban)
- [`docs/architecture.md`](docs/architecture.md) — how the seven pillars fit together
- [`docs/commands.md`](docs/commands.md) — every CLI subcommand
- [`docs/LLM-INSTALL.md`](docs/LLM-INSTALL.md) — paste this URL into Claude and ask it to install yashigatakae for you

---

## Doctor

```
$ yashigatakae doctor
  ✓ binary on PATH
  ✓ ~/.yashigatakae exists
  ✓ secrets.env present (perm 0600)
  ✓ caveman hooks installed (5)
  ✓ embedded skills installed (1: wiki)
  ✓ mempalace.db readable, 1247 entries
  ✓ bifrost reachable (https://yashi.example.com:8443/health → 200)
  ✓ hermes worker active (last claim 4s ago)
  ✓ kintsugi relay reachable
  …
  Result: 24 / 24 pass
```

---

## Built by

**[TeamOYash Technologies](https://github.com/oyash01)** — `@OYash01`

Reverse-engineered by hand. No frameworks, no scaffolding tools, no leaky abstractions.
Every byte audited; every release dogfooded.

---

## License

MIT. Use it, fork it, ship products on it.
