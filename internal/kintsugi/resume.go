package kintsugi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// ResumeOptions captures all CLI flags for `yashigatakae resume`.
type ResumeOptions struct {
	BaseURL     string
	APIKey      string
	KintsugiKey string

	SessionID  string // resume a specific session (default = pick most-recent on relay)
	TargetCWD  string // override target working dir for memory restoration; default = SourceCWD from manifest
	DryRun     bool   // download + decrypt + show what WOULD change, do not write
	Auto       bool   // after restore, print `claude --continue <id>` instead of relying on user
}

// ResumeReport is what Resume returns to the caller (CLI prints).
type ResumeReport struct {
	Manifest        Manifest
	TranscriptPath  string
	MemoryRestored  bool
	BytesDownloaded int64
	Worktree        RestoreReport
	WorktreeError   string
}

// Resume runs the v0.3.0-rc2 resume flow:
//  1. If SessionID is empty, list relay sessions and pick the most-recent
//     one whose latest checkpoint is newest.
//  2. Download latest checkpoint blob.
//  3. Decrypt with KINTSUGI_KEY.
//  4. Unpack tarball to a temp dir, read the manifest.
//  5. Move session.jsonl into ~/.claude/projects/<encoded(target_cwd)>/<sid>.jsonl.
//  6. If memory was packed, merge memory/ into ~/.claude/projects/<encoded(target_cwd)>/memory/.
func Resume(ctx context.Context, opts ResumeOptions) (ResumeReport, error) {
	resolved, err := resolveEnv(opts.BaseURL, opts.APIKey, opts.KintsugiKey)
	if err != nil {
		return ResumeReport{}, err
	}
	client := NewClient(resolved.relayBase, resolved.apiKey)

	sid := opts.SessionID
	if sid == "" {
		sid, err = pickLatestSession(ctx, client)
		if err != nil {
			return ResumeReport{}, err
		}
	}
	cipher, err := client.GetLatest(ctx, sid)
	if err != nil {
		return ResumeReport{}, fmt.Errorf("download latest for session %s: %w", sid, err)
	}
	plain, err := Decrypt(cipher, resolved.kintsugiKey)
	if err != nil {
		return ResumeReport{}, fmt.Errorf("decrypt (wrong KINTSUGI_KEY?): %w", err)
	}

	scratch, err := os.MkdirTemp("", "kintsugi-resume-*")
	if err != nil {
		return ResumeReport{}, err
	}
	defer os.RemoveAll(scratch)

	mf, err := Unpack(plain, scratch)
	if err != nil {
		return ResumeReport{}, err
	}
	if mf.Version != "kintsugi-1" {
		return ResumeReport{}, fmt.Errorf("unsupported manifest version %q", mf.Version)
	}

	report := ResumeReport{
		Manifest:        mf,
		BytesDownloaded: int64(len(cipher)),
	}

	targetCWD := opts.TargetCWD
	if targetCWD == "" {
		targetCWD = mf.SourceCWD
	}
	encoded := encodeCWD(targetCWD)
	claude, _ := osdetect.ClaudeDir()
	projectDir := filepath.Join(claude, "projects", encoded)
	transcriptDst := filepath.Join(projectDir, mf.SessionID+".jsonl")

	if opts.DryRun {
		fmt.Printf("  dry-run — would restore:\n")
		fmt.Printf("    transcript:  %s\n", transcriptDst)
		if mf.HasMemory {
			fmt.Printf("    memory:      %s\n", filepath.Join(projectDir, "memory"))
		}
		report.TranscriptPath = transcriptDst
		return report, nil
	}

	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return report, err
	}
	if err := copyOver(filepath.Join(scratch, sessionTranscriptName), transcriptDst); err != nil {
		return report, fmt.Errorf("restore transcript: %w", err)
	}
	report.TranscriptPath = transcriptDst

	if mf.HasMemory {
		memSrc := filepath.Join(scratch, "memory")
		memDst := filepath.Join(projectDir, "memory")
		if _, err := os.Stat(memSrc); err == nil {
			if err := mergeTree(memSrc, memDst); err != nil {
				return report, fmt.Errorf("restore memory: %w", err)
			}
			report.MemoryRestored = true
		}
		// MEMORY.md sits next to memory/, not inside it.
		metaSrc := filepath.Join(scratch, "MEMORY.md")
		if _, err := os.Stat(metaSrc); err == nil {
			_ = copyOver(metaSrc, filepath.Join(projectDir, "MEMORY.md"))
		}
		// Subagents → projects/<encoded>/<sid>/subagents/
		subSrc := filepath.Join(scratch, "subagents")
		if _, err := os.Stat(subSrc); err == nil {
			subDst := filepath.Join(projectDir, mf.SessionID, "subagents")
			_ = mergeTree(subSrc, subDst)
		}
		// Todos → ~/.claude/todos/
		todoSrc := filepath.Join(scratch, "todos")
		if _, err := os.Stat(todoSrc); err == nil {
			todoDst := filepath.Join(claude, "todos")
			_ = mergeTree(todoSrc, todoDst)
		}
	}

	// Worktree restore — only attempted if the manifest carried diff/untracked AND
	// the target cwd is a git repo.
	if mf.HasWorktree {
		wt := &WorktreeCapture{
			Branch:        mf.WorktreeBranch,
			HeadSHA:       mf.WorktreeHeadSHA,
			ExcludedRules: mf.WorktreeExcluded,
		}
		if b, err := os.ReadFile(filepath.Join(scratch, worktreeDiffName)); err == nil {
			wt.Diff = b
		}
		if b, err := os.ReadFile(filepath.Join(scratch, worktreeUntrackedName)); err == nil {
			wt.UntrackedTar = b
		}
		wtRep, err := RestoreWorktree(targetCWD, wt)
		if err != nil {
			report.WorktreeError = err.Error()
		} else {
			report.Worktree = wtRep
		}
	}

	return report, nil
}

// pickLatestSession lists all sessions on the relay and picks the one whose
// most-recent checkpoint has the newest timestamp.
func pickLatestSession(ctx context.Context, client *Client) (string, error) {
	sids, err := client.ListSessions(ctx)
	if err != nil {
		return "", err
	}
	if len(sids) == 0 {
		return "", errors.New("no sessions on relay yet")
	}
	bestSID := ""
	bestTS := ""
	for _, sid := range sids {
		cps, err := client.ListCheckpoints(ctx, sid)
		if err != nil || len(cps) == 0 {
			continue
		}
		latest := cps[len(cps)-1].TS
		if latest > bestTS {
			bestTS = latest
			bestSID = sid
		}
	}
	if bestSID == "" {
		return "", errors.New("no usable checkpoints found")
	}
	return bestSID, nil
}

// copyOver writes src bytes to dst, replacing.
func copyOver(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// mergeTree copies src→dst recursively, overwriting matching files.
func mergeTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		return copyOver(path, out)
	})
}
