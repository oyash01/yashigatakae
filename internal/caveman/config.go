package caveman

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// Config captures all caveman settings. The on-disk shape is forward-compatible:
// new fields default to zero/empty and old binaries skip them.
type Config struct {
	Level     string `json:"level"`     // lite | full | ultra | off (kept for back-compat)
	Verbosity string `json:"verbosity"` // terse | normal | verbose — independent of Level

	// Auto-compaction
	CompactThresholdTokens int `json:"compact_threshold_tokens"` // 0 = disabled
	CompactTargetTokens    int `json:"compact_target_tokens"`    // size budget for the summary

	// Per-tool result-size caps. Key = tool name (Bash, Read, Edit, Write, WebFetch).
	// Value = bytes; result truncated to first 30 + last 30 lines if over.
	// 0 in map or missing key = use DefaultToolCap.
	ToolCaps       map[string]int `json:"tool_caps"`
	DefaultToolCap int            `json:"default_tool_cap"` // bytes; 0 disables truncation entirely

	// Prompt cache
	PromptCacheEphemeral bool `json:"prompt_cache_ephemeral"` // emit cache_control markers
}

// Defaults match the values referenced in the v0.11.0 plan: compact at 100k
// tokens to a 2k summary, truncate any tool output >2 KiB, mark static prompt
// chunks ephemeral so the API caches them.
func Defaults() Config {
	return Config{
		Level:                  string(LevelFull),
		Verbosity:              "normal",
		CompactThresholdTokens: 100_000,
		CompactTargetTokens:    2_000,
		ToolCaps: map[string]int{
			"Bash":     4_000,
			"Read":     8_000,
			"WebFetch": 4_000,
		},
		DefaultToolCap:       2_000,
		PromptCacheEphemeral: true,
	}
}

var (
	cfgMu     sync.Mutex
	cachedCfg *Config
)

// Load returns the caveman config from disk, applying defaults for any field
// not present. Missing file → defaults.
func Load() (Config, error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if cachedCfg != nil {
		return *cachedCfg, nil
	}

	yashDir, err := osdetect.YashigatakaeDir()
	if err != nil {
		return Defaults(), err
	}
	cfg := Defaults()

	b, err := os.ReadFile(filepath.Join(yashDir, "caveman.json"))
	if err != nil {
		if os.IsNotExist(err) {
			cachedCfg = &cfg
			return cfg, nil
		}
		return cfg, err
	}
	// Decode into a temporary so missing fields keep the defaults.
	tmp := cfg
	if err := json.Unmarshal(b, &tmp); err != nil {
		return cfg, fmt.Errorf("caveman.json parse: %w", err)
	}
	if tmp.Level == "" {
		tmp.Level = cfg.Level
	}
	if tmp.Verbosity == "" {
		tmp.Verbosity = cfg.Verbosity
	}
	if tmp.ToolCaps == nil {
		tmp.ToolCaps = cfg.ToolCaps
	}
	cachedCfg = &tmp
	return tmp, nil
}

// Save persists the config and invalidates the in-process cache.
func Save(cfg Config) error {
	yashDir, err := osdetect.YashigatakaeDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(yashDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(yashDir, "caveman.json"), b, 0o644); err != nil {
		return err
	}
	cfgMu.Lock()
	cachedCfg = &cfg
	cfgMu.Unlock()
	return nil
}

// CapForTool returns the byte cap for a given tool name. 0 means "no cap".
func (c Config) CapForTool(tool string) int {
	if c.ToolCaps != nil {
		if v, ok := c.ToolCaps[tool]; ok && v > 0 {
			return v
		}
	}
	return c.DefaultToolCap
}
