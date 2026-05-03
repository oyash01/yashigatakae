# Getting Started

Five minutes from `curl` to first session.

## 1. Install the binary

**Mac / Linux:**
```bash
curl -fsSL https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.ps1 | iex
```

The script downloads the right binary for your OS+arch from the latest GitHub release, drops it on your PATH, and verifies the checksum. No package manager needed.

## 2. Initialize this machine

```bash
yashigatakae init
```

The interactive wizard asks 7 questions:

1. Install gstack? (skills like `/qa`, `/browse`, `/ship`)
2. Backfill prior Claude sessions? (scope: all / 30d / current project)
3. Register bifrost as your MCP gateway?
4. Build the LLM Wiki for this codebase?
5. Start hermes (background self-learning agent)?
6. Activate caveman? (token compression — pick lite / full / ultra)
7. Run doctor at the end?

Press `→` (or `enter`) to accept each default. Esc cancels at any time.

If you're scripting (CI / Dockerfile), use `yashigatakae init -y` to skip the wizard.

## 3. Check it worked

```bash
yashigatakae doctor
```

You should see 22-24 passing checks. Anything red comes with a fixit suggestion.

## 4. Open Claude Code

Open Claude Code in any repo. On every session start you'll see:

- caveman ruleset injected (token compression)
- `## hydrate (project=X)` block with the top-5 most relevant memories pulled from mempalace
- The `/wiki` skill loaded if a wiki exists for this cwd

Try:
```
/recall ghostnode proxy
```

…to manually pull memories. Or:
```bash
yashigatakae mempalace remember "decided to use age encryption" --project myproject
```

…to seed memory from the shell.

## 5. Build the wiki for your repo

```bash
cd ~/your-repo
yashigatakae graphify . --pro
```

Wiki lands at `~/.yashigatakae/state/codebase-wiki/your-repo/`. Open `index.md` in any markdown viewer; the `/wiki` skill auto-loads it for Claude.

## 6. (Optional) Connect a VPS for cross-device + always-on

See [`vps-setup.md`](vps-setup.md). Five minutes, one root SSH session, prints a 4-line snippet you paste into every laptop.

---

## Where things live

| Path | What |
|---|---|
| `~/.yashigatakae/secrets.env` | API keys, BIFROST_URL, KINTSUGI_KEY |
| `~/.yashigatakae/mempalace.db` | sqlite memory store |
| `~/.yashigatakae/hermes.db` | sqlite task queue |
| `~/.yashigatakae/state/codebase-wiki/<repo>/` | per-repo wiki |
| `~/.yashigatakae/backfill.json` | ledger of past-session uploads |
| `~/.claude/hooks/caveman-*.js` | session-start + truncate hooks |
| `~/.claude/skills/wiki/SKILL.md` | bundled wiki skill |
| `~/.claude/settings.json` | MCP registration (managed) |

## Uninstall

```bash
rm /usr/local/bin/yashigatakae
rm -rf ~/.yashigatakae
# leave ~/.claude alone — that's Claude Code's, not ours
```

Removes everything yashigatakae installed except the gstack skill clone (delete `~/.claude/skills/gstack` separately if you want to remove that too).
