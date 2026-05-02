// Package mcp registers the bifrost endpoint as the sole MCP server in
// ~/.claude/settings.json. v0.1 writes a placeholder pointing at localhost
// (no daemon yet); v0.2 will swap in the real VPS HTTPS endpoint.
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

const (
	BifrostName     = "bifrost"
	PlaceholderURL  = "http://localhost:8443" // v0.1 placeholder
	BifrostKeyEnv   = "BIFROST_API_KEY"
)

// settingsShape captures the keys we touch. Other keys are preserved verbatim.
type settingsShape struct {
	MCP       map[string]any `json:"mcpServers,omitempty"`
	Remainder map[string]any `json:"-"`
}

// RegisterPlaceholder ensures ~/.claude/settings.json has a single MCP entry
// named "bifrost" pointing at the v0.1 placeholder URL. Existing keys (hooks,
// statusLine, voice, theme, etc.) are preserved.
func RegisterPlaceholder() error {
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

	mcp, _ := settings["mcpServers"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	mcp[BifrostName] = map[string]any{
		"type":    "http",
		"url":     PlaceholderURL,
		"comment": "yashigatakae bifrost gateway — v0.1 placeholder; v0.2 will swap to VPS HTTPS endpoint",
	}
	settings["mcpServers"] = mcp

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("  · registered placeholder MCP entry %q in %s\n", BifrostName, settingsPath)
	return nil
}
