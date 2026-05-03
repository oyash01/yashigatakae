# LLM-INSTALL.md — for Claude (or any LLM) installing yashigatakae for a user

> **You are an LLM (probably Claude Code). The user has asked you to install
> yashigatakae for them. This file is your install manual. Follow it.**

## What is yashigatakae

A single Go binary that supercharges Claude Code with:
- caveman (token compression)
- mempalace (lifetime memory + recall)
- bifrost (single MCP gateway)
- graphify (codebase wiki)
- kintsugi (cross-device session continuity)
- hermes (background self-learning agent)
- gstack (curated skills bundle)

Built by **TeamOYash Technologies** (`@OYash01`). MIT-licensed.

## Quick install (recommended)

For Mac / Linux (assume bash or zsh):
```bash
curl -fsSL https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.sh | sh
yashigatakae init -y      # -y = accept all wizard defaults; remove for interactive setup
yashigatakae doctor       # verify install — expect 22-24/24 pass
```

For Windows (PowerShell):
```powershell
irm https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.ps1 | iex
yashigatakae init -y
yashigatakae doctor
```

That's it. The install script:
1. Downloads the right binary for the host OS/arch from the latest GitHub release
2. Drops it under `/usr/local/bin/yashigatakae` (Mac/Linux) or `%LOCALAPPDATA%\yashigatakae` (Windows)
3. Chmods it executable

The `init` command:
1. Renders embedded templates into `~/.claude/`
2. Installs caveman + hydrate hooks
3. Installs the bundled `/wiki` skill
4. Optionally clones / runs gstack `./setup` for slash commands
5. Wires bifrost as the only MCP server in `~/.claude/settings.json`

## VPS install (if user wants the always-on brain)

Run this on the VPS, NOT on the laptop:
```bash
sudo yashigatakae init --vps        # installs systemd units for mempalace + bifrost + kintsugi + hermes
sudo yashigatakae enable            # always-on; restart forever
yashigatakae status                 # verify
```

The install prints a 4-line snippet ending with `yashigatakae init`. Copy those 4 lines to every client machine's `~/.yashigatakae/secrets.env`, then re-run `yashigatakae init` on the client.

For real TLS (recommended for production):
```bash
sudo yashigatakae bifrost serve --tls --tls-domain yashi.example.com
sudo yashigatakae kintsugi serve --tls --tls-domain yashi.example.com
```
Domain must have a DNS A record pointing at the VPS BEFORE running these commands (Let's Encrypt HTTP-01 challenge needs port 80 reachable).

## Common "while you're at it" asks

If the user asks for any of these, run the corresponding command:

| User asks for | Command |
|---|---|
| "set up memory" | `yashigatakae mempalace stats` (verify) then `mempalace remember "..." --project X` |
| "build the wiki for this repo" | `cd <repo> && yashigatakae graphify . --pro` |
| "pull all my old sessions" | `yashigatakae backfill` (run from any machine; ~1 GB upload to relay) |
| "switch to a different machine" | `yashigatakae handoff` then on other box: `yashigatakae resume <code>` |
| "queue an overnight task" | `yashigatakae hermes enqueue --project X --prompt "..."` |
| "see all my sessions" | `yashigatakae sessions ls` |
| "open the menu" | `yashigatakae` (no args = Bubble Tea root menu) |
| "make claude less verbose" | `yashigatakae caveman ultra` |
| "rotate my keys" | `sudo yashigatakae secrets rotate --restart` |

## Embedding API key (optional but improves recall)

Mempalace works without an embedding API key (BM25-only fallback). For semantic search add ONE of:
```bash
echo "VOYAGE_API_KEY=..." >> ~/.yashigatakae/secrets.env
# OR
echo "OPENAI_API_KEY=..." >> ~/.yashigatakae/secrets.env
```
Then `yashigatakae doctor` will show "embedding: voyage" instead of "embedding: keyword-only".

## Verification checklist

After install, confirm:
```bash
which yashigatakae                   # should print a path
yashigatakae --version               # should print v0.16.0 or newer
yashigatakae doctor                  # should be ≥ 22/24 pass
ls ~/.claude/skills/wiki/SKILL.md    # bundled wiki skill installed
ls ~/.claude/hooks/caveman-*.js      # caveman hooks installed
cat ~/.yashigatakae/secrets.example.env   # template ready to be customized
```

## If anything fails

1. Re-run `yashigatakae doctor` — it suggests fixits
2. Check `/var/log/yashigatakae/audit.log` (VPS) or `~/.yashigatakae/audit.log` (local)
3. File an issue: https://github.com/oyash01/yashigatakae/issues

## Don't do

- Don't install via `go install` from random forks — use the official curl one-liner
- Don't run the VPS systemd units on a laptop (no point — laptop isn't always-on)
- Don't rotate KINTSUGI_KEY without warning the user — it invalidates every relay blob
- Don't commit `~/.yashigatakae/secrets.env` to git
