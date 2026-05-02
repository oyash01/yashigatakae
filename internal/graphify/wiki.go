// Package graphify wiki.go: the Karpathy LLM Wiki generator. Produces a
// cross-linked markdown corpus under <wikiDir>/ that Claude reads instead
// of re-greping the source on every session start.
//
// Pipeline:
//   1. inventory + symbols + deps + ADRs + glossary (parallel-safe)
//   2. register every page name in WikiRegistry first (so wikilinks resolve)
//   3. render index/architecture/modules/symbols/decisions/conventions/
//      dependencies/glossary pages, recording [[link]] references
//   4. STUB-PAGES.md from unresolved references; what-links-here.md from
//      registry backrefs; _meta/citations.json for provenance
//
// Every page is plain markdown so it's queryable by Claude AND editable by
// humans. Wikilinks use [[Foo]] syntax and rewrite to relative markdown
// links during rendering.
package graphify

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// GenerateWiki is the v0.14 entry point. Run() dispatches here when
// opts.Pro is true.
func GenerateWiki(opts Options) (Result, error) {
	abs, err := filepath.Abs(opts.Repo)
	if err != nil {
		return Result{}, err
	}
	wikiDir := opts.OutDir
	if wikiDir == "" {
		yashRoot, err := defaultWikiRoot()
		if err != nil {
			return Result{}, err
		}
		wikiDir = filepath.Join(yashRoot, filepath.Base(abs))
	}
	if err := os.MkdirAll(filepath.Join(wikiDir, "modules"), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Join(wikiDir, "symbols"), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Join(wikiDir, "_meta"), 0o755); err != nil {
		return Result{}, err
	}

	files, err := inventory(abs)
	if err != nil {
		return Result{}, err
	}
	symbols := ExtractSymbols(abs, files)
	deps := ExtractDependencies(abs)
	adrs := ExtractADRs(abs, 500)
	glossary := BuildGlossary(abs, files, 3)
	commits, head := gitRecent(abs, 30)

	reg := NewWikiRegistry()

	// Register every page name FIRST so wikilinks resolve in any render order.
	reg.AddPage("index", "index.md")
	reg.AddPage("architecture", "architecture.md")
	reg.AddPage("MODULES", "MODULES.md")
	reg.AddPage("DECISIONS", "DECISIONS.md")
	reg.AddPage("CONVENTIONS", "CONVENTIONS.md")
	reg.AddPage("DEPENDENCIES", "DEPENDENCIES.md")
	reg.AddPage("GLOSSARY", "GLOSSARY.md")
	reg.AddPage("STUB-PAGES", "STUB-PAGES.md")
	reg.AddPage("RECENT-CHANGES", "RECENT-CHANGES.md")
	reg.AddPage("what-links-here", "what-links-here.md")

	byModule := GroupByModule(symbols)
	for moduleName := range byModule {
		safe := safeFilename(moduleName)
		reg.AddPage(moduleName, "modules/"+safe+".md")
	}
	symbolFiles := map[string]Symbol{} // page name -> symbol (deduped via SymbolFingerprint)
	seenNames := map[string]int{}
	for _, s := range symbols {
		page := SymbolFingerprint(s, seenNames)
		safe := safeFilename(page)
		reg.AddPage(page, "symbols/"+safe+".md")
		symbolFiles[page] = s
	}
	for _, g := range glossary {
		reg.AddPage(g.Term, "GLOSSARY.md#"+anchorize(g.Term))
	}

	// Now render. Each generator returns the markdown body; wikilinks are
	// resolved during write via RenderLinks.
	pages := map[string]struct {
		path    string
		body    string
		from    string
		prefix  string
	}{}

	pages["index"] = pageEntry{
		"index.md",
		renderIndex(filepath.Base(abs), head, files, symbols, deps, adrs, byModule),
		"index",
		"",
	}.toAnon()
	pages["architecture"] = pageEntry{
		"architecture.md",
		renderArchitecture(filepath.Base(abs), files, byModule),
		"architecture",
		"",
	}.toAnon()
	pages["MODULES"] = pageEntry{
		"MODULES.md",
		renderModulesIndex(byModule),
		"MODULES",
		"",
	}.toAnon()
	pages["DECISIONS"] = pageEntry{
		"DECISIONS.md",
		renderDecisions(adrs),
		"DECISIONS",
		"",
	}.toAnon()
	pages["CONVENTIONS"] = pageEntry{
		"CONVENTIONS.md",
		renderConventions(symbols, files),
		"CONVENTIONS",
		"",
	}.toAnon()
	pages["DEPENDENCIES"] = pageEntry{
		"DEPENDENCIES.md",
		renderDependencies(deps),
		"DEPENDENCIES",
		"",
	}.toAnon()
	pages["GLOSSARY"] = pageEntry{
		"GLOSSARY.md",
		renderGlossary(glossary),
		"GLOSSARY",
		"",
	}.toAnon()
	pages["RECENT-CHANGES"] = pageEntry{
		"RECENT-CHANGES.md",
		renderRecent(commits),
		"RECENT-CHANGES",
		"",
	}.toAnon()

	// Module pages
	moduleNames := make([]string, 0, len(byModule))
	for k := range byModule {
		moduleNames = append(moduleNames, k)
	}
	sort.Strings(moduleNames)
	for _, m := range moduleNames {
		safe := safeFilename(m)
		pages["module:"+m] = pageEntry{
			"modules/" + safe + ".md",
			renderModulePage(abs, m, byModule[m], files),
			m,
			"../",
		}.toAnon()
	}

	// Symbol pages
	for page, sym := range symbolFiles {
		safe := safeFilename(page)
		pages["symbol:"+page] = pageEntry{
			"symbols/" + safe + ".md",
			renderSymbolPage(abs, page, sym, symbols),
			page,
			"../",
		}.toAnon()
	}

	// First pass: render every page's wikilinks (this populates the
	// registry's stubs + backrefs as a side effect).
	written := map[string]string{}
	for _, p := range pages {
		body := reg.RenderLinks(p.body, p.from, p.prefix)
		written[p.path] = body
	}

	// Now we know which links resolved. Generate STUB + what-links-here.
	stubBody := reg.RenderLinks(renderStubs(reg.Stubs()), "STUB-PAGES", "")
	written["STUB-PAGES.md"] = stubBody

	whatBody := reg.RenderLinks(renderWhatLinksHere(reg), "what-links-here", "")
	written["what-links-here.md"] = whatBody

	// Write everything to disk + collect citations.
	citations := map[string][]string{}
	totalBytes := int64(0)
	fileCount := 0
	for rel, body := range written {
		dst := filepath.Join(wikiDir, rel)
		if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
			return Result{}, err
		}
		totalBytes += int64(len(body))
		fileCount++
	}
	for _, s := range symbols {
		page := SymbolFingerprint(s, map[string]int{}) // recompute (cheap)
		citations[page] = []string{fmt.Sprintf("%s:%d", s.File, s.Line)}
	}
	if err := writeJSON(filepath.Join(wikiDir, "_meta", "citations.json"), citations); err != nil {
		return Result{}, err
	}
	if err := writeJSON(filepath.Join(wikiDir, "_meta", "graphify.json"), map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"repo":         abs,
		"head":         head,
		"file_count":   len(files),
		"symbol_count": len(symbols),
		"module_count": len(byModule),
		"adr_count":    len(adrs),
		"dep_count":    len(deps),
		"glossary":     len(glossary),
	}); err != nil {
		return Result{}, err
	}

	return Result{
		WikiDir:   wikiDir,
		Files:     fileCount,
		Bytes:     totalBytes,
		GitCommit: head,
	}, nil
}

// CheckWiki returns the count of broken wikilinks. Exit-zero iff zero stubs.
// Used by `yashigatakae graphify check <repo>` so CI can gate on cleanliness.
func CheckWiki(opts Options) (int, error) {
	abs, _ := filepath.Abs(opts.Repo)
	yashRoot, err := defaultWikiRoot()
	if err != nil {
		return 0, err
	}
	wikiDir := opts.OutDir
	if wikiDir == "" {
		wikiDir = filepath.Join(yashRoot, filepath.Base(abs))
	}
	stubPath := filepath.Join(wikiDir, "STUB-PAGES.md")
	raw, err := os.ReadFile(stubPath)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", stubPath, err)
	}
	// Count "- [[name]]" lines (each is one stub).
	count := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") &&
			strings.Contains(line, "—") &&
			!strings.HasPrefix(strings.TrimSpace(line), "- (") {
			count++
		}
	}
	return count, nil
}

func defaultWikiRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".yashigatakae", "state", "codebase-wiki"), nil
}

func safeFilename(s string) string {
	out := strings.Builder{}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '-', r == '_':
			out.WriteRune(r)
		case r == '/', r == '\\', r == '.', r == ' ':
			out.WriteRune('_')
		}
	}
	if out.Len() == 0 {
		return "page"
	}
	return out.String()
}

type pageEntry struct {
	path   string
	body   string
	from   string
	prefix string
}

func (p pageEntry) toAnon() struct {
	path   string
	body   string
	from   string
	prefix string
} {
	return struct {
		path   string
		body   string
		from   string
		prefix string
	}{p.path, p.body, p.from, p.prefix}
}
