package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SessionRow is what the browser displays per row.
type SessionRow struct {
	SessionID  string
	Project    string
	Mtime      time.Time
	SizeBytes  int64
	OnLocal    bool
	OnRelay    bool
	SourceCWD  string
	Transcript string // local path (may be empty for relay-only)
}

// SessionsResult is what RunSessionsBrowser returns to the caller.
type SessionsResult struct {
	Action  string // "resume" | "abandon" | "" (cancel)
	Picked  *SessionRow
}

// RunSessionsBrowser renders the browser and blocks until the user picks an
// action. The caller dispatches to kintsugi.Resume / Sessions Abandon.
//
// The TUI package never imports kintsugi — to keep dependencies one-way, the
// caller pre-loads the session list and passes it in.
func RunSessionsBrowser(rows []SessionRow) (SessionsResult, error) {
	m := newSessionsModel(rows)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return SessionsResult{}, err
	}
	sm, _ := final.(sessionsModel)
	return SessionsResult{Action: sm.action, Picked: sm.picked}, nil
}

type sessionsModel struct {
	rows         []SessionRow
	filterRows   []SessionRow
	cursor       int
	filter       string
	action       string
	picked       *SessionRow
	width, height int
}

func newSessionsModel(rows []SessionRow) sessionsModel {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Mtime.After(rows[j].Mtime) })
	return sessionsModel{rows: rows, filterRows: rows}
}

func (m sessionsModel) Init() tea.Cmd { return nil }

func (m sessionsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "q":
			if m.filter == "" {
				return m, tea.Quit
			}
			// when filtering, q is a literal char
			m.filter += "q"
			m.applyFilter()
		case "enter":
			if m.cursor < len(m.filterRows) {
				m.action = "resume"
				row := m.filterRows[m.cursor]
				m.picked = &row
			}
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.filterRows)-1 {
				m.cursor++
			}
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.applyFilter()
			}
		case "/":
			// slash starts/clears filter
			if m.filter != "" {
				m.filter = ""
				m.applyFilter()
			}
		default:
			if len(msg.String()) == 1 {
				m.filter += msg.String()
				m.applyFilter()
			}
		}
	}
	if m.cursor >= len(m.filterRows) {
		m.cursor = len(m.filterRows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	return m, nil
}

func (m *sessionsModel) applyFilter() {
	if m.filter == "" {
		m.filterRows = m.rows
		return
	}
	needle := strings.ToLower(m.filter)
	var out []SessionRow
	for _, r := range m.rows {
		if strings.Contains(strings.ToLower(r.SessionID), needle) ||
			strings.Contains(strings.ToLower(r.Project), needle) ||
			strings.Contains(strings.ToLower(r.SourceCWD), needle) {
			out = append(out, r)
		}
	}
	m.filterRows = out
	m.cursor = 0
}

func (m sessionsModel) View() string {
	header := StyleTitle.Render("sessions") + "  " +
		StyleHint.Render(fmt.Sprintf("%d total · type to filter · enter resume · esc cancel", len(m.rows)))

	listLines := []string{}
	visible := 20
	start := m.cursor - visible/2
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > len(m.filterRows) {
		end = len(m.filterRows)
	}
	for i := start; i < end; i++ {
		r := m.filterRows[i]
		marker := "  "
		idStyle := lipgloss.NewStyle().Foreground(colorFg)
		if i == m.cursor {
			marker = "▶ "
			idStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		badge := StyleBadgeArchive.Render("archive")
		if r.OnLocal && r.OnRelay {
			badge = StyleBadgeActive.Render("synced")
		} else if r.OnLocal && !r.OnRelay {
			badge = StyleBadgeArchive.Render("local")
		} else if !r.OnLocal && r.OnRelay {
			badge = StyleBadgeArchive.Render("relay")
		}
		shortID := r.SessionID
		if len(shortID) > 12 {
			shortID = shortID[:8] + "…"
		}
		when := r.Mtime.Format("01-02 15:04")
		size := humanBytes(r.SizeBytes)
		listLines = append(listLines, fmt.Sprintf("%s%s %-9s  %s  %-20s  %-12s  %s",
			marker, badge, idStyle.Render(shortID), when, truncate(r.Project, 20), size, truncate(r.SourceCWD, 60)))
	}
	if len(m.filterRows) == 0 {
		listLines = append(listLines, StyleHint.Render("(no matches)"))
	}
	list := strings.Join(listLines, "\n")
	footer := StyleHint.Render("filter: ") + m.filter + "_"
	return header + "\n\n" + list + "\n\n" + footer + "\n"
}

func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1fMB", float64(n)/1024/1024)
	default:
		return fmt.Sprintf("%.2fGB", float64(n)/1024/1024/1024)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 4 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// LoadLocalSessions scans ~/.claude/projects/<encoded-cwd>/<uuid>.jsonl and
// returns one SessionRow per file. The TUI caller can merge with relay-side
// rows from kintsugi.Client.ListSessions.
func LoadLocalSessions(claudeDir string) []SessionRow {
	projects := filepath.Join(claudeDir, "projects")
	dirs, err := os.ReadDir(projects)
	if err != nil {
		return nil
	}
	var out []SessionRow
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		projDir := filepath.Join(projects, d.Name())
		jsonls, _ := filepath.Glob(filepath.Join(projDir, "*.jsonl"))
		for _, p := range jsonls {
			info, err := os.Stat(p)
			if err != nil {
				continue
			}
			sid := strings.TrimSuffix(filepath.Base(p), ".jsonl")
			cwd := strings.ReplaceAll(d.Name(), "-", "/")
			out = append(out, SessionRow{
				SessionID:  sid,
				Project:    filepath.Base(cwd),
				Mtime:      info.ModTime(),
				SizeBytes:  info.Size(),
				OnLocal:    true,
				SourceCWD:  cwd,
				Transcript: p,
			})
		}
	}
	return out
}
