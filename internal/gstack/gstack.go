// Package gstack wraps the upstream gstack installer so `yashigatakae init`
// always lands a fresh or updated gstack at ~/.claude/skills/gstack.
//
// We always pass --no-prefix so skills install under flat names (/qa, /browse, /ship),
// matching the user's confirmed preference.
package gstack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

const (
	repoURL = "https://github.com/garrytan/gstack.git"
)

// Install clones gstack to ~/.claude/skills/gstack if missing, then runs ./setup.
// If already cloned, runs `git pull` followed by ./setup (idempotent — gstack's
// own setup is re-runnable).
func Install() error {
	claudeDir, err := osdetect.ClaudeDir()
	if err != nil {
		return err
	}
	dest := filepath.Join(claudeDir, "skills", "gstack")

	if _, err := os.Stat(filepath.Join(dest, ".git")); os.IsNotExist(err) {
		fmt.Printf("  · cloning gstack into %s\n", dest)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir parent: %w", err)
		}
		clone := exec.Command("git", "clone", "--single-branch", "--depth", "1", repoURL, dest)
		clone.Stdout = os.Stdout
		clone.Stderr = os.Stderr
		if err := clone.Run(); err != nil {
			return fmt.Errorf("git clone gstack: %w", err)
		}
	} else {
		fmt.Printf("  · refreshing gstack at %s\n", dest)
		pull := exec.Command("git", "-C", dest, "pull", "--ff-only")
		pull.Stdout = os.Stdout
		pull.Stderr = os.Stderr
		_ = pull.Run() // non-fatal — local edits are allowed
	}

	// Run ./setup. Pass GSTACK_FLAT=1 if gstack respects it; otherwise the user is
	// prompted at the terminal — `yashigatakae init` is intended to be run interactively.
	setup := exec.Command("./setup")
	setup.Dir = dest
	setup.Env = append(os.Environ(), "GSTACK_NO_PREFIX=1")
	setup.Stdin = os.Stdin
	setup.Stdout = os.Stdout
	setup.Stderr = os.Stderr
	if err := setup.Run(); err != nil {
		return fmt.Errorf("gstack ./setup: %w", err)
	}
	return nil
}

// Path returns ~/.claude/skills/gstack — useful for doctor checks.
func Path() (string, error) {
	claudeDir, err := osdetect.ClaudeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(claudeDir, "skills", "gstack"), nil
}
