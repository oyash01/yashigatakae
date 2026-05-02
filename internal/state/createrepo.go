package state

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// CreateStateRepoOptions controls the `state init` flow.
type CreateStateRepoOptions struct {
	Name        string // repo name (e.g. "yashigatakae-state"); defaults from below
	Owner       string // GitHub user or org (e.g. "rohit"); defaults to the gh-authenticated user
	Private     bool   // create as private (recommended)
	Template    string // template repo (default: oyash01/yashigatakae-state-template)
	NoClone     bool   // don't clone after creating; just print the URL
	WriteSecret bool   // append STATE_REPO_URL=<ssh-url> to ~/.yashigatakae/secrets.env
}

// CreateStateRepo shells out to `gh repo create` to make the user a private
// state repo from the public template, then clones it locally and (optionally)
// writes STATE_REPO_URL into secrets.env so subsequent `init` runs use it.
//
// Requires the gh CLI to be installed and authenticated as the target user.
func CreateStateRepo(opts CreateStateRepoOptions) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("`gh` CLI not found on PATH — install from https://cli.github.com and run `gh auth login` first")
	}
	if opts.Name == "" {
		opts.Name = "yashigatakae-state"
	}
	if opts.Template == "" {
		opts.Template = stateRepoTemplateRepo
	}

	owner := opts.Owner
	if owner == "" {
		// Ask gh for the authenticated user.
		out, err := exec.Command("gh", "api", "user", "--jq", ".login").Output()
		if err != nil {
			return fmt.Errorf("could not detect gh-authenticated user (run `gh auth status`): %w", err)
		}
		owner = strings.TrimSpace(string(out))
	}
	full := owner + "/" + opts.Name

	yashDir, _ := osdetect.YashigatakaeDir()
	clonePath := filepath.Join(yashDir, "state")
	if _, err := os.Stat(filepath.Join(clonePath, ".git")); err == nil && !opts.NoClone {
		return fmt.Errorf("local state repo already exists at %s — remove it first or pass --no-clone", clonePath)
	}

	args := []string{"repo", "create", full,
		"--template", opts.Template,
		"--description", "yashigatakae personal state (skills, hooks, codebase wikis)",
	}
	if opts.Private {
		args = append(args, "--private")
	} else {
		args = append(args, "--public")
	}
	if !opts.NoClone {
		args = append(args, "--clone")
	}

	fmt.Printf("  · gh repo create %s (template=%s, private=%v)\n", full, opts.Template, opts.Private)
	cmd := exec.Command("gh", args...)
	cmd.Dir = filepath.Dir(clonePath)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh repo create: %w", err)
	}

	// `gh repo create --clone` clones into ./<name>; rename to ~/.yashigatakae/state.
	if !opts.NoClone {
		clonedAs := filepath.Join(filepath.Dir(clonePath), opts.Name)
		if _, err := os.Stat(clonedAs); err == nil {
			_ = os.RemoveAll(clonePath)
			if err := os.Rename(clonedAs, clonePath); err != nil {
				return fmt.Errorf("rename %s → %s: %w", clonedAs, clonePath, err)
			}
		}
	}

	if opts.WriteSecret {
		sshURL := fmt.Sprintf("git@github.com:%s.git", full)
		if err := appendSecretLine("STATE_REPO_URL", sshURL); err != nil {
			fmt.Printf("  ! could not write STATE_REPO_URL to secrets.env: %v\n", err)
		} else {
			fmt.Printf("  · STATE_REPO_URL=%s written to ~/.yashigatakae/secrets.env\n", sshURL)
		}
	}

	fmt.Printf("\n✓ created %s and cloned to %s\n", full, clonePath)
	fmt.Println("  next: yashigatakae init   # picks up STATE_REPO_URL automatically")
	return nil
}

// appendSecretLine adds (or replaces) KEY=value in ~/.yashigatakae/secrets.env.
func appendSecretLine(key, value string) error {
	yashDir, err := osdetect.YashigatakaeDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(yashDir, 0o700); err != nil {
		return err
	}
	envPath := filepath.Join(yashDir, "secrets.env")
	body := ""
	if b, err := os.ReadFile(envPath); err == nil {
		body = string(b)
	}
	// Replace existing line if present.
	lines := strings.Split(body, "\n")
	out := lines[:0]
	replaced := false
	for _, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			out = append(out, key+"="+value)
			replaced = true
		} else {
			out = append(out, line)
		}
	}
	if !replaced {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
		out = append(out, key+"="+value, "")
	}
	return os.WriteFile(envPath, []byte(strings.Join(out, "\n")), 0o600)
}
