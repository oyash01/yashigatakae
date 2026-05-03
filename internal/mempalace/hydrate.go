package mempalace

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	timeNow = time.Now
	mathExp = math.Exp
)

// HydrateOptions tunes what Hydrate pulls from the store.
type HydrateOptions struct {
	CWD        string // defaults to os.Getwd()
	TopK       int    // default 5
	HalfLife   float64 // days; default 30
	IncludeRecent bool // also pull the most recent N entries (any project)
	RecentN    int    // default 3
}

// HydrateResult is what the SessionStart hook prints back to Claude Code.
type HydrateResult struct {
	Project   string
	Hits      []Hit
	Recent    []Hit
}

// Hydrate reads cwd, derives project = filepath.Base(cwd), then runs hybrid
// recall scoped to that project. Used by the SessionStart hook so a fresh
// Claude Code session opens with the most relevant prior memories already
// in context — no manual /recall needed.
//
// On a brand-new machine where the local mempalace.db is empty, this
// silently returns no hits and the hook prints a one-line "(empty)" notice.
func Hydrate(ctx context.Context, opts HydrateOptions) (HydrateResult, error) {
	if opts.CWD == "" {
		c, _ := os.Getwd()
		opts.CWD = c
	}
	if opts.TopK <= 0 {
		opts.TopK = 5
	}
	if opts.RecentN <= 0 {
		opts.RecentN = 3
	}
	if opts.HalfLife == 0 {
		opts.HalfLife = 30
	}

	res := HydrateResult{Project: filepath.Base(opts.CWD)}

	// Project-scoped hybrid recall using a query derived from the cwd's
	// basename + a bag of file/folder names. Biases the ranker toward
	// entries that talk about THIS repo's files.
	query := strings.Join(seedQueryTerms(opts.CWD), " ")
	if query == "" {
		query = res.Project
	}
	hits, err := RecallHybrid(ctx, HybridOptions{
		TopK:         opts.TopK,
		Project:      res.Project,
		HalfLifeDays: opts.HalfLife,
	}, query)
	if err == nil {
		res.Hits = hits
	}

	// Fallback: if the query-driven recall under-fills the top slot, top up
	// with the most recent project entries (decay-weighted). Without this,
	// project=foo entries that don't mention "foo" verbatim get shut out.
	if len(res.Hits) < opts.TopK {
		seen := map[int64]bool{}
		for _, h := range res.Hits {
			seen[h.ID] = true
		}
		extra, _ := projectByRecency(ctx, res.Project, opts.TopK-len(res.Hits), opts.HalfLife)
		for _, h := range extra {
			if seen[h.ID] {
				continue
			}
			res.Hits = append(res.Hits, h)
			if len(res.Hits) >= opts.TopK {
				break
			}
		}
	}

	// Recent: top RecentN regardless of project, time-decayed sharply so
	// "what was I working on yesterday" surfaces.
	if opts.IncludeRecent {
		recent, err := RecallHybrid(ctx, HybridOptions{
			TopK:         opts.RecentN,
			HalfLifeDays: 3,
		}, res.Project)
		if err == nil {
			res.Recent = recent
		}
	}

	return res, nil
}

// projectByRecency lists every entry in a project, then ranks by
// time-decay (no query needed). Used as the hydrate fallback when the
// query-driven hybrid recall under-fills.
func projectByRecency(ctx context.Context, project string, topK int, halfLife float64) ([]Hit, error) {
	if topK <= 0 {
		return nil, nil
	}
	store, err := Open()
	if err != nil {
		return nil, err
	}
	defer store.Close()

	var entries []Entry
	if err := store.AllEntriesFiltered(ctx, EntryFilter{Project: project}, func(e Entry) error {
		entries = append(entries, e)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	now := timeNow()
	type scored struct {
		entry Entry
		score float64
	}
	out := make([]scored, 0, len(entries))
	for _, e := range entries {
		s := 1.0
		if halfLife > 0 && !e.Timestamp.IsZero() {
			ageDays := now.Sub(e.Timestamp).Hours() / 24.0
			s = mathExp(-ageDays / halfLife)
		}
		out = append(out, scored{e, s})
	}
	// Bubble sort by score desc — n is tiny.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].score > out[i].score {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > topK {
		out = out[:topK]
	}
	hits := make([]Hit, 0, len(out))
	for _, s := range out {
		hits = append(hits, Hit{Entry: s.entry, Score: float32(s.score)})
	}
	return hits, nil
}

// seedQueryTerms grabs cwd basename + up to 10 top-level entries inside cwd
// (filenames + dir names). Cheap signal of "what is this repo about" that
// the BM25 side of the ranker can use even for queries with no embedding.
func seedQueryTerms(cwd string) []string {
	out := []string{filepath.Base(cwd)}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return out
	}
	for i, e := range entries {
		if i >= 10 {
			break
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// FormatForSessionStart renders a HydrateResult as the markdown blob the
// SessionStart hook writes to stdout. Claude Code surfaces this as hidden
// session context.
func (r HydrateResult) FormatForSessionStart() string {
	var b strings.Builder
	if len(r.Hits) == 0 && len(r.Recent) == 0 {
		fmt.Fprintf(&b, "## hydrate (project=%s)\n\n(no relevant memories — this is a fresh project or the local mempalace is empty)\n",
			r.Project)
		return b.String()
	}
	fmt.Fprintf(&b, "## hydrate (project=%s)\n\n", r.Project)
	fmt.Fprintf(&b, "Top %d entries from mempalace ranked by relevance + recency. Use these as background for this session — they're the same memories you'd get from /recall.\n\n",
		len(r.Hits))
	for i, h := range r.Hits {
		body := strings.TrimSpace(h.Body)
		if len(body) > 400 {
			body = body[:400] + "…"
		}
		cat := h.Category
		if cat == "" {
			cat = "-"
		}
		fmt.Fprintf(&b, "### %d. [%s] #%d (score=%.3f)\n%s\n\n", i+1, cat, h.ID, h.Score, body)
	}
	if len(r.Recent) > 0 {
		fmt.Fprintf(&b, "## recent activity (any project)\n\n")
		for _, h := range r.Recent {
			body := strings.TrimSpace(h.Body)
			if len(body) > 200 {
				body = body[:200] + "…"
			}
			fmt.Fprintf(&b, "- [%s/%s] %s\n", h.Project, h.Category, body)
		}
	}
	return b.String()
}
