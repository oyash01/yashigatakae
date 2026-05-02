// Package doctor verifies a yashigatakae install on this machine and prints
// fixits. Run via `yashigatakae doctor` or `yashigatakae status`.
package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oyash01/yashigatakae/internal/caveman"
	"github.com/oyash01/yashigatakae/internal/deps"
	"github.com/oyash01/yashigatakae/internal/gstack"
	"github.com/oyash01/yashigatakae/internal/osdetect"
)

type check struct {
	name string
	pass bool
	hint string
}

// Run prints a green/red checklist + an exit summary. Returns nil even if
// some checks fail (the user reads the output and acts).
func Run() error {
	osID := osdetect.Detect()
	home, _ := osdetect.HomeDir()
	claudeDir, _ := osdetect.ClaudeDir()
	yashDir, _ := osdetect.YashigatakaeDir()

	fmt.Printf("yashigatakae doctor — os=%s home=%s\n\n", osID, home)

	var checks []check

	// ── deps
	for _, s := range deps.Check() {
		c := check{name: "dep:" + s.Name, pass: s.OK}
		if !s.OK {
			c.hint = s.Hint
		}
		checks = append(checks, c)
	}

	// ── ~/.claude exists + writable
	checks = append(checks, dirOK("~/.claude", claudeDir))
	checks = append(checks, dirOK("~/.yashigatakae", yashDir))

	// ── settings.json valid
	checks = append(checks, settingsJSONCheck(claudeDir))

	// ── caveman hooks installed
	cavOK, _ := caveman.HooksInstalled()
	checks = append(checks, check{name: "caveman hooks", pass: cavOK, hint: "re-run `yashigatakae init` to install"})

	// ── gstack present
	if gp, err := gstack.Path(); err == nil {
		_, statErr := os.Stat(filepath.Join(gp, ".git"))
		checks = append(checks, check{name: "gstack repo", pass: statErr == nil, hint: "re-run `yashigatakae init`"})
	}

	// ── flat skill names linked (browse, qa, ship)
	checks = append(checks, skillLinked(claudeDir, "browse"))
	checks = append(checks, skillLinked(claudeDir, "qa"))
	checks = append(checks, skillLinked(claudeDir, "ship"))

	// ── bundled custom skills
	for _, s := range []string{"caveman", "graphify", "backup-sync", "deploy-multi", "env-verify", "proxy-check", "re-extract"} {
		checks = append(checks, skillLinked(claudeDir, s))
	}

	// ── CLAUDE.md sections
	for _, marker := range []string{"## gstack", "## caveman"} {
		checks = append(checks, claudeMDHas(claudeDir, marker))
	}

	pass := 0
	for _, c := range checks {
		mark := "✗"
		if c.pass {
			mark = "✓"
			pass++
		}
		fmt.Printf("  %s %s", mark, c.name)
		if !c.pass && c.hint != "" {
			fmt.Printf("  — %s", c.hint)
		}
		fmt.Println()
	}
	fmt.Printf("\n%d/%d checks passed\n", pass, len(checks))
	return nil
}

// Status is a compact per-machine status print used by `yashigatakae status`.
// v0.1 just calls Run; v0.6 will add cluster + drift info.
func Status() error {
	return Run()
}

func dirOK(label, p string) check {
	if p == "" {
		return check{name: label, pass: false, hint: "could not determine path"}
	}
	if _, err := os.Stat(p); err != nil {
		return check{name: label, pass: false, hint: fmt.Sprintf("missing — re-run `yashigatakae init`")}
	}
	return check{name: label, pass: true}
}

func settingsJSONCheck(claudeDir string) check {
	p := filepath.Join(claudeDir, "settings.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return check{name: "settings.json", pass: false, hint: "missing — re-run `yashigatakae init`"}
	}
	var any map[string]any
	if err := json.Unmarshal(b, &any); err != nil {
		return check{name: "settings.json", pass: false, hint: "invalid JSON — fix manually or re-run init"}
	}
	return check{name: "settings.json valid", pass: true}
}

func skillLinked(claudeDir, name string) check {
	p := filepath.Join(claudeDir, "skills", name)
	if _, err := os.Stat(p); err == nil {
		return check{name: "skill:" + name, pass: true}
	}
	return check{name: "skill:" + name, pass: false, hint: "not installed yet"}
}

func claudeMDHas(claudeDir, marker string) check {
	p := filepath.Join(claudeDir, "CLAUDE.md")
	b, err := os.ReadFile(p)
	if err != nil {
		return check{name: "CLAUDE.md " + marker, pass: false, hint: "CLAUDE.md missing"}
	}
	if strings.Contains(string(b), marker) {
		return check{name: "CLAUDE.md " + marker, pass: true}
	}
	return check{name: "CLAUDE.md " + marker, pass: false, hint: "section missing — re-run init"}
}
