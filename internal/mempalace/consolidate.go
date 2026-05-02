package mempalace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// ConsolidateOptions controls the rollup pass.
type ConsolidateOptions struct {
	Project   string
	Window    time.Duration // rollup the OLDEST entries in this window
	BatchSize int           // entries per summary (default 50)
	DryRun    bool
	Archive   bool // when true, originals removed and saved to ~/.yashigatakae/mempalace-archive/<ts>.jsonl
}

// ConsolidateResult is the human-readable outcome.
type ConsolidateResult struct {
	Inspected    int
	Summaries    int
	Archived     int
	ArchivePath  string
	NewEntryIDs  []int64
}

// Consolidate rolls up old entries into one summary entry per BatchSize.
// Heuristic summary (no LLM dep): a bullet list of the first 80 chars of
// each merged entry, sorted by ts asc, capped at BatchSize. The summary
// gets category="observation", tags=["consolidated", project], a synthetic
// body header, and is inserted as a fresh entry; the originals are
// archived (and removed from active set) when --archive is set.
func Consolidate(ctx context.Context, opts ConsolidateOptions) (ConsolidateResult, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 50
	}
	if opts.Window <= 0 {
		opts.Window = 30 * 24 * time.Hour
	}
	res := ConsolidateResult{}

	store, err := Open()
	if err != nil {
		return res, err
	}
	defer store.Close()

	cutoff := time.Now().Add(-opts.Window).UTC()
	var pool []Entry
	if err := store.AllEntriesFiltered(ctx, EntryFilter{Project: opts.Project}, func(e Entry) error {
		if e.Timestamp.Before(cutoff) {
			pool = append(pool, e)
		}
		return nil
	}); err != nil {
		return res, err
	}
	res.Inspected = len(pool)
	if len(pool) < opts.BatchSize {
		return res, nil
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].Timestamp.Before(pool[j].Timestamp) })

	// Process oldest BatchSize entries (single batch — caller can re-run).
	batch := pool[:opts.BatchSize]
	summary := buildSummary(opts.Project, batch)
	if opts.DryRun {
		fmt.Println("--- consolidation preview ---")
		fmt.Println(summary)
		return res, nil
	}

	// Insert summary as a fresh entry.
	id, err := store.Insert(ctx, Entry{
		Source:   "mempalace",
		Project:  opts.Project,
		Tags:     []string{"consolidated", "summary"},
		Body:     summary,
		Category: string(CatObservation),
	}, "")
	if err != nil {
		return res, err
	}
	res.Summaries = 1
	res.NewEntryIDs = []int64{id}

	if opts.Archive {
		yash, _ := osdetect.YashigatakaeDir()
		archDir := filepath.Join(yash, "mempalace-archive")
		_ = os.MkdirAll(archDir, 0o755)
		path := filepath.Join(archDir, time.Now().UTC().Format("20060102T150405")+"_"+
			sanitize(opts.Project)+".jsonl")
		f, err := os.Create(path)
		if err != nil {
			return res, err
		}
		enc := json.NewEncoder(f)
		archived := 0
		for _, e := range batch {
			if err := enc.Encode(e); err != nil {
				f.Close()
				return res, err
			}
			if _, err := store.Delete(ctx, e.ID); err != nil {
				f.Close()
				return res, err
			}
			archived++
		}
		f.Close()
		res.Archived = archived
		res.ArchivePath = path
	}
	return res, nil
}

func buildSummary(project string, batch []Entry) string {
	var b strings.Builder
	from := batch[0].Timestamp.Format("2006-01-02")
	to := batch[len(batch)-1].Timestamp.Format("2006-01-02")
	fmt.Fprintf(&b, "## consolidated %s — %s..%s — %d entries\n\n", project, from, to, len(batch))
	for _, e := range batch {
		head := strings.TrimSpace(e.Body)
		if i := strings.Index(head, "\n"); i > 0 {
			head = head[:i]
		}
		if len(head) > 120 {
			head = head[:120] + "…"
		}
		ts := e.Timestamp.Format("01-02")
		cat := e.Category
		if cat == "" {
			cat = "misc"
		}
		fmt.Fprintf(&b, "- [%s][%s] %s\n", ts, cat, head)
	}
	return b.String()
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	out := strings.Builder{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		} else if r == '_' || r == ' ' {
			out.WriteRune('-')
		}
	}
	if out.Len() == 0 {
		return "all"
	}
	return out.String()
}
