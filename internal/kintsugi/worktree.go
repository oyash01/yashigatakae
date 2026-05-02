package kintsugi

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// WorktreeCapture is what `yashigatakae handoff` collects from the active git
// repo before packing. The fields slot directly into PackOptions.
type WorktreeCapture struct {
	Diff           []byte   // git diff --binary HEAD (tracked modifications)
	UntrackedTar   []byte   // tar of untracked files (post-gitignore, post-excludes)
	Branch         string   // current branch
	HeadSHA        string   // git rev-parse HEAD
	ExcludedRules  []string // glob patterns honored
}

// DefaultExcludeGlobs is the list of paths the worktree-capture step always skips,
// regardless of what's in user excludes file. Defends against shipping secrets.
var DefaultExcludeGlobs = []string{
	".env", ".env.*", "secrets.*", ".secrets.*",
	"id_rsa", "id_ed25519", "id_ecdsa", "id_*", "*.pem", "*.key",
	".git/", "node_modules/", "dist/", "build/", ".next/",
	".venv/", "venv/", "__pycache__/",
	".DS_Store",
}

// CaptureWorktree runs git in `cwd` and produces a WorktreeCapture. Returns nil
// (without error) if cwd is not a git repository — handoff is still useful for
// non-git projects, just without worktree state.
//
// extraExcludes is appended to DefaultExcludeGlobs.
func CaptureWorktree(cwd string, extraExcludes []string) (*WorktreeCapture, error) {
	if !isGitRepo(cwd) {
		return nil, nil
	}
	wc := &WorktreeCapture{
		ExcludedRules: append(append([]string{}, DefaultExcludeGlobs...), extraExcludes...),
	}

	if out, err := runGit(cwd, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		wc.Branch = strings.TrimSpace(string(out))
	}
	if out, err := runGit(cwd, "rev-parse", "HEAD"); err == nil {
		wc.HeadSHA = strings.TrimSpace(string(out))
	}

	// Tracked modifications: git diff --binary HEAD captures both text + binary
	// changes; --no-color so it's apply-able verbatim.
	if out, err := runGit(cwd, "diff", "--binary", "--no-color", "HEAD"); err == nil && len(out) > 0 {
		wc.Diff = out
	}

	// Untracked files (post-gitignore): git ls-files --others --exclude-standard.
	rawUntracked, err := runGit(cwd, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return wc, nil // keep diff, lose untracked — better than total failure
	}
	if len(rawUntracked) > 0 {
		paths := splitNul(string(rawUntracked))
		paths = filterPaths(paths, wc.ExcludedRules)
		if len(paths) > 0 {
			tarBytes, err := tarFiles(cwd, paths)
			if err != nil {
				return wc, fmt.Errorf("tar untracked: %w", err)
			}
			wc.UntrackedTar = tarBytes
		}
	}

	if wc.Diff == nil && wc.UntrackedTar == nil {
		return nil, nil // clean tree — nothing to capture
	}
	return wc, nil
}

// RestoreReport is returned from RestoreWorktree.
type RestoreReport struct {
	DiffApplied         bool
	DiffConflicts       []string // file paths with merge conflicts after `git apply --3way`
	UntrackedWritten    []string
	UntrackedSkipped    []string // already existed locally, user must resolve via TUI picker
}

// RestoreWorktree applies a captured worktree onto `cwd`. The diff is applied
// via `git apply --3way --whitespace=nowarn` (so divergent local changes get
// real merge markers, surfacable to the TUI conflict picker). Untracked files
// are written through; if a file already exists locally, it lands in
// UntrackedSkipped for the caller to resolve.
//
// Refuses to apply onto a worktree that isn't a git repo — returns the
// captured Diff/UntrackedTar so the caller can offer alternative recovery.
func RestoreWorktree(cwd string, wt *WorktreeCapture) (RestoreReport, error) {
	rep := RestoreReport{}
	if wt == nil {
		return rep, nil
	}
	if !isGitRepo(cwd) {
		return rep, errors.New("target cwd is not a git repository — cannot apply worktree diff")
	}

	if len(wt.Diff) > 0 {
		// Stage 1: try clean apply.
		if err := applyDiff(cwd, wt.Diff, false); err == nil {
			rep.DiffApplied = true
		} else {
			// Stage 2: 3-way apply, which leaves merge markers on conflict but
			// reports a non-zero exit + the conflicting paths on stderr.
			conflicts, err2 := applyDiff3Way(cwd, wt.Diff)
			rep.DiffConflicts = conflicts
			if err2 != nil && len(conflicts) == 0 {
				return rep, fmt.Errorf("apply diff (3way): %w", err2)
			}
			rep.DiffApplied = len(conflicts) == 0
		}
	}

	if len(wt.UntrackedTar) > 0 {
		written, skipped, err := untarFiles(cwd, wt.UntrackedTar)
		if err != nil {
			return rep, fmt.Errorf("untar untracked: %w", err)
		}
		rep.UntrackedWritten = written
		rep.UntrackedSkipped = skipped
	}
	return rep, nil
}

// ── internals ────────────────────────────────────────────────────────────────

func isGitRepo(cwd string) bool {
	out, err := runGit(cwd, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func runGit(cwd string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	return cmd.Output()
}

func splitNul(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\x00")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// filterPaths applies the exclude globs (matched against basename + the full
// rel path with a trailing slash for dir-prefix patterns).
func filterPaths(paths, globs []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		skip := false
		for _, g := range globs {
			if matchExclude(p, g) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, p)
		}
	}
	return out
}

// matchExclude is a simple matcher: "name/" → any path starting with that segment;
// "*.ext" → filepath.Match against the basename; literal → exact basename or
// path equality.
func matchExclude(p, glob string) bool {
	if strings.HasSuffix(glob, "/") {
		prefix := glob
		// match "node_modules/foo" against "node_modules/"
		if strings.HasPrefix(p, prefix) {
			return true
		}
		// also match nested "src/node_modules/..."
		if strings.Contains(p, "/"+prefix) {
			return true
		}
		return false
	}
	if strings.ContainsAny(glob, "*?[") {
		ok, _ := filepath.Match(glob, filepath.Base(p))
		if ok {
			return true
		}
		ok, _ = filepath.Match(glob, p)
		return ok
	}
	return filepath.Base(p) == glob || p == glob
}

func tarFiles(cwd string, paths []string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	now := time.Now().UTC()
	for _, rel := range paths {
		abs := filepath.Join(cwd, rel)
		info, err := os.Lstat(abs)
		if err != nil {
			continue // file vanished between ls-files and now
		}
		if info.IsDir() || (info.Mode()&os.ModeSymlink) != 0 {
			continue // skip dirs (ls-files returns leaves) and symlinks (security)
		}
		body, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:    filepath.ToSlash(rel),
			Mode:    int64(info.Mode().Perm()),
			Size:    int64(len(body)),
			ModTime: now,
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(body); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func untarFiles(cwd string, body []byte) (written, skipped []string, err error) {
	tr := tar.NewReader(bytes.NewReader(body))
	for {
		hdr, e := tr.Next()
		if e != nil {
			break
		}
		if strings.Contains(hdr.Name, "..") {
			continue // path-traversal defense
		}
		dst := filepath.Join(cwd, filepath.FromSlash(hdr.Name))
		if _, statErr := os.Stat(dst); statErr == nil {
			skipped = append(skipped, hdr.Name)
			continue
		}
		if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
			return written, skipped, mkErr
		}
		f, openErr := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
		if openErr != nil {
			return written, skipped, openErr
		}
		buf := make([]byte, hdr.Size)
		if _, readErr := tr.Read(buf); readErr != nil && readErr.Error() != "EOF" {
			f.Close()
			return written, skipped, readErr
		}
		if _, writeErr := f.Write(buf); writeErr != nil {
			f.Close()
			return written, skipped, writeErr
		}
		f.Close()
		written = append(written, hdr.Name)
	}
	return written, skipped, nil
}

func applyDiff(cwd string, diff []byte, threeWay bool) error {
	args := []string{"apply", "--whitespace=nowarn"}
	if threeWay {
		args = append(args, "--3way")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	cmd.Stdin = bytes.NewReader(diff)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git apply: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// applyDiff3Way runs `git apply --3way` and parses the conflict file list out
// of stderr. Returns conflict paths even when err is non-nil (which is normal
// when conflicts exist).
func applyDiff3Way(cwd string, diff []byte) ([]string, error) {
	cmd := exec.Command("git", "apply", "--3way", "--whitespace=nowarn")
	cmd.Dir = cwd
	cmd.Stdin = bytes.NewReader(diff)
	out, err := cmd.CombinedOutput()
	conflicts := parseConflicts(string(out))
	if err != nil && len(conflicts) == 0 {
		return nil, fmt.Errorf("git apply --3way: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return conflicts, nil
}

func parseConflicts(out string) []string {
	var conflicts []string
	for _, line := range strings.Split(out, "\n") {
		// "U	path/to/file" or "Falling back to three-way merge..." — the
		// conflicted file lines start with "U\t" or contain "with conflicts in".
		if strings.HasPrefix(line, "U\t") {
			conflicts = append(conflicts, strings.TrimPrefix(line, "U\t"))
		} else if strings.Contains(line, "conflicts") && strings.Contains(line, ":") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				p := strings.TrimSpace(parts[len(parts)-1])
				if p != "" {
					conflicts = append(conflicts, p)
				}
			}
		}
	}
	return conflicts
}
