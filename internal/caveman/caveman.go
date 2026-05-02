// Package caveman installs the token-saving hooks (SessionStart,
// UserPromptSubmit, PreToolUse) into ~/.claude/hooks/ and exposes a
// CLI to switch the compression level (lite/full/ultra/off).
package caveman

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

type Level string

const (
	LevelLite  Level = "lite"
	LevelFull  Level = "full"
	LevelUltra Level = "ultra"
	LevelOff   Level = "off"
)

// SetLevel writes the chosen level to ~/.yashigatakae/caveman.json.
// The activate hook reads this on every SessionStart.
func SetLevel(s string) error {
	lvl := Level(s)
	switch lvl {
	case LevelLite, LevelFull, LevelUltra, LevelOff:
	default:
		return fmt.Errorf("invalid caveman level %q (want: lite|full|ultra|off)", s)
	}

	yashDir, err := osdetect.YashigatakaeDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(yashDir, 0o755); err != nil {
		return err
	}

	cfg := map[string]any{"level": string(lvl)}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(yashDir, "caveman.json"), b, 0o644); err != nil {
		return err
	}
	fmt.Printf("✓ caveman level set to %s\n", lvl)
	return nil
}

// CurrentLevel reads the configured level (defaults to full).
func CurrentLevel() (Level, error) {
	yashDir, err := osdetect.YashigatakaeDir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(yashDir, "caveman.json"))
	if err != nil {
		return LevelFull, nil // sane default
	}
	var cfg struct {
		Level string `json:"level"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return LevelFull, nil
	}
	return Level(cfg.Level), nil
}

// HooksInstalled reports whether the caveman hook files exist in ~/.claude/hooks.
func HooksInstalled() (bool, error) {
	claudeDir, err := osdetect.ClaudeDir()
	if err != nil {
		return false, err
	}
	required := []string{
		"caveman-activate.js",
		"caveman-mode-tracker.js",
		"caveman-config.js",
		"caveman-statusline.sh",
	}
	for _, name := range required {
		if _, err := os.Stat(filepath.Join(claudeDir, "hooks", name)); err != nil {
			return false, nil
		}
	}
	return true, nil
}
