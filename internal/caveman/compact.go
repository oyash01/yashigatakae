package caveman

import (
	"fmt"
	"strings"
	"time"
)

// EstimateTokens returns a fast char/4 token estimate. Anthropic's tokenizer
// averages ~3.5–4 chars/token for English; this is the same heuristic used by
// the official cookbook for cheap pressure checks.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return len(s) / 4
}

// CompactionPrompt is the system message we inject when context pressure crosses
// the threshold. The structure is taken from the Anthropic context-compaction
// cookbook: keep only essential facts, file paths, decisions, TODOs.
func CompactionPrompt(targetTokens int, sessionID string) string {
	ts := time.Now().UTC().Format(time.RFC3339)
	if targetTokens <= 0 {
		targetTokens = 2000
	}
	var b strings.Builder
	b.WriteString("## compaction ")
	b.WriteString(ts)
	if sessionID != "" {
		b.WriteString(" (session=")
		b.WriteString(sessionID)
		b.WriteString(")")
	}
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf(
		"Context approaching the configured caveman limit. Summarize the prior "+
			"conversation into ≤%d tokens of essential facts, decisions, file paths, "+
			"and TODOs. Drop verbose tool output, exploratory dead-ends, and resolved "+
			"errors. Preserve: \n"+
			"  • exact file paths + line numbers we touched\n"+
			"  • decisions made and the reason\n"+
			"  • open TODOs (with status)\n"+
			"  • any constants/keys/config values referenced more than once\n"+
			"  • the user's original goal in 1 sentence\n\n"+
			"Output the summary as markdown under a heading `## context summary`. "+
			"After this turn, treat that summary as the only relevant prior context.",
		targetTokens))
	return b.String()
}

// PressureReport is what the SessionStart hook gets back. The hook decides
// whether to inject CompactionPrompt based on ShouldCompact.
type PressureReport struct {
	EstimatedTokens int    `json:"estimated_tokens"`
	Threshold       int    `json:"threshold"`
	ShouldCompact   bool   `json:"should_compact"`
	Prompt          string `json:"prompt,omitempty"` // populated only when ShouldCompact
}

// CheckPressure computes whether the conversation has crossed the configured
// threshold. The transcript is the concatenated prior messages (passed in from
// the hook — caveman doesn't read ~/.claude/sessions itself to keep this pure).
func CheckPressure(transcript string, sessionID string) (PressureReport, error) {
	cfg, err := Load()
	if err != nil {
		return PressureReport{}, err
	}
	tokens := EstimateTokens(transcript)
	rep := PressureReport{
		EstimatedTokens: tokens,
		Threshold:       cfg.CompactThresholdTokens,
		ShouldCompact:   cfg.CompactThresholdTokens > 0 && tokens >= cfg.CompactThresholdTokens,
	}
	if rep.ShouldCompact {
		rep.Prompt = CompactionPrompt(cfg.CompactTargetTokens, sessionID)
	}
	return rep, nil
}
