package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// RunRoot is the no-args `yashigatakae` entry point. Opens the root menu;
// returns nil on clean Esc/q exit.
func RunRoot() error {
	p := tea.NewProgram(newRootModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type rootAction string

const (
	actDoctor   rootAction = "doctor"
	actSessions rootAction = "sessions"
	actMempal   rootAction = "mempalace"
	actHermes   rootAction = "hermes"
	actStatus   rootAction = "status"
	actBackfill rootAction = "backfill"
	actExit     rootAction = "exit"
)

type rootItem struct {
	label  string
	hint   string
	action rootAction
}

var rootItems = []rootItem{
	{"Doctor", "Run all health checks", actDoctor},
	{"Sessions browser", "Resume any past or live session", actSessions},
	{"Status", "Drift, version, last sync", actStatus},
	{"Backfill", "Push every local transcript to relay (~1 GB)", actBackfill},
	{"Mempalace", "Recall / remember (coming in TUI v2)", actMempal},
	{"Hermes queue", "View / enqueue tasks (coming in TUI v2)", actHermes},
	{"Exit", "(or press q / Esc)", actExit},
}

type rootModel struct {
	cursor int
	chosen rootAction
	width  int
	height int
}

func newRootModel() rootModel { return rootModel{} }

func (m rootModel) Init() tea.Cmd { return nil }

func (m rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.chosen = actExit
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(rootItems)-1 {
				m.cursor++
			}
		case "enter":
			m.chosen = rootItems[m.cursor].action
			if m.chosen == actExit {
				return m, tea.Quit
			}
			// Hand off to a sub-program. We Quit the root, then the caller
			// inspects m.chosen and runs the right sub-flow.
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m rootModel) View() string {
	var b lipgloss.Style
	_ = b

	header := StyleTitle.Render("yashigatakae") + "  " + StyleHint.Render("press ↑↓ + enter, q to quit")
	rows := ""
	for i, it := range rootItems {
		marker := "  "
		label := it.label
		hint := it.hint
		if i == m.cursor {
			marker = "▶ "
			label = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(label)
		} else {
			label = lipgloss.NewStyle().Foreground(colorFg).Render(label)
		}
		rows += fmt.Sprintf("%s%-22s  %s\n", marker, label, StyleHint.Render(hint))
	}
	body := StyleBox.Render(rows)
	return header + "\n\n" + body + "\n"
}

// Chosen returns the action the user picked, or actExit if they bailed.
func (m rootModel) Chosen() rootAction { return m.chosen }

// RunAndDispatch runs the root model and returns the chosen action so the CLI
// caller can dispatch (the caller owns the sub-flow, not the TUI package, to
// keep tui dependency-free of state/doctor/kintsugi packages).
func RunAndDispatch() (string, error) {
	m := newRootModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	rm, ok := final.(rootModel)
	if !ok {
		return "exit", nil
	}
	return string(rm.Chosen()), nil
}
