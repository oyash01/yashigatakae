package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	gstackSectionMarker   = "## gstack"
	cavemanSectionMarker  = "## caveman"
	kintsugiSectionMarker = "## kintsugi (cross-device continuity)"
)

// ensureClaudeMDSections idempotently appends the three yashigatakae-managed
// sections to ~/.claude/CLAUDE.md if they aren't already present.
//
// Source priority for the section bodies:
//   1. State repo at <stateDir>/templates/CLAUDE.md.tmpl (user's customizations)
//   2. Embedded fallback baked into the binary
//
// Lets users override the canned sections without losing them when the binary
// updates.
func ensureClaudeMDSections(claudeDir, stateDir string) error {
	mdPath := filepath.Join(claudeDir, "CLAUDE.md")
	var src string
	if stateDir != "" {
		srcPath := filepath.Join(stateDir, "templates", "CLAUDE.md.tmpl")
		if raw, err := os.ReadFile(srcPath); err == nil {
			src = string(raw)
		}
	}
	if src == "" {
		// Fall back to embedded.
		raw, err := embedded.ReadFile("embedded/templates/CLAUDE.md.tmpl")
		if err != nil {
			fmt.Println("  · no CLAUDE.md.tmpl available — skipping section insert")
			return nil
		}
		src = string(raw)
	}

	var existing string
	if b, err := os.ReadFile(mdPath); err == nil {
		existing = string(b)
	}

	missing := []string{}
	for _, marker := range []string{gstackSectionMarker, cavemanSectionMarker, kintsugiSectionMarker} {
		if !strings.Contains(existing, marker) {
			section := extractSection(src, marker)
			if section != "" {
				missing = append(missing, section)
			}
		}
	}

	if len(missing) == 0 {
		fmt.Println("  · all yashigatakae sections already present in CLAUDE.md")
		return nil
	}

	if !strings.HasSuffix(existing, "\n") && existing != "" {
		existing += "\n"
	}
	for _, sec := range missing {
		existing += "\n" + strings.TrimRight(sec, "\n") + "\n"
	}
	if err := os.WriteFile(mdPath, []byte(existing), 0o644); err != nil {
		return err
	}
	fmt.Printf("  · appended %d section(s) to %s\n", len(missing), mdPath)
	return nil
}

// extractSection pulls a single `## <heading>` section from the source text.
// Returns the section verbatim (heading line included) up to but not including
// the next `## ` line. Returns "" if not found.
func extractSection(src, marker string) string {
	idx := strings.Index(src, marker)
	if idx == -1 {
		return ""
	}
	rest := src[idx:]
	// Find next top-level heading ("\n## ") and cut.
	if next := strings.Index(rest[len(marker):], "\n## "); next != -1 {
		return rest[:len(marker)+next]
	}
	return rest
}
