package graphify

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Symbol is one identifier extracted from source. v0.13 uses regex per
// language — fast, framework-free, and sufficient for the wiki use case.
// Tree-sitter / LSP arrives in v0.13.1 for accurate signatures.
type Symbol struct {
	Name      string
	Kind      string // func | type | var | const | class | interface
	Lang      string
	File      string // path relative to repo
	Line      int
	Signature string // truncated at 200 chars
	Module    string // parent module name (top-level dir under repo)
	Doc       string // leading comment block above the declaration
}

// ExtractSymbols walks the file inventory and pulls public symbols out of
// every recognized language. Module is set to the first path segment
// (top-level directory) so we can group symbols per module page.
func ExtractSymbols(repo string, files []FileEntry) []Symbol {
	var out []Symbol
	for _, f := range files {
		switch f.Lang {
		case "go":
			out = append(out, scanGo(repo, f)...)
		case "python":
			out = append(out, scanPython(repo, f)...)
		case "typescript", "javascript":
			out = append(out, scanJSTS(repo, f)...)
		case "rust":
			out = append(out, scanRust(repo, f)...)
		}
	}
	return out
}

var (
	reGoFunc      = regexp.MustCompile(`^func\s+(?:\(\s*\w+\s+\*?\w+\s*\)\s+)?([A-Z]\w*)\s*\(`)
	reGoType      = regexp.MustCompile(`^type\s+([A-Z]\w*)\s+`)
	reGoVar       = regexp.MustCompile(`^var\s+([A-Z]\w*)\s+`)
	reGoConst     = regexp.MustCompile(`^const\s+([A-Z]\w*)\s+`)
	rePyDef       = regexp.MustCompile(`^def\s+([A-Za-z_]\w*)\s*\(`)
	rePyClass     = regexp.MustCompile(`^class\s+([A-Za-z_]\w*)`)
	reJSExport    = regexp.MustCompile(`^export\s+(?:default\s+)?(?:async\s+)?(function|class|const|let|interface|type)\s+([A-Za-z_]\w*)`)
	reRustPubFn   = regexp.MustCompile(`^pub\s+(?:async\s+)?fn\s+([A-Za-z_]\w*)`)
	reRustPubType = regexp.MustCompile(`^pub\s+(?:struct|enum|trait)\s+([A-Za-z_]\w*)`)
)

func scanGo(repo string, f FileEntry) []Symbol {
	return scanLines(repo, f, func(line string) (string, string, string) {
		if m := reGoFunc.FindStringSubmatch(line); m != nil {
			return m[1], "func", line
		}
		if m := reGoType.FindStringSubmatch(line); m != nil {
			return m[1], "type", line
		}
		if m := reGoVar.FindStringSubmatch(line); m != nil {
			return m[1], "var", line
		}
		if m := reGoConst.FindStringSubmatch(line); m != nil {
			return m[1], "const", line
		}
		return "", "", ""
	})
}

func scanPython(repo string, f FileEntry) []Symbol {
	return scanLines(repo, f, func(line string) (string, string, string) {
		l := strings.TrimLeft(line, " \t")
		if l != line {
			return "", "", "" // private to a class? skip module-level symbol surface
		}
		if m := rePyClass.FindStringSubmatch(line); m != nil {
			return m[1], "class", line
		}
		if m := rePyDef.FindStringSubmatch(line); m != nil {
			if strings.HasPrefix(m[1], "_") {
				return "", "", ""
			}
			return m[1], "func", line
		}
		return "", "", ""
	})
}

func scanJSTS(repo string, f FileEntry) []Symbol {
	return scanLines(repo, f, func(line string) (string, string, string) {
		if m := reJSExport.FindStringSubmatch(line); m != nil {
			return m[2], m[1], line
		}
		return "", "", ""
	})
}

func scanRust(repo string, f FileEntry) []Symbol {
	return scanLines(repo, f, func(line string) (string, string, string) {
		if m := reRustPubFn.FindStringSubmatch(line); m != nil {
			return m[1], "fn", line
		}
		if m := reRustPubType.FindStringSubmatch(line); m != nil {
			return m[1], "type", line
		}
		return "", "", ""
	})
}

// scanLines opens a file and applies `extract` to every line. extract returns
// (name, kind, signature); empty name = no symbol on this line.
func scanLines(repo string, f FileEntry, extract func(string) (string, string, string)) []Symbol {
	full := filepath.Join(repo, f.Path)
	fp, err := os.Open(full)
	if err != nil {
		return nil
	}
	defer fp.Close()

	module := topLevelOf(f.Path)
	var out []Symbol
	scanner := bufio.NewScanner(fp)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var docBuf []string
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		// Track preceding comment block as the doc.
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "///") {
			docBuf = append(docBuf, strings.TrimLeft(trimmed, "/# "))
			continue
		}
		name, kind, sig := extract(line)
		if name == "" {
			docBuf = nil
			continue
		}
		if len(sig) > 200 {
			sig = sig[:200] + "…"
		}
		out = append(out, Symbol{
			Name:      name,
			Kind:      kind,
			Lang:      f.Lang,
			File:      f.Path,
			Line:      lineNum,
			Signature: sig,
			Module:    module,
			Doc:       strings.Join(docBuf, "\n"),
		})
		docBuf = nil
	}
	return out
}

// topLevelOf returns a "module" identifier for a file path. The heuristic:
//   - "internal/foo/bar.go"   → "internal/foo"
//   - "internal/foo.go"        → "internal"
//   - "cmd/yashigatakae/main.go" → "cmd/yashigatakae"
//   - "pkg/widget/widget.go"   → "pkg/widget"
//   - "main.go"                → "(root)"
//
// First-segment-only granularity ("internal" alone) would lump unrelated
// packages together; expanding to the package directory makes the wiki
// substantially more navigable on Go-style layouts.
func topLevelOf(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	if len(parts) <= 1 {
		return "(root)"
	}
	if len(parts) == 2 {
		return parts[0]
	}
	// 3+ segments: take the first two when the first is one of the common
	// "container" dirs, otherwise just the first.
	containers := map[string]bool{
		"internal": true, "pkg": true, "cmd": true, "src": true, "lib": true, "app": true,
	}
	if containers[parts[0]] {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// GroupByModule collects symbols by their module field, sorted within each.
func GroupByModule(symbols []Symbol) map[string][]Symbol {
	out := map[string][]Symbol{}
	for _, s := range symbols {
		out[s.Module] = append(out[s.Module], s)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool {
			if out[k][i].File != out[k][j].File {
				return out[k][i].File < out[k][j].File
			}
			return out[k][i].Line < out[k][j].Line
		})
	}
	return out
}

// SymbolFingerprint returns a stable name we can use for both the symbol page
// filename AND wikilink target. Avoids collisions when two modules export the
// same name by qualifying the second one.
func SymbolFingerprint(s Symbol, seen map[string]int) string {
	name := s.Name
	seen[name]++
	if seen[name] > 1 {
		name = fmt.Sprintf("%s.%s", s.Module, s.Name)
	}
	return name
}
