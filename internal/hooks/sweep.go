package hooks

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oyash01/yashigatakae/internal/mempalace"
)

// SessionEndPayload is the JSON shape Claude Code sends to SessionEnd hooks
// over stdin. We tolerate unknown fields and missing keys.
type SessionEndPayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
}

// transcriptEntry is one line of a Claude Code session JSONL.
type transcriptEntry struct {
	Type    string          `json:"type"`               // "user", "assistant", "system", ...
	Role    string          `json:"role"`               // sometimes "user" / "assistant"
	Message json.RawMessage `json:"message,omitempty"`  // newer schema
	Content json.RawMessage `json:"content,omitempty"`  // legacy
	Text    string          `json:"text,omitempty"`     // some variants put text here
	UUID    string          `json:"uuid,omitempty"`
}

// Sweep parses the SessionEnd payload from stdin, extracts user prompts +
// assistant text responses from the transcript, and writes them to mempalace
// via the local store (no network). Returns the count of entries written.
//
// Defensive: fails closed (returns nil) if no transcript file is present —
// SessionEnd hooks are not allowed to break a session shutdown.
func Sweep(ctx context.Context, in io.Reader) (int, error) {
	raw, _ := io.ReadAll(in)
	if len(raw) == 0 {
		return 0, nil
	}
	var payload SessionEndPayload
	_ = json.Unmarshal(raw, &payload)
	if payload.TranscriptPath == "" {
		return 0, nil
	}

	f, err := os.Open(payload.TranscriptPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil // session never wrote a transcript
		}
		return 0, err
	}
	defer f.Close()

	project := inferProject(payload.CWD)
	tags := "session,session:" + payload.SessionID

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024) // tolerate long lines
	count := 0
	var pending strings.Builder // user-prompt buffer; flushed when assistant responds

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e transcriptEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		role := e.Role
		if role == "" {
			role = e.Type
		}
		text := extractText(e)
		if text == "" {
			continue
		}
		switch role {
		case "user":
			pending.Reset()
			pending.WriteString("USER: ")
			pending.WriteString(truncate(text, 4000))
		case "assistant":
			if pending.Len() == 0 {
				continue
			}
			pending.WriteString("\n\nASSISTANT: ")
			pending.WriteString(truncate(text, 4000))
			id, err := mempalace.Remember(ctx, mempalace.RememberOptions{
				Body:    pending.String(),
				Source:  "session",
				Project: project,
				Tags:    tags,
			})
			pending.Reset()
			if err != nil {
				// Don't propagate — hook must not block session shutdown.
				continue
			}
			_ = id
			count++
		}
	}
	return count, scanner.Err()
}

// extractText walks the few schemas we've seen in Claude Code transcripts to
// pull out a printable string.
func extractText(e transcriptEntry) string {
	if e.Text != "" {
		return e.Text
	}

	// Newer schema: message is { content: [ { type:"text", text:"..." }, ... ] }
	if len(e.Message) > 0 {
		var msg struct {
			Content json.RawMessage `json:"content"`
			Text    string          `json:"text"`
		}
		if err := json.Unmarshal(e.Message, &msg); err == nil {
			if msg.Text != "" {
				return msg.Text
			}
			if t := walkContent(msg.Content); t != "" {
				return t
			}
		}
	}

	if t := walkContent(e.Content); t != "" {
		return t
	}
	return ""
}

// walkContent flattens a content array (used by both Anthropic + MCP shapes)
// into a single string.
func walkContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string first (some schemas just put text directly).
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil && asString != "" {
		return asString
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return ""
	}
	var out strings.Builder
	for _, m := range arr {
		if t, ok := m["text"].(string); ok && t != "" {
			if out.Len() > 0 {
				out.WriteString("\n")
			}
			out.WriteString(t)
		}
	}
	return out.String()
}

func inferProject(cwd string) string {
	if cwd == "" {
		return ""
	}
	return filepath.Base(cwd)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// RunSweepCmd is the entry point invoked by the SessionEnd hook. Prints a
// terse one-line summary on success; never returns a non-zero exit (we don't
// want the hook to make session-end fail).
func RunSweepCmd() {
	n, err := Sweep(context.Background(), os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "yashigatakae hooks sweep: %v\n", err)
		// Continue — don't propagate.
	}
	if n > 0 {
		fmt.Fprintf(os.Stderr, "yashigatakae: swept %d transcript pair(s) into mempalace\n", n)
	}
}
