package mempalace

import (
	"context"
	"strings"
)

// DedupeResult is what FindDuplicate returns.
type DedupeResult struct {
	Found       bool
	ExistingID  int64
	Similarity  float32
}

// FindDuplicate looks for a near-duplicate of `body` in the same project.
// Strategy:
//  1. If queryVec is non-empty AND existing entries have embeddings, cosine.
//     A match >= cosineThresh is a hit.
//  2. Else fall back to a normalized-string equality check on the most-recent
//     `windowSize` entries (cheap; catches "same exact note twice").
//
// The window keeps the cost bounded: brute-force over 200 rows is microseconds
// even with 1024-dim vectors. Larger corpora can re-run consolidate first.
func FindDuplicate(ctx context.Context, store *Store, project, body string, queryVec []float32, windowSize int, cosineThresh float32) (DedupeResult, error) {
	if windowSize <= 0 {
		windowSize = 200
	}
	if cosineThresh <= 0 {
		cosineThresh = 0.95
	}
	hasVec := len(queryVec) > 0
	normBody := normalizeForEq(body)

	var best DedupeResult
	count := 0
	err := store.AllEntriesFiltered(ctx, EntryFilter{Project: project}, func(e Entry) error {
		if count >= windowSize {
			return errStopIter
		}
		count++
		// Vector path
		if hasVec && len(e.Embedding) == len(queryVec) {
			score := cosine(queryVec, e.Embedding)
			if score >= cosineThresh && score > best.Similarity {
				best = DedupeResult{Found: true, ExistingID: e.ID, Similarity: score}
			}
			return nil
		}
		// String path
		if normalizeForEq(e.Body) == normBody {
			best = DedupeResult{Found: true, ExistingID: e.ID, Similarity: 1.0}
		}
		return nil
	})
	if err != nil && err != errStopIter {
		return DedupeResult{}, err
	}
	return best, nil
}

// errStopIter is a sentinel returned by AllEntriesFiltered callbacks that
// have collected enough rows.
var errStopIter = stopIterError{}

type stopIterError struct{}

func (stopIterError) Error() string { return "stop iteration" }

// normalizeForEq lower-cases, collapses whitespace, trims punctuation tails.
// Catches "GhostNode pinned sing-box 1.8.10" vs "ghostnode pinned sing-box 1.8.10".
func normalizeForEq(s string) string {
	s = strings.ToLower(s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, ".!? \t\n")
	return s
}
