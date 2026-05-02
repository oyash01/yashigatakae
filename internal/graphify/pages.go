package graphify

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// renderIndex produces the wiki hub. Infobox at top, TOC below. Links to
// every other page using [[wikilink]] syntax so the renderer rewrites
// them to relative paths during the second pass.
func renderIndex(repoName, head string, files []FileEntry, symbols []Symbol, deps []Dependency, adrs []ADR, byModule map[string][]Symbol) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — wiki\n\n", repoName)

	// Infobox
	totalBytes := int64(0)
	for _, f := range files {
		totalBytes += f.Size
	}
	langs := languageBreakdown(files)
	primaryLang := strings.Split(strings.TrimPrefix(strings.SplitN(langs, "\n", 4)[3], "| "), " |")[0]
	fmt.Fprintf(&b, "> **infobox**\n")
	fmt.Fprintf(&b, "> | field | value |\n> |---|---|\n")
	fmt.Fprintf(&b, "> | repository | `%s` |\n", repoName)
	fmt.Fprintf(&b, "> | HEAD | `%s` |\n", head)
	fmt.Fprintf(&b, "> | files | %d |\n", len(files))
	fmt.Fprintf(&b, "> | size | %s |\n", humanBytes(totalBytes))
	fmt.Fprintf(&b, "> | primary language | %s |\n", strings.TrimSpace(primaryLang))
	fmt.Fprintf(&b, "> | modules | %d |\n", len(byModule))
	fmt.Fprintf(&b, "> | public symbols | %d |\n", len(symbols))
	fmt.Fprintf(&b, "> | dependencies | %d |\n", len(deps))
	fmt.Fprintf(&b, "> | decisions logged | %d |\n", len(adrs))
	fmt.Fprintf(&b, "> | generated | %s |\n", time.Now().UTC().Format(time.RFC3339))
	b.WriteString("\n")

	b.WriteString("## navigate\n\n")
	b.WriteString("- [[architecture]] — component map + module dependency graph\n")
	b.WriteString("- [[MODULES]] — every module as a clickable index\n")
	b.WriteString("- [[DECISIONS]] — ADR-style decisions extracted from git log\n")
	b.WriteString("- [[CONVENTIONS]] — naming/error/test patterns observed in code\n")
	b.WriteString("- [[DEPENDENCIES]] — direct + transitive deps with versions\n")
	b.WriteString("- [[GLOSSARY]] — domain terms and where they're defined\n")
	b.WriteString("- [[STUB-PAGES]] — concepts referenced but not yet documented (priority for next pass)\n")
	b.WriteString("- [[RECENT-CHANGES]] — last 30 commits\n")
	b.WriteString("- [[what-links-here]] — reverse index for any page\n\n")

	// Top modules by symbol count (the "hot" pages)
	type mc struct {
		name  string
		count int
	}
	var top []mc
	for m, ss := range byModule {
		top = append(top, mc{m, len(ss)})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].count > top[j].count })
	if len(top) > 10 {
		top = top[:10]
	}
	if len(top) > 0 {
		b.WriteString("## top modules (by public symbols)\n\n")
		for _, t := range top {
			fmt.Fprintf(&b, "- [[%s]] — %d symbols\n", t.name, t.count)
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n")
	b.WriteString("_Read [[architecture]] first to get the lay of the land. " +
		"Follow the linked pages as needed — every concept Claude might mention should resolve " +
		"to a real page or [[STUB-PAGES|stub]]._\n")
	return b.String()
}

func renderArchitecture(repoName string, files []FileEntry, byModule map[string][]Symbol) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# architecture — %s\n\n", repoName)
	b.WriteString("Top-level structure derived from inventory + module groupings.\n\n")

	// Module list with counts
	type m struct {
		name  string
		files int
		bytes int64
		syms  int
	}
	mods := map[string]*m{}
	for _, f := range files {
		k := topLevelOf(f.Path)
		if mods[k] == nil {
			mods[k] = &m{name: k}
		}
		mods[k].files++
		mods[k].bytes += f.Size
	}
	for k, ss := range byModule {
		if mods[k] == nil {
			mods[k] = &m{name: k}
		}
		mods[k].syms = len(ss)
	}
	keys := make([]string, 0, len(mods))
	for k := range mods {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString("## modules\n\n")
	b.WriteString("| module | files | size | public symbols |\n|---|---:|---:|---:|\n")
	for _, k := range keys {
		mm := mods[k]
		// Modules with public symbols get clickable wikilinks; the rest
		// (assets / installers / .github / root) appear as plain code spans.
		label := "`" + k + "`"
		if mm.syms > 0 {
			label = "[[" + k + "]]"
		}
		fmt.Fprintf(&b, "| %s | %d | %s | %d |\n", label, mm.files, humanBytes(mm.bytes), mm.syms)
	}
	b.WriteString("\n## see also\n\n")
	b.WriteString("- [[DEPENDENCIES]] — what we pull in from outside\n")
	b.WriteString("- [[CONVENTIONS]] — code patterns that show up across modules\n")
	return b.String()
}

func renderModulesIndex(byModule map[string][]Symbol) string {
	var b strings.Builder
	b.WriteString("# MODULES\n\n")
	b.WriteString("Every module ships its own page with infobox + symbols list.\n\n")
	keys := make([]string, 0, len(byModule))
	for k := range byModule {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "- [[%s]] — %d public symbols\n", k, len(byModule[k]))
	}
	return b.String()
}

func renderModulePage(repo, name string, syms []Symbol, files []FileEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# module — %s\n\n", name)

	// Infobox
	files = filterByModule(files, name)
	totalBytes := int64(0)
	for _, f := range files {
		totalBytes += f.Size
	}
	fmt.Fprintf(&b, "> **infobox**\n")
	fmt.Fprintf(&b, "> | field | value |\n> |---|---|\n")
	fmt.Fprintf(&b, "> | path | `%s/` |\n", name)
	fmt.Fprintf(&b, "> | files | %d |\n", len(files))
	fmt.Fprintf(&b, "> | size | %s |\n", humanBytes(totalBytes))
	fmt.Fprintf(&b, "> | public symbols | %d |\n", len(syms))
	fmt.Fprintf(&b, "> | parent | [[architecture]] |\n")
	b.WriteString("\n")

	// Group symbols by kind
	byKind := map[string][]Symbol{}
	for _, s := range syms {
		byKind[s.Kind] = append(byKind[s.Kind], s)
	}
	kinds := []string{"type", "interface", "func", "fn", "class", "var", "const"}
	for _, kind := range kinds {
		ss := byKind[kind]
		if len(ss) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", kind)
		for _, s := range ss {
			fmt.Fprintf(&b, "- [[%s]] — `%s:%d` — `%s`\n",
				s.Name, s.File, s.Line, oneLine(s.Signature))
		}
		b.WriteString("\n")
	}

	b.WriteString("## see also\n\n")
	b.WriteString("- [[architecture]]\n")
	b.WriteString("- [[CONVENTIONS]]\n")
	return b.String()
}

func renderSymbolPage(repo, page string, s Symbol, allSymbols []Symbol) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", page)

	fmt.Fprintf(&b, "> **infobox**\n")
	fmt.Fprintf(&b, "> | field | value |\n> |---|---|\n")
	fmt.Fprintf(&b, "> | name | `%s` |\n", s.Name)
	fmt.Fprintf(&b, "> | kind | %s |\n", s.Kind)
	fmt.Fprintf(&b, "> | language | %s |\n", s.Lang)
	fmt.Fprintf(&b, "> | file | `%s:%d` |\n", s.File, s.Line)
	fmt.Fprintf(&b, "> | module | [[%s]] |\n", s.Module)
	b.WriteString("\n")

	b.WriteString("## signature\n\n```")
	b.WriteString(s.Lang)
	b.WriteString("\n")
	b.WriteString(s.Signature)
	b.WriteString("\n```\n\n")

	if s.Doc != "" {
		b.WriteString("## doc\n\n")
		b.WriteString(s.Doc)
		b.WriteString("\n\n")
	}

	// Naive callers: any symbol whose Doc OR Signature mentions this name.
	// True call-graph lands in v0.13.1 with LSP/tree-sitter.
	callers := []Symbol{}
	for _, other := range allSymbols {
		if other.Name == s.Name && other.File == s.File {
			continue
		}
		if strings.Contains(other.Doc, s.Name) || strings.Contains(other.Signature, s.Name) {
			callers = append(callers, other)
			if len(callers) >= 10 {
				break
			}
		}
	}
	if len(callers) > 0 {
		b.WriteString("## referenced by\n\n")
		for _, c := range callers {
			fmt.Fprintf(&b, "- [[%s]] — `%s:%d`\n", c.Name, c.File, c.Line)
		}
		b.WriteString("\n")
	}

	b.WriteString("## see also\n\n")
	fmt.Fprintf(&b, "- [[%s]] (parent module)\n", s.Module)
	return b.String()
}

func renderDecisions(adrs []ADR) string {
	var b strings.Builder
	b.WriteString("# DECISIONS\n\n")
	b.WriteString("ADR-style record. Extracted automatically from substantive git commits ")
	b.WriteString("(long body OR contains a decision keyword like \"decided\" / \"because\" / \"chose\").\n\n")
	if len(adrs) == 0 {
		b.WriteString("(no decisions captured yet)\n")
		return b.String()
	}
	for _, a := range adrs {
		fmt.Fprintf(&b, "## %s — %s — %s\n\n", a.Date, a.Hash, a.Subject)
		fmt.Fprintf(&b, "**Author:** %s  \n", a.Author)
		if a.Reason != "" {
			fmt.Fprintf(&b, "**Reason:** %s  \n", a.Reason)
		}
		if a.Body != "" {
			b.WriteString("\n```\n")
			body := a.Body
			if len(body) > 2000 {
				body = body[:2000] + "\n…(truncated)"
			}
			b.WriteString(body)
			b.WriteString("\n```\n")
		}
		b.WriteString("\n---\n\n")
	}
	return b.String()
}

func renderConventions(symbols []Symbol, files []FileEntry) string {
	var b strings.Builder
	b.WriteString("# CONVENTIONS\n\n")
	b.WriteString("Patterns derived from observed AST counts. Updated each `graphify --pro` run.\n\n")

	// Naming style: counts of CamelCase vs snake_case among public symbols.
	camel, snake := 0, 0
	for _, s := range symbols {
		if strings.Contains(s.Name, "_") {
			snake++
		} else {
			camel++
		}
	}
	b.WriteString("## naming\n\n")
	if camel+snake > 0 {
		fmt.Fprintf(&b, "- CamelCase symbols: %d (%.0f%%)\n", camel, 100*float64(camel)/float64(camel+snake))
		fmt.Fprintf(&b, "- snake_case symbols: %d (%.0f%%)\n", snake, 100*float64(snake)/float64(camel+snake))
	}
	b.WriteString("\n")

	// Test layout
	tests := 0
	for _, f := range files {
		base := strings.ToLower(f.Path)
		if strings.HasSuffix(base, "_test.go") || strings.Contains(base, "/test/") ||
			strings.HasPrefix(base, "test/") || strings.HasSuffix(base, ".test.ts") ||
			strings.HasSuffix(base, ".spec.ts") {
			tests++
		}
	}
	b.WriteString("## tests\n\n")
	fmt.Fprintf(&b, "- test files: %d\n", tests)
	if tests > 0 && len(files) > 0 {
		fmt.Fprintf(&b, "- ratio: %.1f%% of files\n", 100*float64(tests)/float64(len(files)))
	}
	b.WriteString("\n")

	// Top modules by churn
	moduleCount := map[string]int{}
	for _, f := range files {
		moduleCount[topLevelOf(f.Path)]++
	}
	type mc struct {
		k string
		n int
	}
	var top []mc
	for k, n := range moduleCount {
		top = append(top, mc{k, n})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].n > top[j].n })
	if len(top) > 5 {
		top = top[:5]
	}
	b.WriteString("## largest modules\n\n")
	for _, t := range top {
		label := "`" + t.k + "`"
		if t.k != "(root)" {
			label = "[[" + t.k + "]]"
		}
		fmt.Fprintf(&b, "- %s — %d files\n", label, t.n)
	}

	return b.String()
}

func renderDependencies(deps []Dependency) string {
	var b strings.Builder
	b.WriteString("# DEPENDENCIES\n\n")
	if len(deps) == 0 {
		b.WriteString("(no manifests found at repo root)\n")
		return b.String()
	}
	bySource := map[string][]Dependency{}
	for _, d := range deps {
		bySource[d.Source] = append(bySource[d.Source], d)
	}
	keys := make([]string, 0, len(bySource))
	for k := range bySource {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, src := range keys {
		fmt.Fprintf(&b, "## %s (%d entries)\n\n", src, len(bySource[src]))
		b.WriteString("| name | version | direct | dev |\n|---|---|:---:|:---:|\n")
		for _, d := range bySource[src] {
			direct := " "
			if d.Direct {
				direct = "✓"
			}
			dev := " "
			if d.IsDev {
				dev = "✓"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", d.Name, d.Version, direct, dev)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderGlossary(g []GlossaryEntry) string {
	var b strings.Builder
	b.WriteString("# GLOSSARY\n\n")
	b.WriteString("Domain terms appearing >3 times across docs. Each entry links back to the files it appears in.\n\n")
	if len(g) == 0 {
		b.WriteString("(no terms passed the threshold)\n")
		return b.String()
	}
	for _, e := range g {
		fmt.Fprintf(&b, "### %s\n\n", e.Term)
		fmt.Fprintf(&b, "Appears %d times. ", e.Count)
		if len(e.Where) > 0 {
			b.WriteString("Defined or discussed in: ")
			parts := make([]string, len(e.Where))
			for i, p := range e.Where {
				parts[i] = "`" + p + "`"
			}
			b.WriteString(strings.Join(parts, ", "))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderRecent(commits []Commit) string {
	var b strings.Builder
	b.WriteString("# RECENT-CHANGES\n\n")
	if len(commits) == 0 {
		b.WriteString("(no git history)\n")
		return b.String()
	}
	b.WriteString("| hash | when | author | subject |\n|---|---|---|---|\n")
	for _, c := range commits {
		subj := strings.ReplaceAll(c.Subject, "|", "\\|")
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.Hash, c.When, c.Author, subj)
	}
	return b.String()
}

func renderStubs(stubs []StubEntry) string {
	var b strings.Builder
	b.WriteString("# STUB-PAGES\n\n")
	b.WriteString("Concepts referenced via `[[wikilinks]]` somewhere in the wiki but not yet ")
	b.WriteString("backed by their own page. Sorted by reference count (highest first) so the ")
	b.WriteString("next `graphify --pro` pass can prioritize.\n\n")
	if len(stubs) == 0 {
		b.WriteString("(no broken references — wiki is fully resolved!)\n")
		return b.String()
	}
	for _, s := range stubs {
		fmt.Fprintf(&b, "- **%s** — referenced %dx\n", s.Name, s.RefCount)
	}
	return b.String()
}

func renderWhatLinksHere(reg *WikiRegistry) string {
	var b strings.Builder
	b.WriteString("# what-links-here\n\n")
	b.WriteString("Reverse index: for each page, which other pages link TO it.\n\n")
	all := reg.AllPages()
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		refs := reg.Backrefs(k)
		if len(refs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", k)
		for _, r := range refs {
			fmt.Fprintf(&b, "- [[%s]]\n", r)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// filterByModule returns just the files whose top-level dir matches `name`.
func filterByModule(files []FileEntry, name string) []FileEntry {
	var out []FileEntry
	for _, f := range files {
		if topLevelOf(f.Path) == name {
			out = append(out, f)
		}
	}
	return out
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "`", "'")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}
