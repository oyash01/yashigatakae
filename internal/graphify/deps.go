package graphify

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Dependency is one row in DEPENDENCIES.md.
type Dependency struct {
	Name      string
	Version   string
	Direct    bool
	Source    string // go.mod | package.json | requirements.txt | Cargo.toml | pyproject.toml
	IsDev     bool
}

// ExtractDependencies parses every recognized lockfile / manifest in the repo
// and returns deduped entries. Per-source parser failures don't block others.
func ExtractDependencies(repo string) []Dependency {
	var out []Dependency
	out = append(out, parseGoMod(filepath.Join(repo, "go.mod"))...)
	out = append(out, parsePackageJSON(filepath.Join(repo, "package.json"))...)
	out = append(out, parseRequirementsTxt(filepath.Join(repo, "requirements.txt"))...)
	out = append(out, parseCargoToml(filepath.Join(repo, "Cargo.toml"))...)
	out = append(out, parsePyprojectToml(filepath.Join(repo, "pyproject.toml"))...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func parseGoMod(path string) []Dependency {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var deps []Dependency
	scanner := bufio.NewScanner(f)
	inRequire := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "require (") {
			inRequire = true
			continue
		}
		if line == ")" {
			inRequire = false
			continue
		}
		direct := strings.HasPrefix(line, "require ")
		if !inRequire && !direct {
			continue
		}
		raw := strings.TrimPrefix(line, "require ")
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "//") {
			continue
		}
		// strip trailing "// indirect"
		isIndirect := strings.Contains(raw, "indirect")
		if i := strings.Index(raw, "//"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
		parts := strings.Fields(raw)
		if len(parts) < 2 {
			continue
		}
		deps = append(deps, Dependency{
			Name:    parts[0],
			Version: parts[1],
			Direct:  !isIndirect,
			Source:  "go.mod",
		})
	}
	return deps
}

func parsePackageJSON(path string) []Dependency {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil
	}
	var deps []Dependency
	for name, ver := range pkg.Dependencies {
		deps = append(deps, Dependency{Name: name, Version: ver, Direct: true, Source: "package.json"})
	}
	for name, ver := range pkg.DevDependencies {
		deps = append(deps, Dependency{Name: name, Version: ver, Direct: true, IsDev: true, Source: "package.json"})
	}
	return deps
}

func parseRequirementsTxt(path string) []Dependency {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var deps []Dependency
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		// foo==1.2 / foo>=1.2 / foo
		name, ver := line, ""
		for _, sep := range []string{"==", ">=", "<=", ">", "<", "~="} {
			if i := strings.Index(line, sep); i > 0 {
				name = strings.TrimSpace(line[:i])
				ver = strings.TrimSpace(line[i+len(sep):])
				break
			}
		}
		if i := strings.IndexAny(name, "[;"); i > 0 {
			name = name[:i]
		}
		deps = append(deps, Dependency{Name: name, Version: ver, Direct: true, Source: "requirements.txt"})
	}
	return deps
}

func parseCargoToml(path string) []Dependency {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var deps []Dependency
	scanner := bufio.NewScanner(f)
	section := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section != "dependencies" && section != "dev-dependencies" {
			continue
		}
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			name := strings.TrimSpace(parts[0])
			ver := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			if name == "" || strings.HasPrefix(name, "#") {
				continue
			}
			deps = append(deps, Dependency{
				Name:    name,
				Version: ver,
				Direct:  true,
				IsDev:   section == "dev-dependencies",
				Source:  "Cargo.toml",
			})
		}
	}
	return deps
}

func parsePyprojectToml(path string) []Dependency {
	// Minimal TOML walk — pyproject.toml's dependencies live in
	// [project] dependencies = ["foo>=1.2", ...] OR
	// [tool.poetry.dependencies] foo = "^1.2"
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var deps []Dependency
	scanner := bufio.NewScanner(f)
	section := ""
	inProjectDeps := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			inProjectDeps = false
			continue
		}
		if section == "project" && strings.HasPrefix(line, "dependencies") {
			inProjectDeps = true
			continue
		}
		if inProjectDeps {
			if strings.Contains(line, "]") {
				inProjectDeps = false
			}
			body := strings.Trim(line, " ,\"]")
			if body == "" {
				continue
			}
			deps = append(deps, Dependency{Name: body, Direct: true, Source: "pyproject.toml"})
		}
		if strings.HasPrefix(section, "tool.poetry.dependencies") || strings.HasPrefix(section, "tool.poetry.dev-dependencies") {
			if strings.Contains(line, "=") {
				parts := strings.SplitN(line, "=", 2)
				name := strings.TrimSpace(parts[0])
				ver := strings.Trim(strings.TrimSpace(parts[1]), `"`)
				if name != "" && !strings.HasPrefix(name, "#") {
					deps = append(deps, Dependency{
						Name:    name,
						Version: ver,
						Direct:  true,
						IsDev:   strings.Contains(section, "dev"),
						Source:  "pyproject.toml",
					})
				}
			}
		}
	}
	return deps
}
