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

// State-repo handling (v0.9.0+):
//
// The state-repo is a per-USER, typically PRIVATE GitHub repo where each
// install stores its own custom skills, hooks overrides, and graphify wikis.
// It is OPTIONAL — fresh installs work without it because every required
// template + caveman hook is embedded in the binary itself (see embed.go).
//
// To use a personal state-repo, set STATE_REPO_URL in
// ~/.yashigatakae/secrets.env. Examples:
//
//   STATE_REPO_URL=git@github.com:<you>/yashigatakae-state.git
//   STATE_REPO_URL=https://x-access-token:<gh_pat>@github.com/<you>/yashi-state.git
//
// `yashigatakae state init` (added in v0.9.0) creates one for you from
// the public template oyash01/yashigatakae-state-template.
const stateRepoTemplateRepo = "oyash01/yashigatakae-state-template"

type InitOptions struct {
	VPS            bool
	GitHub         bool
	LocalStateRepo string // when set, skip git clone and use this path instead (dogfood mode)
	SkipGstack     bool   // skip the gstack ./setup step (dogfood / CI)
}

// Run executes the v0.1 init flow.
func Run(opts InitOptions) error {
	if opts.VPS {
		return RunVPS()
	}
	fmt.Println("yashigatakae init — bootstrapping this machine")
	fmt.Println()

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

	// 2. extract embedded templates (always — embedded in the binary)
	fmt.Println("[2/8] Render embedded templates + secrets.example.env")
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	if written, err := extractEmbeddedTemplates(claudeDir, home, user); err != nil {
		return err
	} else {
		for _, p := range written {
			fmt.Printf("  · wrote %s\n", p)
		}
	}
	fmt.Println()

	// 3. extract embedded hooks (caveman + statusline) — always
	fmt.Println("[3/8] Install embedded caveman hooks")
	if written, err := extractEmbeddedHooks(claudeDir); err != nil {
		return err
	} else {
		fmt.Printf("  · installed %d hook script(s)\n", len(written))
	}
	fmt.Println()

	// 4. optional state-repo (per-user, private). Skip if neither
	//    --state-repo NOR STATE_REPO_URL is set.
	fmt.Println("[4/8] Optional personal state repo (skills + custom hooks + wikis)")
	stateDir, err := obtainStateRepo(yashDir, opts.LocalStateRepo)
	if err != nil {
		fmt.Printf("  ! %s\n  (continuing without a state repo — embedded defaults still install)\n", err)
		stateDir = ""
	}
	if stateDir != "" {
		fmt.Printf("  · using state repo at %s\n", stateDir)
		// Layered overrides: hooks/ + skills/ from state repo overlay on top of embedded.
		if err := copyDirContents(filepath.Join(stateDir, "hooks"), filepath.Join(claudeDir, "hooks")); err != nil {
			return err
		}
		if err := copySkills(filepath.Join(stateDir, "skills"), filepath.Join(claudeDir, "skills")); err != nil {
			return err
		}
	} else {
		fmt.Println("  · (no state repo configured — set STATE_REPO_URL in ~/.yashigatakae/secrets.env or run `yashigatakae state init` to create one)")
	}
	fmt.Println()

	// 5. (skills handled inside step 4)
	fmt.Println("[5/8] Skills install handled in step 4 (state repo) + step 3 (caveman hooks)")
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

// obtainStateRepo resolves the state repo path. Order:
//   1. localOverride (e.g. dogfood `--state-repo /path`)
//   2. existing clone at ~/.yashigatakae/state (just `git pull`)
//   3. clone STATE_REPO_URL env var (typically the user's private repo)
//   4. nothing — return ("", nil) so the caller can skip gracefully
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
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		// Already cloned — refresh it.
		pull := exec.Command("git", "-C", dest, "pull", "--ff-only")
		_ = pull.Run() // non-fatal
		return dest, nil
	}

	url := os.Getenv("STATE_REPO_URL")
	if url == "" {
		return "", nil // user hasn't configured one — let caller handle
	}
	fmt.Printf("  · cloning %s into %s\n", url, dest)
	cmd := exec.Command("git", "clone", "--single-branch", "--depth", "1", url, dest)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("clone state repo (set STATE_REPO_URL in ~/.yashigatakae/secrets.env or run `yashigatakae state init`): %w", err)
	}
	return dest, nil
}

// (Legacy multi-URL clone helper removed in v0.9.0 — state-repo is now
// per-user and STATE_REPO_URL-driven. See obtainStateRepo above.)

// renderTemplates expands every *.tmpl file under stateDir/templates into
// claudeDir, dropping the .tmpl suffix. The data map exposes ${HOME} and ${USER}.
//
// IMPORTANT: templates are STARTERS, not authoritative state. If a target file
// already exists in claudeDir (e.g. the user has customized their settings.json
// or CLAUDE.md), we leave it alone — the mcp / hooks / claudemd packages all
// merge into existing files. Overwriting on every init would destroy user data.
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
		dstName := strings.TrimSuffix(e.Name(), ".tmpl")
		dst := filepath.Join(claudeDir, dstName)

		// Skip if destination already has content — preserve user data.
		// Empty files (size 0) are treated as not-yet-initialized and rendered.
		if info, statErr := os.Stat(dst); statErr == nil && info.Size() > 0 {
			fmt.Printf("  · %s exists (%d bytes) — preserved (template is starter only)\n", dstName, info.Size())
			continue
		}

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
		fmt.Printf("  · %s → %s (rendered fresh)\n", e.Name(), dst)
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

func defaultHookSpecs(claudeDir string, _ osdetect.OS) []HookSpec {
	hooksDir := filepath.Join(claudeDir, "hooks")
	prefix := func(name string) string {
		return filepath.Join(hooksDir, name)
	}
	// Use the running yashigatakae binary's absolute path for PostToolUse +
	// SessionEnd hooks, so they work even if the binary isn't on Claude
	// Code's PATH at runtime. Caveman hooks remain Node-script based for now
	// (real reimplementation in Go is a v0.7 polish).
	yashi, err := os.Executable()
	if err != nil || yashi == "" {
		yashi = "yashigatakae" // fallback to PATH lookup
	}
	type spec = HookSpec
	return []spec{
		{Event: "SessionStart", Type: "command", Cmd: "node " + quote(prefix("caveman-activate.js"))},
		{Event: "UserPromptSubmit", Type: "command", Cmd: "node " + quote(prefix("caveman-mode-tracker.js"))},
		{Event: "PreToolUse", Matcher: "Bash|Read|WebFetch", Type: "command", Cmd: "node " + quote(prefix("caveman-truncate.js"))},
		{Event: "PostToolUse", Matcher: "Edit|Write", Type: "command", Cmd: quote(yashi) + " hooks autocommit"},
		{Event: "SessionEnd", Type: "command", Cmd: quote(yashi) + " hooks sweep"},
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
