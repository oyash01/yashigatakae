package graphify

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// GlossaryEntry is one row in GLOSSARY.md.
type GlossaryEntry struct {
	Term  string
	Count int
	Where []string // up to 3 file paths the term appears in
}

var (
	reProperNoun  = regexp.MustCompile(`\b([A-Z][A-Za-z0-9]{2,})\b`)
	stopWords     = newStopwordSet()
	docFileExts   = map[string]bool{".md": true, ".rst": true, ".txt": true, ".adoc": true}
)

// BuildGlossary scans every README/markdown/text file plus inline doc
// comments in source files, counts proper nouns appearing > minCount times
// across the corpus, and returns the entries sorted by frequency desc.
func BuildGlossary(repo string, files []FileEntry, minCount int) []GlossaryEntry {
	if minCount <= 0 {
		minCount = 3
	}
	count := map[string]int{}
	where := map[string][]string{}

	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Path))
		if !docFileExts[ext] {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(repo, f.Path))
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, m := range reProperNoun.FindAllStringSubmatch(string(raw), -1) {
			term := m[1]
			if stopWords[term] {
				continue
			}
			count[term]++
			if !seen[term] && len(where[term]) < 3 {
				where[term] = append(where[term], f.Path)
				seen[term] = true
			}
		}
	}

	out := []GlossaryEntry{}
	for term, c := range count {
		if c < minCount {
			continue
		}
		out = append(out, GlossaryEntry{Term: term, Count: c, Where: where[term]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Term < out[j].Term
	})
	return out
}

// newStopwordSet returns the set of common-English words and Markdown
// boilerplate we never want to glossary-ize. Built once at package init.
func newStopwordSet() map[string]bool {
	words := []string{
		"The", "This", "That", "These", "Those", "When", "Where", "Why", "How",
		"What", "Who", "Which", "It", "Its", "If", "In", "On", "Of", "Or", "And",
		"But", "Not", "Yes", "No", "True", "False", "Null", "None", "All", "Any",
		"Some", "Each", "Every", "Most", "More", "Less", "First", "Second", "Third",
		"Note", "Notes", "Example", "Examples", "Usage", "TODO", "FIXME",
		"Markdown", "Github", "GitHub",
	}
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}
	return out
}
