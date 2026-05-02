package mempalace

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"
)

// HybridOptions controls Recall when --mode hybrid is selected.
type HybridOptions struct {
	TopK         int
	Project      string
	Category     string
	HalfLifeDays float64 // <=0 disables time-decay
	RRFK         int     // reciprocal-rank-fusion smoothing constant; default 60
	BM25K1       float64 // term-frequency saturation; default 1.5
	BM25B        float64 // length normalization; default 0.75
}

// RecallHybrid runs cosine + BM25 over the (project-filtered) corpus,
// fuses ranks via Reciprocal Rank Fusion, then applies time-decay if
// HalfLifeDays > 0.
//
// On a 50k-row store this still scans every row twice. Acceptable — typical
// caller is a slash command running once per question, not a hot path.
func RecallHybrid(ctx context.Context, opts HybridOptions, query string) ([]Hit, error) {
	if opts.TopK <= 0 {
		opts.TopK = 10
	}
	if opts.RRFK <= 0 {
		opts.RRFK = 60
	}
	if opts.BM25K1 <= 0 {
		opts.BM25K1 = 1.5
	}
	if opts.BM25B <= 0 {
		opts.BM25B = 0.75
	}

	store, err := Open()
	if err != nil {
		return nil, err
	}
	defer store.Close()

	emb := AutoEmbedder()
	var qvec []float32
	if _, isNoop := emb.(noEmbedder); !isNoop {
		if v, err := emb.Embed(ctx, query); err == nil {
			qvec = v
		}
	}

	// 1. Materialize the working set (project + category filter).
	var entries []Entry
	if err := store.AllEntriesFiltered(ctx, EntryFilter{Project: opts.Project, Category: opts.Category}, func(e Entry) error {
		entries = append(entries, e)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	// 2. Cosine ranks (only for entries with compatible embedding).
	cosRank := map[int64]int{}
	if len(qvec) > 0 {
		type cs struct {
			id    int64
			score float32
		}
		var arr []cs
		for _, e := range entries {
			if len(e.Embedding) != len(qvec) {
				continue
			}
			arr = append(arr, cs{id: e.ID, score: cosine(qvec, e.Embedding)})
		}
		sort.Slice(arr, func(i, j int) bool { return arr[i].score > arr[j].score })
		for i, c := range arr {
			cosRank[c.id] = i + 1
		}
	}

	// 3. BM25 ranks.
	bm := buildBM25(entries, opts.BM25K1, opts.BM25B)
	bmRank := map[int64]int{}
	type br struct {
		id    int64
		score float64
	}
	var bmArr []br
	for _, e := range entries {
		s := bm.score(e, query)
		if s > 0 {
			bmArr = append(bmArr, br{id: e.ID, score: s})
		}
	}
	sort.Slice(bmArr, func(i, j int) bool { return bmArr[i].score > bmArr[j].score })
	for i, b := range bmArr {
		bmRank[b.id] = i + 1
	}

	// 4. Reciprocal Rank Fusion.
	now := time.Now()
	type fused struct {
		entry Entry
		score float64
	}
	var out []fused
	for _, e := range entries {
		var s float64
		if r, ok := cosRank[e.ID]; ok {
			s += 1.0 / float64(opts.RRFK+r)
		}
		if r, ok := bmRank[e.ID]; ok {
			s += 1.0 / float64(opts.RRFK+r)
		}
		if s == 0 {
			continue
		}
		if opts.HalfLifeDays > 0 && !e.Timestamp.IsZero() {
			ageDays := now.Sub(e.Timestamp).Hours() / 24.0
			s *= math.Exp(-ageDays / opts.HalfLifeDays)
		}
		out = append(out, fused{entry: e, score: s})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].score > out[j].score })
	if len(out) > opts.TopK {
		out = out[:opts.TopK]
	}
	hits := make([]Hit, 0, len(out))
	for _, f := range out {
		hits = append(hits, Hit{Entry: f.entry, Score: float32(f.score)})
	}
	return hits, nil
}

// --- BM25 ---

type bm25Index struct {
	k1, b   float64
	avgLen  float64
	docFreq map[string]int
	docs    []bm25Doc
	N       int
}

type bm25Doc struct {
	id      int64
	tokens  []string
	tf      map[string]int
	length  int
}

func buildBM25(entries []Entry, k1, b float64) *bm25Index {
	idx := &bm25Index{k1: k1, b: b, docFreq: map[string]int{}, N: len(entries)}
	var totalLen int
	for _, e := range entries {
		toks := tokenize(e.Body)
		tf := map[string]int{}
		for _, t := range toks {
			tf[t]++
		}
		for t := range tf {
			idx.docFreq[t]++
		}
		idx.docs = append(idx.docs, bm25Doc{id: e.ID, tokens: toks, tf: tf, length: len(toks)})
		totalLen += len(toks)
	}
	if len(idx.docs) > 0 {
		idx.avgLen = float64(totalLen) / float64(len(idx.docs))
	}
	return idx
}

func (idx *bm25Index) score(e Entry, query string) float64 {
	qtoks := tokenize(query)
	if len(qtoks) == 0 || idx.avgLen == 0 {
		return 0
	}
	var doc *bm25Doc
	for i := range idx.docs {
		if idx.docs[i].id == e.ID {
			doc = &idx.docs[i]
			break
		}
	}
	if doc == nil {
		return 0
	}
	var s float64
	dl := float64(doc.length)
	for _, q := range qtoks {
		df := idx.docFreq[q]
		if df == 0 {
			continue
		}
		idf := math.Log(1 + (float64(idx.N)-float64(df)+0.5)/(float64(df)+0.5))
		f := float64(doc.tf[q])
		num := f * (idx.k1 + 1)
		den := f + idx.k1*(1-idx.b+idx.b*dl/idx.avgLen)
		s += idf * (num / den)
	}
	return s
}

// helper used by categorize.go and dedupe.go
func nonEmptyLower(parts []string) []string {
	out := []string{}
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
