// Package state is the v0.1 init orchestrator. It clones (or uses a local copy
// of) yashigatakae-state, renders templates into ~/.claude/, copies hooks and
// skills, runs gstack ./setup, registers the placeholder MCP, and registers
// the caveman + auto-commit hooks in settings.json.
package state

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/oyash01/yashigatakae/internal/deps"
	"github.com/oyash01/yashigatakae/internal/gstack"
	"github.com/oyash01/yashigatakae/internal/hooks"
	"github.com/oyash01/yashigatakae/internal/mcp"
	"github.com/oyash01/yashigatakae/internal/osdetect"
)

const (
	stateRepoURL = "https://github.com/oyash01/yashigatakae-state.git"
)

type InitOptions struct {
	VPS            bool
	GitHub         bool
	LocalStateRepo string // when set, skip git clone and use this path instead (dogfood mode)
	SkipGstack     bool   // skip the gstack ./setup step (dogfood / CI)
}

// Run executes the v0.1 init flow.
func Run(opts InitOptions) error {
	fmt.Println("yashigatakae init — bootstrapping this machine\n")

	osID := osdetect.Detect()
	home, err := osdetect.HomeDir()
	if err != nil {
		return err
	}
	claudeDir, err := osdetect.ClaudeDir()
	if err != nil {
		return err
	}
	yashDir, err := osdetect.YashigatakaeDir()
	if err != nil {
		return err
	}
	fmt.Printf("  os=%s  home=%s  claude=%s  state=%s\n\n", osID, home, claudeDir, yashDir)

	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return err
	}
	for _, sub := range []string{"hooks", "skills", "agents", "projects"} {
		_ = os.MkdirAll(filepath.Join(claudeDir, sub), 0o755)
	}
	if err := os.MkdirAll(yashDir, 0o755); err != nil {
		return err
	}

	// 1. dependency status (informational)
	fmt.Println("[1/8] Dependency check")
	statuses := deps.Check()
	fmt.Print(deps.FormatTable(statuses))
	if !deps.AllOK(statuses) {
		fmt.Println("  ! some deps missing — install them and re-run.")
	}
	fmt.Println()

	// 2. obtain state repo
	fmt.Println("[2/8] State repo")
	stateDir, err := obtainStateRepo(yashDir, opts.LocalStateRepo)
	if err != nil {
		return err
	}
	fmt.Printf("  · using state repo at %s\n\n", stateDir)

	// 3. render templates → ~/.claude/
	fmt.Println("[3/8] Render templates")
	if err := renderTemplates(stateDir, claudeDir, home); err != nil {
		return err
	}
	fmt.Println()

	// 4. copy hooks
	fmt.Println("[4/8] Install hooks")
	if err := copyDirContents(filepath.Join(stateDir, "hooks"), filepath.Join(claudeDir, "hooks")); err != nil {
		return err
	}
	fmt.Println()

	// 5. copy skills (preserves any existing skill of same name unless it's clearly stale)
	fmt.Println("[5/8] Install bundled skills")
	if err := copySkills(filepath.Join(stateDir, "skills"), filepath.Join(claudeDir, "skills")); err != nil {
		return err
	}
	fmt.Println()

	// 6. gstack
	fmt.Println("[6/8] gstack")
	if opts.SkipGstack {
		fmt.Println("  · --skip-gstack set — skipping gstack ./setup")
	} else {
		if err := gstack.Install(); err != nil {
			fmt.Printf("  ! gstack install failed: %v\n  (continuing — re-run later)\n", err)
		}
	}
	fmt.Println()

	// 7. settings.json hooks + MCP placeholder
	fmt.Println("[7/8] Register hooks + MCP placeholder in settings.json")
	specs := defaultHookSpecs(claudeDir, osID)
	if err := hooks.Register(specs); err != nil {
		return err
	}
	if err := mcp.RegisterPlaceholder(); err != nil {
		return err
	}
	fmt.Println()

	// 8. CLAUDE.md sections
	fmt.Println("[8/8] CLAUDE.md sections")
	if err := ensureClaudeMDSections(claudeDir, stateDir); err != nil {
		return err
	}

	fmt.Println("\n✓ yashigatakae init complete. Run `yashigatakae doctor` to verify.")
	return nil
}

func obtainStateRepo(yashDir, localOverride string) (string, error) {
	if localOverride != "" {
		abs, err := filepath.Abs(localOverride)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("--state-repo path %s: %w", abs, err)
		}
		return abs, nil
	}

	dest := filepath.Join(yashDir, "state")
	if _, err := os.Stat(filepath.Join(dest, ".git")); os.IsNotExist(err) {
		fmt.Printf("  · cloning yashigatakae-state into %s\n", dest)
		clone := exec.Command("git", "clone", "--single-branch", "--depth", "1", stateRepoURL, dest)
		clone.Stdout = os.Stdout
		clone.Stderr = os.Stderr
		if err := clone.Run(); err != nil {
			return "", fmt.Errorf("git clone yashigatakae-state: %w (private repo — auth via gh or a deploy key)", err)
		}
	} else {
		pull := exec.Command("git", "-C", dest, "pull", "--ff-only")
		_ = pull.Run() // non-fatal
	}
	return dest, nil
}

// renderTemplates expands every *.tmpl file under stateDir/templates into
// claudeDir, dropping the .tmpl suffix. The data map exposes ${HOME} and ${USER}.
func renderTemplates(stateDir, claudeDir, home string) error {
	tmplDir := filepath.Join(stateDir, "templates")
	entries, err := os.ReadDir(tmplDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("  · no templates/ in state repo — skipping")
			return nil
		}
		return err
	}
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	data := map[string]string{
		"HOME": home,
		"USER": user,
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tmpl") {
			continue
		}
		src := filepath.Join(tmplDir, e.Name())
		dst := filepath.Join(claudeDir, strings.TrimSuffix(e.Name(), ".tmpl"))
		raw, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		t, err := template.New(e.Name()).Parse(string(raw))
		if err != nil {
			return fmt.Errorf("parse %s: %w", src, err)
		}
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		if err := t.Execute(f, data); err != nil {
			f.Close()
			return fmt.Errorf("render %s: %w", src, err)
		}
		f.Close()
		fmt.Printf("  · %s → %s\n", e.Name(), dst)
	}
	return nil
}

func copyDirContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("  · no %s in state repo — skipping\n", src)
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			if err := copyDirContents(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	fmt.Printf("  · copied %d entries from %s → %s\n", len(entries), src, dst)
	return nil
}

func copySkills(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("  · no skills/ in state repo — skipping")
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillSrc := filepath.Join(src, e.Name())
		skillDst := filepath.Join(dst, e.Name())
		// gstack manages itself — never overwrite.
		if e.Name() == "gstack" {
			continue
		}
		if err := os.MkdirAll(skillDst, 0o755); err != nil {
			return err
		}
		if err := copyDirContents(skillSrc, skillDst); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func defaultHookSpecs(claudeDir string, os osdetect.OS) []HookSpec {
	hooksDir := filepath.Join(claudeDir, "hooks")
	prefix := func(name string) string {
		return filepath.Join(hooksDir, name)
	}
	type spec = HookSpec
	return []spec{
		{Event: "SessionStart", Type: "command", Cmd: "node " + quote(prefix("caveman-activate.js"))},
		{Event: "UserPromptSubmit", Type: "command", Cmd: "node " + quote(prefix("caveman-mode-tracker.js"))},
		// PostToolUse auto-commit on changes to ~/.claude/skills/** and ~/.claude/CLAUDE.md
		{Event: "PostToolUse", Matcher: "Edit|Write", Type: "command", Cmd: "bash " + quote(prefix("yashigatakae-autocommit.sh"))},
		// SessionEnd memory sweep stub (real impl in v0.2)
		{Event: "SessionEnd", Type: "command", Cmd: "bash " + quote(prefix("yashigatakae-sweep.sh"))},
	}
}

func quote(p string) string {
	if strings.ContainsAny(p, " \t") {
		return `"` + p + `"`
	}
	return p
}

// HookSpec re-exports the hooks.HookSpec for use in defaultHookSpecs without an import cycle.
type HookSpec = hooks.HookSpec

// Render is the public entry point for `yashigatakae state render`.
func Render() error {
	yashDir, _ := osdetect.YashigatakaeDir()
	stateDir := filepath.Join(yashDir, "state")
	claudeDir, _ := osdetect.ClaudeDir()
	home, _ := osdetect.HomeDir()
	return renderTemplates(stateDir, claudeDir, home)
}

// Pull is the public entry point for `yashigatakae state pull`.
func Pull() error {
	yashDir, _ := osdetect.YashigatakaeDir()
	stateDir := filepath.Join(yashDir, "state")
	cmd := exec.Command("git", "-C", stateDir, "pull", "--ff-only")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Sync is the v0.1 stub for `yashigatakae sync` (full impl in v0.6).
func Sync() error {
	fmt.Println("sync — v0.1 stub. Pulling state repo and re-rendering templates...")
	if err := Pull(); err != nil {
		return err
	}
	return Render()
}

// HelpLink is shown for the v0.6 `link` stub.
func HelpLink() string {
	return "link — register a new machine in the cluster (v0.6)."
}
