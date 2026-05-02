// Package deps verifies external dependencies needed by yashigatakae init.
package deps

import (
	"fmt"
	"os/exec"
	"strings"
)

type Status struct {
	Name    string
	Path    string
	Version string
	OK      bool
	Hint    string
}

// Check returns a status for each binary yashigatakae shells out to during init.
// We don't fail hard here — the caller decides what's blocking vs nice-to-have.
func Check() []Status {
	checks := []struct {
		name string
		cmd  []string
		hint string
	}{
		{"git", []string{"git", "--version"}, "install git: https://git-scm.com/downloads"},
		{"curl", []string{"curl", "--version"}, "curl ships on macOS/Linux by default; on Windows install via winget"},
		{"node", []string{"node", "--version"}, "install Node 20+: https://nodejs.org/ (gstack browse needs it)"},
		{"npm", []string{"npm", "--version"}, "ships with node"},
		{"claude", []string{"claude", "--version"}, "install Claude Code: https://claude.com/claude-code"},
	}
	out := make([]Status, 0, len(checks))
	for _, c := range checks {
		s := Status{Name: c.name, Hint: c.hint}
		path, err := exec.LookPath(c.cmd[0])
		if err != nil {
			out = append(out, s)
			continue
		}
		s.Path = path
		v, _ := exec.Command(c.cmd[0], c.cmd[1:]...).Output()
		s.Version = strings.TrimSpace(string(v))
		s.OK = true
		out = append(out, s)
	}
	return out
}

// FormatTable renders a status slice as a small text table.
func FormatTable(statuses []Status) string {
	var sb strings.Builder
	for _, s := range statuses {
		mark := "✗"
		ver := s.Hint
		if s.OK {
			mark = "✓"
			ver = s.Version
			if len(ver) > 60 {
				ver = ver[:60] + "..."
			}
		}
		sb.WriteString(fmt.Sprintf("  %s %-8s %s\n", mark, s.Name, ver))
	}
	return sb.String()
}

// AllOK returns true only if every dependency check passed.
func AllOK(statuses []Status) bool {
	for _, s := range statuses {
		if !s.OK {
			return false
		}
	}
	return true
}
