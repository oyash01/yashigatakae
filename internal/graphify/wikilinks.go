package graphify

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// WikiRegistry tracks every page that exists in a wiki run, plus every
// [[link]] that referenced something. After all generators have run we know
// which links resolved (real page) and which need a STUB-PAGES.md entry.
type WikiRegistry struct {
	mu       sync.Mutex
	pages    map[string]string   // page name -> relative path under wiki root
	backrefs map[string][]string // page name -> list of pages that link to it
	stubs    map[string]int      // unresolved link name -> count
}

func NewWikiRegistry() *WikiRegistry {
	return &WikiRegistry{
		pages:    map[string]string{},
		backrefs: map[string][]string{},
		stubs:    map[string]int{},
	}
}

// AddPage records that a page exists.
func (r *WikiRegistry) AddPage(name, relPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pages[name] = relPath
}

// PageExists returns true if `name` is a known page.
func (r *WikiRegistry) PageExists(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.pages[name]
	return ok
}

// LinkTo records that `from` referenced `to`. If `to` doesn't exist as a
// page, it counts as a stub.
func (r *WikiRegistry) LinkTo(from, to string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backrefs[to] = append(r.backrefs[to], from)
	if _, ok := r.pages[to]; !ok {
		r.stubs[to]++
	}
}

// Backrefs returns the list of pages linking TO `name`, deduped.
func (r *WikiRegistry) Backrefs(name string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	out := []string{}
	for _, b := range r.backrefs[name] {
		if !seen[b] {
			out = append(out, b)
			seen[b] = true
		}
	}
	sort.Strings(out)
	return out
}

// Stubs returns unresolved [[wikilinks]] sorted by frequency desc.
func (r *WikiRegistry) Stubs() []StubEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]StubEntry, 0, len(r.stubs))
	for name, count := range r.stubs {
		out = append(out, StubEntry{Name: name, RefCount: count, Backrefs: r.backrefs[name]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RefCount != out[j].RefCount {
			return out[i].RefCount > out[j].RefCount
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// AllPages returns name -> path.
func (r *WikiRegistry) AllPages() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.pages))
	for k, v := range r.pages {
		out[k] = v
	}
	return out
}

// StubEntry is one row in STUB-PAGES.md.
type StubEntry struct {
	Name     string
	RefCount int
	Backrefs []string
}

var wikilinkRE = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// RenderLinks scans `body` for [[Foo]] tokens and rewrites them to either
// a real markdown link if the page exists, or a stub-bound link otherwise.
// Side effect: registers each reference in the registry so STUB-PAGES.md /
// what-links-here.md can be built later.
//
// `from` is the page name doing the linking (used for backrefs).
// `relPrefix` is the prefix needed to walk back to the wiki root from `from`
// (e.g. "../" for pages under modules/).
func (r *WikiRegistry) RenderLinks(body, from, relPrefix string) string {
	return wikilinkRE.ReplaceAllStringFunc(body, func(match string) string {
		name := match[2 : len(match)-2]
		display := name
		// [[Page|alias]] support
		if i := strings.Index(name, "|"); i >= 0 {
			display = name[i+1:]
			name = name[:i]
		}
		r.LinkTo(from, name)
		if path, ok := r.pages[name]; ok {
			return fmt.Sprintf("[%s](%s%s)", display, relPrefix, path)
		}
		return fmt.Sprintf("[%s](%sSTUB-PAGES.md#%s)", display, relPrefix, anchorize(name))
	})
}

// anchorize converts "Foo Bar" to "foo-bar" for markdown anchors.
func anchorize(s string) string {
	out := strings.Builder{}
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			out.WriteRune('-')
		}
	}
	return out.String()
}
