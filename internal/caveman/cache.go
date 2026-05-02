package caveman

import "encoding/json"

// PromptCacheMarker is the JSON object Claude Code emits in a SessionStart hook
// to ask the API to cache a static prefix of the system prompt. Anthropic's
// docs call this "ephemeral cache_control".
//
// We expose a tiny helper instead of a full SDK because the only consumer is
// the SessionStart hook script — it shells out to `yashigatakae caveman cache
// ephemeral` and embeds whatever bytes we print into the prompt envelope.
func PromptCacheMarker() ([]byte, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}
	if !cfg.PromptCacheEphemeral {
		return []byte(`{"cache_control":null}`), nil
	}
	return json.Marshal(map[string]any{
		"cache_control": map[string]string{"type": "ephemeral"},
	})
}
