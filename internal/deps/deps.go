// Package deps verifies external dependencies needed by yashigatakae init.
package deps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		{"bun", []string{"bun", "--version"}, "install bun (gstack browse compiler): curl -fsSL https://bun.sh/install | bash"},
		{"claude", []string{"claude", "--version"}, "install Claude Code: https://claude.com/claude-code"},
	}
	out := make([]Status, 0, len(checks))
	for _, c := range checks {
		s := Status{Name: c.name, Hint: c.hint}
		path, err := exec.LookPath(c.cmd[0])
		if err != nil {
			// Some installers (bun, cargo, ghcup, ...) drop binaries into well-known
			// user-local dirs that aren't on PATH by default. Probe a small list before
			// giving up — saves the user a fight with their shell rc on first install.
			if alt := lookupFallback(c.cmd[0]); alt != "" {
				path = alt
			} else {
				out = append(out, s)
				continue
			}
		}
		s.Path = path
		args := append([]string{}, c.cmd[1:]...)
		v, _ := exec.Command(path, args...).Output()
		s.Version = strings.TrimSpace(string(v))
		s.OK = true
		out = append(out, s)
	}
	return out
}

// lookupFallback checks common per-user install dirs that aren't usually on
// PATH. Returns "" if nothing found.
func lookupFallback(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidates := []string{
		filepath.Join(home, ".bun", "bin", name),
		filepath.Join(home, ".cargo", "bin", name),
		filepath.Join(home, ".local", "bin", name),
		filepath.Join(home, ".npm-global", "bin", name),
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// FallbackPATH returns the additional PATH entries that hold user-installed
// binaries (~/.bun/bin, ~/.cargo/bin, ~/.local/bin). Use this when shelling
// out to gstack ./setup or other tools that may need bun/cargo on PATH.
func FallbackPATH() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	parts := []string{
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".npm-global", "bin"),
	}
	return strings.Join(parts, string(os.PathListSeparator))
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
