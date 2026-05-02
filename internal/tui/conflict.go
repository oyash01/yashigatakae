package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConflictChoice is the user's resolution for a single conflict file.
type ConflictChoice string

const (
	ChoiceKeepLocal     ConflictChoice = "keep-local"
	ChoiceKeepRemote    ConflictChoice = "keep-remote"
	ChoiceStashThenTake ConflictChoice = "stash-then-take-remote"
	ChoiceShowDiff      ConflictChoice = "show-diff"
	ChoiceCancelAll     ConflictChoice = "cancel-all"
)

// ConflictItem describes one file in conflict.
type ConflictItem struct {
	Path        string
	LocalLines  int
	RemoteLines int
	DiffPreview string // optional 5-10 line snippet
}

// ResolveConflicts shows a Bubble Tea picker for each conflict file and
// returns the user's choice per file. ChoiceCancelAll cancels the resume.
func ResolveConflicts(items []ConflictItem) (map[string]ConflictChoice, error) {
	out := map[string]ConflictChoice{}
	for _, it := range items {
		m := newConflictModel(it)
		p := tea.NewProgram(m, tea.WithAltScreen())
		final, err := p.Run()
		if err != nil {
			return out, err
		}
		cm := final.(conflictModel)
		if cm.choice == ChoiceCancelAll {
			out[it.Path] = ChoiceCancelAll
			return out, nil
		}
		out[it.Path] = cm.choice
	}
	return out, nil
}

type conflictModel struct {
	item    ConflictItem
	cursor  int
	choice  ConflictChoice
	options []conflictOpt
}

type conflictOpt struct {
	label  string
	hint   string
	choice ConflictChoice
}

func newConflictModel(item ConflictItem) conflictModel {
	return conflictModel{
		item: item,
		options: []conflictOpt{
			{"Keep local", "Discard the remote version of this file", ChoiceKeepLocal},
			{"Keep remote", "Overwrite local — remote version wins", ChoiceKeepRemote},
			{"Stash then take remote", "git stash local changes, then apply remote", ChoiceStashThenTake},
			{"Show 3-way diff", "Print the diff and re-show this picker", ChoiceShowDiff},
			{"Cancel resume", "Abort the entire restore", ChoiceCancelAll},
		},
	}
}

func (m conflictModel) Init() tea.Cmd { return nil }

func (m conflictModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.choice = ChoiceCancelAll
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "enter":
			m.choice = m.options[m.cursor].choice
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m conflictModel) View() string {
	header := StyleTitle.Render("kintsugi resume conflict")
	subj := lipgloss.NewStyle().Foreground(colorWarn).Bold(true).Render(m.item.Path)
	stat := StyleHint.Render(fmt.Sprintf("local %d lines uncommitted · remote %d lines",
		m.item.LocalLines, m.item.RemoteLines))

	rows := ""
	for i, opt := range m.options {
		marker := "  "
		label := opt.label
		hint := opt.hint
		if i == m.cursor {
			marker = "▶ "
			label = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(label)
		}
		rows += fmt.Sprintf("%s%-26s  %s\n", marker, label, StyleHint.Render(hint))
	}
	body := StyleBox.Render(rows)

	preview := ""
	if m.item.DiffPreview != "" {
		preview = "\n" + StyleHint.Render("preview:") + "\n" +
			lipgloss.NewStyle().Foreground(colorMuted).Render(strings.TrimRight(m.item.DiffPreview, "\n"))
	}

	return header + "\n" + subj + "\n" + stat + "\n\n" + body + preview + "\n"
}
