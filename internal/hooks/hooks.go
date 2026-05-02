// Package hooks wires SessionStart, UserPromptSubmit, PostToolUse, and
// SessionEnd hooks into ~/.claude/settings.json. Hook script contents are
// rendered from the state-repo templates by the state package; this package
// just registers them in settings.json.
package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// HookSpec describes a single hook entry in settings.json.
type HookSpec struct {
	Event   string // SessionStart | UserPromptSubmit | PostToolUse | SessionEnd | PreToolUse
	Matcher string // tool matcher for PostToolUse; empty for whole-session events
	Type    string // command
	Cmd     string // shell command (already path-templated for this OS)
}

// Register inserts the given hooks into ~/.claude/settings.json. Existing
// matching entries (same event + same Cmd) are not duplicated.
func Register(specs []HookSpec) error {
	claudeDir, err := osdetect.ClaudeDir()
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")

	var settings map[string]any
	if b, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(b, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	} else if os.IsNotExist(err) {
		settings = map[string]any{}
	} else {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	for _, s := range specs {
		entries, _ := hooks[s.Event].([]any)
		// Build a fresh entry. We always replace yashigatakae-managed entries (idempotent).
		newEntry := map[string]any{}
		if s.Matcher != "" {
			newEntry["matcher"] = s.Matcher
		}
		newEntry["hooks"] = []any{
			map[string]any{
				"type":     s.Type,
				"command":  s.Cmd,
				"managed":  "yashigatakae",
			},
		}
		// Drop any existing yashigatakae-managed entry with the same matcher.
		filtered := entries[:0]
		for _, raw := range entries {
			e, _ := raw.(map[string]any)
			if e == nil {
				filtered = append(filtered, raw)
				continue
			}
			isManaged := false
			if hh, ok := e["hooks"].([]any); ok {
				for _, h := range hh {
					if hm, ok := h.(map[string]any); ok && hm["managed"] == "yashigatakae" {
						isManaged = true
						break
					}
				}
			}
			sameMatcher := s.Matcher == ""
			if !sameMatcher {
				if m, ok := e["matcher"].(string); ok && m == s.Matcher {
					sameMatcher = true
				}
			}
			if isManaged && sameMatcher {
				continue
			}
			filtered = append(filtered, raw)
		}
		filtered = append(filtered, newEntry)
		hooks[s.Event] = filtered
	}
	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("  · registered %d managed hook(s) in %s\n", len(specs), settingsPath)
	return nil
}
