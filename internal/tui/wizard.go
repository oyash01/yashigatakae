package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WizardChoices captures every yes/no the setup wizard collected. Caller
// (cmd/yashigatakae/main.go newInitCmd) walks through the steps after Run.
type WizardChoices struct {
	InstallGstack    bool
	BackfillSessions bool   // pull every ~/.claude/projects/**/*.jsonl into mempalace + relay
	BackfillScope    string // "all" | "recent30d" | "current-project-only"
	RegisterMCP      bool   // wire bifrost as the only MCP server in settings.json
	BuildWiki        bool   // run `graphify <cwd> --pro`
	StartHermes      bool   // enable always-on hermes worker
	ActivateCaveman  bool
	CavemanLevel     string // "lite" | "full" | "ultra"
	RunDoctor        bool
	VPSURL           string // optional — if set, register bifrost endpoint here
	BifrostKey       string // optional — paired with VPSURL
}

// DefaultChoices is what gets pre-selected before the user touches anything.
// Picked so a "press enter through everything" path produces a sensible setup.
func DefaultChoices() WizardChoices {
	return WizardChoices{
		InstallGstack:    true,
		BackfillSessions: true,
		BackfillScope:    "recent30d",
		RegisterMCP:      true,
		BuildWiki:        true,
		StartHermes:      true,
		ActivateCaveman:  true,
		CavemanLevel:     "full",
		RunDoctor:        true,
	}
}

// RunInitWizard walks the user through the setup choices and returns them.
// Returns ok=false if the user cancelled (Esc / Ctrl-C).
func RunInitWizard() (WizardChoices, bool, error) {
	m := newWizardModel(DefaultChoices())
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return WizardChoices{}, false, err
	}
	wm := final.(wizardModel)
	return wm.choices, !wm.cancelled, nil
}

// step represents one screen of the wizard. The wizard model walks them in
// order. Each step has its own keyboard handler that mutates choices.
type step struct {
	id       string
	title    string
	body     string
	render   func(c WizardChoices) string         // step-specific body, returns the rendered text
	keys     func(msg tea.KeyMsg, c *WizardChoices) (advance bool, back bool)
}

type wizardModel struct {
	choices   WizardChoices
	cur       int
	cancelled bool
	width     int
	height    int
	flash     string // last status line shown under the step body
}

func newWizardModel(c WizardChoices) wizardModel {
	return wizardModel{choices: c}
}

func (m wizardModel) Init() tea.Cmd { return nil }

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	steps := wizardSteps()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		}
		if m.cur >= len(steps) {
			return m, tea.Quit
		}
		advance, back := steps[m.cur].keys(msg, &m.choices)
		if back && m.cur > 0 {
			m.cur--
		} else if advance {
			m.cur++
			if m.cur >= len(steps) {
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m wizardModel) View() string {
	steps := wizardSteps()
	if m.cur >= len(steps) {
		return StyleTitle.Render("✓ wizard complete") + "\n\n"
	}
	st := steps[m.cur]
	header := StyleTitle.Render("yashigatakae setup") + "  " +
		StyleHint.Render(fmt.Sprintf("step %d / %d  ·  press ←/→ to move, q to cancel", m.cur+1, len(steps)))

	body := StyleBox.Render(
		lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(st.title) + "\n\n" +
			st.render(m.choices))

	footer := StyleHint.Render("← back  ·  → / enter accept  ·  y/n toggle  ·  esc cancel")
	return header + "\n\n" + body + "\n" + footer + "\n"
}

// wizardSteps lists every screen in order. Adding a new question = new entry.
func wizardSteps() []step {
	return []step{
		{
			id:    "welcome",
			title: "Welcome",
			render: func(c WizardChoices) string {
				return "yashigatakae will set up your Claude Code on this machine:\n" +
					"  · gstack skills (qa / browse / ship + 7 of yours)\n" +
					"  · caveman (token compression hooks)\n" +
					"  · mempalace (lifetime memory; recall on session start)\n" +
					"  · bifrost (single MCP gateway)\n" +
					"  · hermes (background self-learning agent)\n" +
					"  · graphify (codebase wiki)\n" +
					"  · kintsugi (cross-device session relay)\n\n" +
					"You can press → to take every default, or step through to customize."
			},
			keys: yesNoKeys(nil, true),
		},
		{
			id:    "gstack",
			title: "Install gstack?",
			render: func(c WizardChoices) string {
				return choice("Install / refresh gstack skills (qa / browse / ship)", c.InstallGstack)
			},
			keys: yesNoKeys(func(c *WizardChoices, v bool) { c.InstallGstack = v }, true),
		},
		{
			id:    "backfill",
			title: "Pull past Claude sessions into mempalace?",
			render: func(c WizardChoices) string {
				body := choice("Backfill prior session transcripts", c.BackfillSessions) + "\n\n"
				if c.BackfillSessions {
					body += "Scope (press 1 / 2 / 3 to switch):\n"
					body += radio("1", "all  — every transcript under ~/.claude/projects/", c.BackfillScope == "all") + "\n"
					body += radio("2", "recent30d  — last 30 days only (default)", c.BackfillScope == "recent30d") + "\n"
					body += radio("3", "current-project-only  — only the project at the wizard's cwd", c.BackfillScope == "current-project-only") + "\n"
				}
				return body
			},
			keys: func(msg tea.KeyMsg, c *WizardChoices) (bool, bool) {
				switch msg.String() {
				case "y", "Y":
					c.BackfillSessions = true
				case "n", "N":
					c.BackfillSessions = false
				case "1":
					c.BackfillScope = "all"
				case "2":
					c.BackfillScope = "recent30d"
				case "3":
					c.BackfillScope = "current-project-only"
				case "right", "enter":
					return true, false
				case "left":
					return false, true
				}
				return false, false
			},
		},
		{
			id:    "mcp",
			title: "Register bifrost as your MCP gateway?",
			render: func(c WizardChoices) string {
				return choice("Wire bifrost into ~/.claude/settings.json (one MCP endpoint instead of N)", c.RegisterMCP)
			},
			keys: yesNoKeys(func(c *WizardChoices, v bool) { c.RegisterMCP = v }, true),
		},
		{
			id:    "wiki",
			title: "Build the codebase LLM Wiki for your current project?",
			render: func(c WizardChoices) string {
				body := choice("Generate Karpathy-style LLM wiki for $(pwd)", c.BuildWiki) + "\n\n"
				body += "  · index.md hub + architecture + DECISIONS + GLOSSARY\n"
				body += "  · per-module + per-symbol pages\n"
				body += "  · auto-loaded by Claude via the bundled /wiki skill\n"
				return body
			},
			keys: yesNoKeys(func(c *WizardChoices, v bool) { c.BuildWiki = v }, true),
		},
		{
			id:    "hermes",
			title: "Start hermes (background self-learning agent)?",
			render: func(c WizardChoices) string {
				body := choice("Run hermes as an always-on worker", c.StartHermes) + "\n\n"
				body += "  · pulls queued tasks 24/7 (you enqueue from inside Claude Code)\n"
				body += "  · each finished task writes 1–5 lessons to mempalace\n"
				body += "  · future sessions /recall those lessons automatically\n"
				return body
			},
			keys: yesNoKeys(func(c *WizardChoices, v bool) { c.StartHermes = v }, true),
		},
		{
			id:    "caveman",
			title: "Activate caveman (token compression)?",
			render: func(c WizardChoices) string {
				body := choice("Auto-compress responses + truncate huge tool outputs", c.ActivateCaveman) + "\n\n"
				if c.ActivateCaveman {
					body += "Level (press 1 / 2 / 3):\n"
					body += radio("1", "lite  — drop filler, keep articles", c.CavemanLevel == "lite") + "\n"
					body += radio("2", "full  — caveman speak (default)", c.CavemanLevel == "full") + "\n"
					body += radio("3", "ultra — maximum compression", c.CavemanLevel == "ultra") + "\n"
				}
				return body
			},
			keys: func(msg tea.KeyMsg, c *WizardChoices) (bool, bool) {
				switch msg.String() {
				case "y", "Y":
					c.ActivateCaveman = true
				case "n", "N":
					c.ActivateCaveman = false
				case "1":
					c.CavemanLevel = "lite"
				case "2":
					c.CavemanLevel = "full"
				case "3":
					c.CavemanLevel = "ultra"
				case "right", "enter":
					return true, false
				case "left":
					return false, true
				}
				return false, false
			},
		},
		{
			id:    "doctor",
			title: "Run doctor at the end?",
			render: func(c WizardChoices) string {
				return choice("Run all 22+ health checks after install completes", c.RunDoctor)
			},
			keys: yesNoKeys(func(c *WizardChoices, v bool) { c.RunDoctor = v }, true),
		},
		{
			id:    "review",
			title: "Review your choices",
			render: func(c WizardChoices) string {
				var b strings.Builder
				row := func(label string, on bool, extra string) {
					mark := "✗"
					if on {
						mark = "✓"
					}
					if extra != "" {
						extra = "  (" + extra + ")"
					}
					fmt.Fprintf(&b, "  %s  %s%s\n", mark, label, extra)
				}
				row("Install gstack", c.InstallGstack, "")
				row("Backfill past sessions", c.BackfillSessions, c.BackfillScope)
				row("Register MCP gateway", c.RegisterMCP, "")
				row("Build codebase wiki", c.BuildWiki, "")
				row("Start hermes worker", c.StartHermes, "")
				row("Activate caveman", c.ActivateCaveman, c.CavemanLevel)
				row("Run doctor", c.RunDoctor, "")
				b.WriteString("\nPress → to apply, ← to go back and change something.\n")
				return b.String()
			},
			keys: yesNoKeys(nil, true),
		},
	}
}

// yesNoKeys returns a key handler that:
//   - 'y' / 'n' / space toggles a single bool (via setter; nil = no toggle)
//   - right/enter advances; left goes back; esc cancels (handled at root)
//
// requireDecision is currently unused — kept for future steps that should
// not advance until the user explicitly picks.
func yesNoKeys(set func(c *WizardChoices, v bool), _ bool) func(tea.KeyMsg, *WizardChoices) (bool, bool) {
	return func(msg tea.KeyMsg, c *WizardChoices) (bool, bool) {
		s := msg.String()
		switch s {
		case "y", "Y":
			if set != nil {
				set(c, true)
			}
		case "n", "N":
			if set != nil {
				set(c, false)
			}
		case " ":
			// space toggles whatever the setter holds (read-modify-write).
			// We can't read the current value through the setter, so we
			// assume the call site doesn't need toggle semantics.
		case "right", "enter":
			return true, false
		case "left":
			return false, true
		}
		return false, false
	}
}

func choice(label string, on bool) string {
	mark := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).Render("[ no]")
	if on {
		mark = lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b")).Render("[YES]")
	}
	return mark + "  " + label + "  " + StyleHint.Render("(y / n)")
}

func radio(key, label string, selected bool) string {
	dot := "○"
	if selected {
		dot = lipgloss.NewStyle().Foreground(colorAccent).Render("●")
	}
	return "  " + dot + " " + StyleHint.Render(key+":") + " " + label
}
