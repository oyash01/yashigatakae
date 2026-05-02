// Package mempalace is the lifetime semantic memory store: sqlite-backed,
// embedded via Voyage or OpenAI, brute-force cosine ranked.
//
// v0.2.0-rc1: local CLI only (remember / recall / forget / stats).
// v0.2.0-rc2 will expose the same surface as MCP tools over HTTP.
package mempalace

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Help is what the v0.1 stub command shows. Now updated for v0.2.
func Help() string {
	return `mempalace — lifetime semantic memory store backed by sqlite + cosine ranking.

  yashigatakae mempalace remember <text> [--project P] [--tags a,b,c] [--source S]
  yashigatakae mempalace recall   <query> [--top N] [--project P]
  yashigatakae mempalace forget   <id>
  yashigatakae mempalace stats

Embedding provider is auto-selected: VOYAGE_API_KEY (preferred) or OPENAI_API_KEY.
Without a key set, recall falls back to keyword matching.`
}

// RememberOptions captures all CLI flags for `mempalace remember`.
type RememberOptions struct {
	Body          string
	Source        string
	SourceMachine string
	Project       string
	Tags          string // comma-separated
	NoDedupe      bool   // skip the cosine/string dedupe pass
	NoCategorize  bool   // skip auto-category assignment
}

// Remember inserts a new entry. If an embedder is configured, it computes
// and stores the vector; otherwise the entry is keyword-only.
func Remember(ctx context.Context, opts RememberOptions) (int64, error) {
	if strings.TrimSpace(opts.Body) == "" {
		return 0, fmt.Errorf("body is empty")
	}
	store, err := Open()
	if err != nil {
		return 0, err
	}
	defer store.Close()

	source := opts.Source
	if source == "" {
		source = "cli"
	}

	emb := AutoEmbedder()
	var vec []float32
	if _, isNoop := emb.(noEmbedder); !isNoop {
		v, err := emb.Embed(ctx, opts.Body)
		if err != nil {
			fmt.Printf("  ! embedding failed (%v) — storing keyword-only entry\n", err)
		} else {
			vec = v
		}
	}

	tags := stringTags(opts.Tags)

	// Dedupe pass — fold near-duplicates into the existing row.
	if !opts.NoDedupe {
		dup, err := FindDuplicate(ctx, store, opts.Project, opts.Body, vec, 200, 0.95)
		if err == nil && dup.Found {
			if uerr := store.UpdateMergedRefresh(ctx, dup.ExistingID, tags); uerr == nil {
				return dup.ExistingID, nil
			}
		}
	}

	cat := ""
	if !opts.NoCategorize {
		cat = string(Categorize(opts.Body, tags))
	}

	id, err := store.Insert(ctx, Entry{
		Source:        source,
		SourceMachine: opts.SourceMachine,
		Project:       opts.Project,
		Tags:          tags,
		Body:          opts.Body,
		Embedding:     vec,
		Category:      cat,
	}, emb.Model())
	if err != nil {
		return 0, err
	}
	return id, nil
}

// RecallOptions captures all CLI flags for `mempalace recall`.
type RecallOptions struct {
	Query        string
	TopK         int
	Project      string
	Category     string
	HalfLifeDays float64 // <=0 disables time decay; default 30
	Mode         string  // "semantic" | "keyword" | "hybrid" (default hybrid)
}

// Recall returns up to TopK hits. Mode = "hybrid" (default) fuses cosine and
// BM25 via reciprocal rank fusion; "semantic" is cosine-only; "keyword" is
// BM25-only. Time-decay (HalfLifeDays > 0) re-weights by age.
func Recall(ctx context.Context, opts RecallOptions) ([]Hit, error) {
	if opts.TopK <= 0 {
		opts.TopK = 10
	}
	if strings.TrimSpace(opts.Query) == "" {
		return nil, fmt.Errorf("query is empty")
	}
	if opts.Mode == "" {
		opts.Mode = "hybrid"
	}
	if opts.HalfLifeDays == 0 {
		opts.HalfLifeDays = 30
	}
	if opts.Mode == "hybrid" {
		return RecallHybrid(ctx, HybridOptions{
			TopK:         opts.TopK,
			Project:      opts.Project,
			Category:     opts.Category,
			HalfLifeDays: opts.HalfLifeDays,
		}, opts.Query)
	}
	store, err := Open()
	if err != nil {
		return nil, err
	}
	defer store.Close()

	// Try semantic search first.
	emb := AutoEmbedder()
	var queryVec []float32
	if _, isNoop := emb.(noEmbedder); !isNoop {
		v, err := emb.Embed(ctx, opts.Query)
		if err == nil {
			queryVec = v
		}
	}

	var hits []Hit
	if len(queryVec) > 0 {
		err = store.AllEntries(ctx, opts.Project, func(e Entry) error {
			if len(e.Embedding) == 0 || len(e.Embedding) != len(queryVec) {
				return nil // skip entries without compatible embedding
			}
			score := cosine(queryVec, e.Embedding)
			hits = append(hits, Hit{Entry: e, Score: score})
			return nil
		})
	} else {
		// Keyword fallback: tokenize on non-letter chars, score by sum of
		// token hits (TF-style — no IDF, but good enough for small corpora).
		tokens := tokenize(opts.Query)
		if len(tokens) == 0 {
			return nil, nil
		}
		err = store.AllEntries(ctx, opts.Project, func(e Entry) error {
			body := strings.ToLower(e.Body)
			var matches int
			for _, t := range tokens {
				matches += strings.Count(body, t)
			}
			if matches == 0 {
				return nil
			}
			// Normalize by body length so shorter bodies rank higher per match.
			score := float32(matches) / float32(len(body)+1)
			hits = append(hits, Hit{Entry: e, Score: score})
			return nil
		})
	}
	if err != nil {
		return nil, err
	}

	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > opts.TopK {
		hits = hits[:opts.TopK]
	}
	return hits, nil
}

// Forget deletes an entry by ID.
func Forget(ctx context.Context, id int64) (bool, error) {
	store, err := Open()
	if err != nil {
		return false, err
	}
	defer store.Close()
	n, err := store.Delete(ctx, id)
	return n > 0, err
}

// tokenize lowers the query and splits on non-letter/digit characters,
// dropping single-char tokens (too noisy).
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var out []string
	var buf strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			buf.WriteRune(r)
			continue
		}
		if buf.Len() > 1 {
			out = append(out, buf.String())
		}
		buf.Reset()
	}
	if buf.Len() > 1 {
		out = append(out, buf.String())
	}
	return out
}

// Stats returns aggregate counts.
func Stats(ctx context.Context) (StatsResult, error) {
	store, err := Open()
	if err != nil {
		return StatsResult{}, err
	}
	defer store.Close()
	return store.Stats(ctx)
}
