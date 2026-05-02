package mempalace

import (
	"regexp"
	"strings"
)

// Category is the small enum we annotate every entry with. Heuristic-driven
// (no LLM dep). The categories cover the bulk of what shows up in practice;
// anything that doesn't match falls back to "misc".
//
// Plan called for an LLM-tagged variant; that lands as a future toggle. The
// heuristic tier is good enough that recall --category fact filters cleanly.
type Category string

const (
	CatUserPref    Category = "user_pref"
	CatObservation Category = "observation"
	CatFact        Category = "fact"
	CatDecision    Category = "decision"
	CatError       Category = "error"
	CatCodeSnippet Category = "code_snippet"
	CatURL         Category = "url"
	CatLesson      Category = "lesson"
	CatMisc        Category = "misc"
)

var (
	reURL          = regexp.MustCompile(`(?i)\bhttps?://\S+`)
	reCodeFence    = regexp.MustCompile("(?m)^```")
	reShellPrompt  = regexp.MustCompile(`(?m)^\s*(\$|>>>|#)\s+\S`)
	reErrorWord    = regexp.MustCompile(`(?i)\b(error|exception|panic|stack ?trace|traceback|failed)\b`)
	reDecisionWord = regexp.MustCompile(`(?i)\b(decided|will use|going with|chose|picked|switched to)\b`)
	rePrefWord     = regexp.MustCompile(`(?i)\b(prefer|always|never|i (use|hate|like|want))\b`)
	reLessonTag    = regexp.MustCompile(`(?i)\blesson\b`)
)

// Categorize returns the best-fit Category for the entry. Order matters —
// earlier checks win, so URL-only entries beat the lesson tag etc.
func Categorize(body string, tags []string) Category {
	if body == "" {
		return CatMisc
	}

	// Tags trump body heuristics — caller knows best.
	for _, t := range tags {
		switch strings.ToLower(t) {
		case "lesson":
			return CatLesson
		case "decision", "adr":
			return CatDecision
		case "fact":
			return CatFact
		case "user_pref", "preference", "userpref":
			return CatUserPref
		}
	}

	body = strings.TrimSpace(body)

	// Pure URL line — common from sweep hooks.
	if isMostlyURL(body) {
		return CatURL
	}
	if reCodeFence.MatchString(body) || reShellPrompt.MatchString(body) {
		return CatCodeSnippet
	}
	if reErrorWord.MatchString(body) {
		return CatError
	}
	if reDecisionWord.MatchString(body) {
		return CatDecision
	}
	if rePrefWord.MatchString(body) {
		return CatUserPref
	}
	if reLessonTag.MatchString(body) {
		return CatLesson
	}
	// Short factual statement (one sentence, no opinion words)
	if len(body) < 280 && !strings.ContainsAny(body, "?!") {
		return CatFact
	}
	return CatObservation
}

func isMostlyURL(s string) bool {
	loc := reURL.FindStringIndex(s)
	if loc == nil {
		return false
	}
	urlLen := loc[1] - loc[0]
	return urlLen >= len(s)/2
}
