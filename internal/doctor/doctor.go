// Package doctor verifies a yashigatakae install on this machine and prints
// fixits. Run via `yashigatakae doctor` or `yashigatakae status`.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/oyash01/yashigatakae/internal/caveman"
	"github.com/oyash01/yashigatakae/internal/deps"
	"github.com/oyash01/yashigatakae/internal/gstack"
	"github.com/oyash01/yashigatakae/internal/mempalace"
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

	// ── mempalace store (sqlite db opens cleanly + schema applied)
	checks = append(checks, mempalaceCheck())

	_ = home // (silence unused if any future code path needs it)

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

// Status is `yashigatakae status` — runs all doctor checks plus enriched
// drift information (binary version vs latest release, state-repo HEAD vs
// origin/main, mempalace counts).
func Status() error {
	if err := Run(); err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("drift")
	binCheck()
	stateCheck()
	return nil
}

func binCheck() {
	tag, err := fetchLatestTag("oyash01/yashigatakae")
	if err != nil {
		fmt.Printf("  ! can't reach github.com to check latest release: %v\n", err)
		return
	}
	fmt.Printf("  · binary: latest release on GitHub = %s (run `yashigatakae upgrade` to swap)\n", tag)
}

func stateCheck() {
	yash, _ := osdetect.YashigatakaeDir()
	stateDir := filepath.Join(yash, "state")
	if _, err := os.Stat(filepath.Join(stateDir, ".git")); err != nil {
		fmt.Println("  · state-repo: not present (run `yashigatakae init`)")
		return
	}
	// Fetch + compare.
	if out, err := runCmd(stateDir, "git", "fetch", "--quiet"); err != nil {
		fmt.Printf("  ! state-repo fetch failed: %s\n", strings.TrimSpace(out))
		return
	}
	local, _ := runCmd(stateDir, "git", "rev-parse", "--short", "HEAD")
	remote, _ := runCmd(stateDir, "git", "rev-parse", "--short", "origin/main")
	local = strings.TrimSpace(local)
	remote = strings.TrimSpace(remote)
	if local == remote {
		fmt.Printf("  · state-repo: in sync (%s)\n", local)
		return
	}
	ahead, _ := runCmd(stateDir, "git", "rev-list", "--count", "origin/main..HEAD")
	behind, _ := runCmd(stateDir, "git", "rev-list", "--count", "HEAD..origin/main")
	fmt.Printf("  ! state-repo: drift — local=%s origin=%s ahead=%s behind=%s (run `yashigatakae sync`)\n",
		local, remote, strings.TrimSpace(ahead), strings.TrimSpace(behind))
}

func fetchLatestTag(repo string) (string, error) {
	resp, err := http.Get("https://api.github.com/repos/" + repo + "/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return rel.TagName, nil
}

func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
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

func mempalaceCheck() check {
	store, err := mempalace.Open()
	if err != nil {
		return check{name: "mempalace store", pass: false, hint: err.Error()}
	}
	defer store.Close()
	stats, err := store.Stats(context.Background())
	if err != nil {
		return check{name: "mempalace store", pass: false, hint: err.Error()}
	}
	return check{name: fmt.Sprintf("mempalace store (%d entries)", stats.TotalEntries), pass: true}
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
