// Package graphify generates per-repo codebase wikis. v0.4.0-rc1 ships a
// minimal generator: overview.md, index.md, recent.md, files.json. LSP +
// tree-sitter callgraph + Claude-generated prose ship in rc2+.
package graphify

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

func Help() string {
	return `graphify — codebase wiki generator.

  yashigatakae graphify <repo>            # generate wiki under ~/.yashigatakae/state/codebase-wiki/<basename>/
  yashigatakae graphify <repo> --refresh  # force regenerate (default: skip if recent)

v0.4.0-rc1 produces:
  overview.md  — README excerpt + top-level structure summary
  index.md     — file tree, links to other artifacts
  recent.md    — last 30 commits summarized
  files.json   — structured file inventory (path, size, language)

LSP/tree-sitter callgraph + Claude-generated prose ship in v0.4.0-rc2.`
}

// Options for `yashigatakae graphify <repo>`.
type Options struct {
	Repo    string
	Refresh bool
	OutDir  string // override default ~/.yashigatakae/state/codebase-wiki/<base>
}

// Result is what Run returns.
type Result struct {
	WikiDir   string
	Files     int
	Bytes     int64
	GitCommit string
}

// Run is the v0.4.0-rc1 generator.
func Run(opts Options) (Result, error) {
	if opts.Repo == "" {
		return Result{}, fmt.Errorf("repo path required")
	}
	abs, err := filepath.Abs(opts.Repo)
	if err != nil {
		return Result{}, err
	}
	if info, err := os.Stat(abs); err != nil {
		return Result{}, fmt.Errorf("repo %s: %w", abs, err)
	} else if !info.IsDir() {
		return Result{}, fmt.Errorf("repo %s is not a directory", abs)
	}

	wikiDir := opts.OutDir
	if wikiDir == "" {
		yash, err := osdetect.YashigatakaeDir()
		if err != nil {
			return Result{}, err
		}
		wikiDir = filepath.Join(yash, "state", "codebase-wiki", filepath.Base(abs))
	}
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		return Result{}, err
	}

	// 1. Inventory files (gitignore-aware via `git ls-files` if it's a git repo).
	files, err := inventory(abs)
	if err != nil {
		return Result{}, err
	}
	res := Result{WikiDir: wikiDir, Files: len(files)}
	for _, f := range files {
		res.Bytes += f.Size
	}

	// 2. files.json
	if err := writeJSON(filepath.Join(wikiDir, "files.json"), map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"repo":         abs,
		"file_count":   len(files),
		"total_bytes":  res.Bytes,
		"files":        files,
	}); err != nil {
		return res, err
	}

	// 3. recent.md (last 30 commits)
	commits, head := gitRecent(abs, 30)
	res.GitCommit = head
	if err := writeRecentMD(filepath.Join(wikiDir, "recent.md"), commits); err != nil {
		return res, err
	}

	// 4. overview.md
	if err := writeOverviewMD(filepath.Join(wikiDir, "overview.md"), abs, files, head); err != nil {
		return res, err
	}

	// 5. index.md
	if err := writeIndexMD(filepath.Join(wikiDir, "index.md"), abs, files, head); err != nil {
		return res, err
	}

	return res, nil
}

// FileEntry is one row in files.json.
type FileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Lang string `json:"lang,omitempty"`
}

func inventory(repo string) ([]FileEntry, error) {
	// Prefer `git ls-files` so .gitignored stuff (node_modules, vendor) is skipped.
	if out, err := exec.Command("git", "-C", repo, "ls-files").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		entries := make([]FileEntry, 0, len(lines))
		for _, line := range lines {
			if line == "" {
				continue
			}
			full := filepath.Join(repo, line)
			info, err := os.Stat(full)
			if err != nil || info.IsDir() {
				continue
			}
			entries = append(entries, FileEntry{
				Path: line,
				Size: info.Size(),
				Lang: detectLang(line),
			})
		}
		return entries, nil
	}
	// Fallback: walkdir, skip common dirs.
	skip := map[string]bool{"node_modules": true, ".git": true, "vendor": true, "dist": true, "build": true, "__pycache__": true}
	var entries []FileEntry
	err := filepath.WalkDir(repo, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(repo, path)
		info, _ := d.Info()
		entries = append(entries, FileEntry{Path: rel, Size: info.Size(), Lang: detectLang(rel)})
		return nil
	})
	return entries, err
}

func detectLang(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".rs":
		return "rust"
	case ".sh":
		return "shell"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yml", ".yaml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".sql":
		return "sql"
	case ".dockerfile":
		return "dockerfile"
	}
	if strings.HasPrefix(strings.ToLower(filepath.Base(p)), "dockerfile") {
		return "dockerfile"
	}
	return ""
}

// Commit is one row in recent.md.
type Commit struct {
	Hash    string
	Subject string
	When    string
	Author  string
}

func gitRecent(repo string, n int) ([]Commit, string) {
	out, err := exec.Command("git", "-C", repo, "log", fmt.Sprintf("-%d", n),
		"--pretty=format:%h%x09%an%x09%cr%x09%s").Output()
	if err != nil {
		return nil, ""
	}
	headOut, _ := exec.Command("git", "-C", repo, "rev-parse", "--short", "HEAD").Output()
	head := strings.TrimSpace(string(headOut))
	var commits []Commit
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		commits = append(commits, Commit{Hash: parts[0], Author: parts[1], When: parts[2], Subject: parts[3]})
	}
	return commits, head
}

func writeRecentMD(path string, commits []Commit) error {
	var sb strings.Builder
	sb.WriteString("# recent\n\n")
	if len(commits) == 0 {
		sb.WriteString("(no git history)\n")
	} else {
		sb.WriteString("| hash | when | author | subject |\n|---|---|---|---|\n")
		for _, c := range commits {
			subj := strings.ReplaceAll(c.Subject, "|", "\\|")
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", c.Hash, c.When, c.Author, subj))
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

func writeOverviewMD(path, repo string, files []FileEntry, head string) error {
	var sb strings.Builder
	sb.WriteString("# overview — " + filepath.Base(repo) + "\n\n")
	sb.WriteString("> Generated by yashigatakae graphify v0.4.0-rc1 — " + time.Now().UTC().Format(time.RFC3339) + "\n")
	if head != "" {
		sb.WriteString("> HEAD = `" + head + "`\n")
	}
	sb.WriteString("\n## from README\n\n")
	if readme := findReadme(repo); readme != "" {
		raw, _ := os.ReadFile(readme)
		// Take first ~40 lines.
		lines := strings.SplitN(string(raw), "\n", 41)
		if len(lines) > 40 {
			lines = lines[:40]
			lines = append(lines, "*(…truncated; see "+filepath.Base(readme)+")*")
		}
		sb.WriteString(strings.Join(lines, "\n"))
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("(no README found)\n\n")
	}
	sb.WriteString("## structure\n\n")
	sb.WriteString(topLevelTree(repo, files))
	sb.WriteString("\n## languages\n\n")
	sb.WriteString(languageBreakdown(files))
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

func writeIndexMD(path, repo string, files []FileEntry, head string) error {
	var sb strings.Builder
	sb.WriteString("# index — " + filepath.Base(repo) + "\n\n")
	if head != "" {
		sb.WriteString("HEAD = `" + head + "`  \n")
	}
	sb.WriteString("Generated " + time.Now().UTC().Format(time.RFC3339) + "  \n\n")
	sb.WriteString("## artifacts\n\n")
	sb.WriteString("- [overview.md](overview.md) — README excerpt + structure + languages\n")
	sb.WriteString("- [recent.md](recent.md) — last 30 commits\n")
	sb.WriteString("- [files.json](files.json) — structured inventory (paths, sizes, languages)\n")
	sb.WriteString("\n## stats\n\n")
	sb.WriteString(fmt.Sprintf("- file count: %d\n", len(files)))
	var bytes int64
	for _, f := range files {
		bytes += f.Size
	}
	sb.WriteString(fmt.Sprintf("- total bytes: %d (%.2f MB)\n", bytes, float64(bytes)/1024/1024))
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

func findReadme(repo string) string {
	for _, name := range []string{"README.md", "README.MD", "README.rst", "README", "Readme.md", "readme.md"} {
		p := filepath.Join(repo, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func topLevelTree(repo string, files []FileEntry) string {
	dirCounts := map[string]int{}
	dirBytes := map[string]int64{}
	rootFiles := []FileEntry{}
	for _, f := range files {
		if !strings.Contains(f.Path, string(os.PathSeparator)) && !strings.Contains(f.Path, "/") {
			rootFiles = append(rootFiles, f)
			continue
		}
		dir := f.Path
		if i := strings.IndexAny(dir, "/\\"); i >= 0 {
			dir = dir[:i]
		}
		dirCounts[dir]++
		dirBytes[dir] += f.Size
	}
	var sb strings.Builder
	dirs := make([]string, 0, len(dirCounts))
	for d := range dirCounts {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		sb.WriteString(fmt.Sprintf("- `%s/` — %d files, %s\n", d, dirCounts[d], humanBytes(dirBytes[d])))
	}
	if len(rootFiles) > 0 {
		sb.WriteString("\n_root-level files: ")
		names := make([]string, 0, len(rootFiles))
		for _, f := range rootFiles {
			names = append(names, "`"+f.Path+"`")
		}
		sort.Strings(names)
		sb.WriteString(strings.Join(names, ", "))
		sb.WriteString("_\n")
	}
	return sb.String()
}

func languageBreakdown(files []FileEntry) string {
	count := map[string]int{}
	bytes := map[string]int64{}
	for _, f := range files {
		lang := f.Lang
		if lang == "" {
			lang = "other"
		}
		count[lang]++
		bytes[lang] += f.Size
	}
	type row struct {
		Lang  string
		Count int
		Bytes int64
	}
	var rows []row
	for k, v := range count {
		rows = append(rows, row{Lang: k, Count: v, Bytes: bytes[k]})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Bytes > rows[j].Bytes })
	var sb strings.Builder
	sb.WriteString("| language | files | bytes |\n|---|---:|---:|\n")
	for _, r := range rows {
		sb.WriteString(fmt.Sprintf("| %s | %d | %s |\n", r.Lang, r.Count, humanBytes(r.Bytes)))
	}
	return sb.String()
}

func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.2fMB", float64(n)/1024/1024)
	}
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
