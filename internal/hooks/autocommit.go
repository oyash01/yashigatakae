package hooks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// RunAutocommit is invoked from the PostToolUse(Edit|Write) hook. It rsyncs
// the user's ~/.claude/{skills,hooks} into the state-repo working copy and
// auto-commits any changes. Pushes are deferred to `yashigatakae sync` to
// avoid latency on every edit.
//
// Defensive: never returns a non-zero exit — hook failures must not block
// the user's editor flow.
func RunAutocommit() {
	if err := autocommit(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "yashigatakae hooks autocommit: %v\n", err)
	}
}

func autocommit(ctx context.Context) error {
	yashDir, err := osdetect.YashigatakaeDir()
	if err != nil {
		return err
	}
	claudeDir, err := osdetect.ClaudeDir()
	if err != nil {
		return err
	}
	stateDir := filepath.Join(yashDir, "state")
	if _, err := os.Stat(filepath.Join(stateDir, ".git")); err != nil {
		return nil // state repo not present — nothing to do
	}

	// Sync managed dirs from ~/.claude into the state-repo working copy.
	for _, sub := range []string{"skills", "hooks"} {
		src := filepath.Join(claudeDir, sub) + string(os.PathSeparator)
		dst := filepath.Join(stateDir, sub)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		// Best-effort rsync; fall back to a tar pipe if rsync is missing.
		if _, lookErr := exec.LookPath("rsync"); lookErr == nil {
			cmd := exec.CommandContext(ctx, "rsync", "-a", "--delete", src, dst+string(os.PathSeparator))
			cmd.Stdout, cmd.Stderr = io_discard(), io_discard()
			_ = cmd.Run()
		} else {
			// Fallback: cp -r overwrite. Less precise (won't delete) but works on Windows where rsync isn't standard.
			_ = copyTreeOverwrite(src, dst)
		}
	}

	// Commit if there are changes.
	statusCmd := exec.CommandContext(ctx, "git", "-C", stateDir, "status", "--porcelain")
	out, _ := statusCmd.Output()
	if len(out) == 0 {
		return nil
	}
	addCmd := exec.CommandContext(ctx, "git", "-C", stateDir, "add", "-A")
	if err := addCmd.Run(); err != nil {
		return err
	}
	msg := "auto: yashigatakae sync " + time.Now().UTC().Format(time.RFC3339)
	commitCmd := exec.CommandContext(ctx, "git", "-C", stateDir,
		"-c", "user.name=yashigatakae",
		"-c", "user.email=yashigatakae@local",
		"commit", "-m", msg, "--quiet")
	_ = commitCmd.Run()
	return nil
}
