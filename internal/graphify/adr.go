package graphify

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// ADR is one decision record extracted from git log. We treat any
// substantive commit (>20 lines body OR contains decision keywords) as a
// candidate; the heuristic is intentionally loose because false positives
// are far cheaper than false negatives in a wiki.
type ADR struct {
	Hash    string
	Author  string
	Date    string
	Subject string
	Body    string
	Reason  string // first sentence after a "because"/"why"/"reason" keyword
}

var reReason = regexp.MustCompile(`(?i)(?:because|reason|why|so that|in order to)\b[:\-]?\s*(.+)`)

// ExtractADRs walks `git log` and returns commits that look like decisions.
// `repo` is the on-disk path. `limit` caps how many commits we consider.
func ExtractADRs(repo string, limit int) []ADR {
	if limit <= 0 {
		limit = 1000
	}
	out, err := exec.Command("git", "-C", repo, "log",
		fmt.Sprintf("-%d", limit),
		"--pretty=format:%H%x09%an%x09%cI%x09%s%x1F%b%x1E").Output()
	if err != nil {
		return nil
	}
	var adrs []ADR
	for _, raw := range strings.Split(string(out), "\x1E") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// header (4 fields tab-sep) <US> body
		parts := strings.SplitN(raw, "\x1F", 2)
		head := parts[0]
		body := ""
		if len(parts) == 2 {
			body = strings.TrimSpace(parts[1])
		}
		hf := strings.SplitN(head, "\t", 4)
		if len(hf) < 4 {
			continue
		}
		if !looksLikeDecision(hf[3], body) {
			continue
		}
		reason := ""
		if m := reReason.FindStringSubmatch(body); m != nil {
			r := m[1]
			if i := strings.IndexAny(r, ".\n"); i > 0 {
				r = r[:i]
			}
			reason = strings.TrimSpace(r)
		}
		adrs = append(adrs, ADR{
			Hash:    hf[0][:8],
			Author:  hf[1],
			Date:    hf[2][:10],
			Subject: hf[3],
			Body:    body,
			Reason:  reason,
		})
	}
	return adrs
}

var decisionKeywords = []string{
	"decided", "decision", "rationale", "reason", "because",
	"chose", "going with", "switching to", "adopt", "adopted",
	"deprecated", "remove", "removed", "introduce", "introduced",
	"refactor", "rewrite", "redesign", "migrate",
}

func looksLikeDecision(subject, body string) bool {
	combined := strings.ToLower(subject + "\n" + body)
	if len(body) > 400 { // long body ≈ deliberate explanation
		return true
	}
	for _, kw := range decisionKeywords {
		if strings.Contains(combined, kw) {
			return true
		}
	}
	return false
}
