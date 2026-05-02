package kintsugi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// HandoffOptions captures all CLI flags for `yashigatakae handoff`.
type HandoffOptions struct {
	Note         string
	BaseURL      string // override BIFROST_URL → kintsugi base
	APIKey       string // override BIFROST_API_KEY
	KintsugiKey  string // override KINTSUGI_KEY (encryption pass)
	SessionID    string // override auto-detected session
	IncludeMemo  bool   // also pack ~/.claude/projects/<encoded-cwd>/memory
	DryRun       bool   // pack + encrypt + print size, do NOT upload
}

// Handoff executes the v0.3.0-rc2 handoff flow:
//  1. Detect the active Claude Code session (most-recently-modified
//     ~/.claude/sessions/<pid>.json)
//  2. Locate its transcript JSONL inside ~/.claude/projects/<encoded-cwd>/
//  3. Pack into a tar.gz (manifest + transcript + optional memory dir)
//  4. Encrypt with age (KINTSUGI_KEY)
//  5. POST to the relay
//  6. Print resume code (sha256-12 of ciphertext) for the other machine to use
func Handoff(ctx context.Context, opts HandoffOptions) (string, error) {
	resolved, err := resolveEnv(opts.BaseURL, opts.APIKey, opts.KintsugiKey)
	if err != nil {
		return "", err
	}

	sid, sourceCWD, transcript, err := detectSession(opts.SessionID)
	if err != nil {
		return "", err
	}

	pack := PackOptions{
		SessionID:      sid,
		TranscriptFile: transcript,
		SourceCWD:      sourceCWD,
		Note:           opts.Note,
	}
	if opts.IncludeMemo {
		mem := projectMemoryDir(sourceCWD)
		if _, err := os.Stat(mem); err == nil {
			pack.MemoryDir = mem
		}
	}

	body, mf, err := Pack(pack)
	if err != nil {
		return "", err
	}
	cipher, err := Encrypt(body, resolved.kintsugiKey)
	if err != nil {
		return "", err
	}
	code := Fingerprint(cipher)

	fmt.Printf("  session: %s\n", sid)
	fmt.Printf("  source:  %s on %s\n", sourceCWD, mf.SourceMachine)
	fmt.Printf("  size:    %d bytes (encrypted)\n", len(cipher))

	if opts.DryRun {
		fmt.Println("  (dry-run — not uploaded)")
		return code, nil
	}

	client := NewClient(resolved.relayBase, resolved.apiKey)
	if err := client.PostCheckpoint(ctx, sid, mf.SourceMachine, cipher); err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	return code, nil
}

// detectSession finds the active session by reading the most recent file in
// ~/.claude/sessions/. Falls back to override if provided.
func detectSession(override string) (sid, cwd, transcript string, err error) {
	claudeDir, err := osdetect.ClaudeDir()
	if err != nil {
		return "", "", "", err
	}
	sessionsDir := filepath.Join(claudeDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return "", "", "", fmt.Errorf("no Claude Code sessions found at %s", sessionsDir)
	}
	type cand struct {
		path string
		t    time.Time
	}
	var c []cand
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, _ := e.Info()
		c = append(c, cand{filepath.Join(sessionsDir, e.Name()), info.ModTime()})
	}
	if len(c) == 0 {
		return "", "", "", errors.New("no sessions found")
	}
	sort.Slice(c, func(i, j int) bool { return c[i].t.After(c[j].t) })

	for _, cc := range c {
		raw, err := os.ReadFile(cc.path)
		if err != nil {
			continue
		}
		var s struct {
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		if override != "" && s.SessionID != override {
			continue
		}
		// Find the JSONL for this sessionId
		encoded := encodeCWD(s.CWD)
		jsonl := filepath.Join(claudeDir, "projects", encoded, s.SessionID+".jsonl")
		if _, err := os.Stat(jsonl); err == nil {
			return s.SessionID, s.CWD, jsonl, nil
		}
	}
	return "", "", "", errors.New("could not locate transcript JSONL for any active session")
}

// encodeCWD turns "/Users/rohitkumar/Desktop/ghostnode" into
// "-Users-rohitkumar-Desktop-ghostnode" — Claude Code's encoding.
func encodeCWD(cwd string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-")
	return r.Replace(cwd)
}

func projectMemoryDir(cwd string) string {
	claude, _ := osdetect.ClaudeDir()
	return filepath.Join(claude, "projects", encodeCWD(cwd), "memory")
}

type resolvedConfig struct {
	relayBase    string
	apiKey       string
	kintsugiKey  string
}

// ResolvedCLIConfig is the public version of resolvedConfig used by commands
// that don't need encryption (sessions ls/checkpoints).
type ResolvedCLIConfig struct {
	RelayBase string
	APIKey    string
}

// ResolveEnvForCLI is the lightweight wrapper used by `sessions ls` and
// similar read-only commands that don't need KINTSUGI_KEY.
func ResolveEnvForCLI() (ResolvedCLIConfig, error) {
	baseURL := os.Getenv("BIFROST_URL")
	apiKey := os.Getenv("BIFROST_API_KEY")
	if baseURL == "" {
		return ResolvedCLIConfig{}, errors.New("BIFROST_URL is not set")
	}
	relay := os.Getenv("KINTSUGI_URL")
	if relay == "" {
		relay = strings.TrimSuffix(baseURL, "/mcp")
		relay = strings.TrimSuffix(relay, "/")
		relay = strings.Replace(relay, ":8443", ":8444", 1)
	}
	return ResolvedCLIConfig{RelayBase: relay, APIKey: apiKey}, nil
}

// resolveEnv reads BIFROST_URL / BIFROST_API_KEY / KINTSUGI_KEY from env (or
// from the provided overrides), validates them, and converts BIFROST_URL into
// a kintsugi relay base URL by stripping any /mcp path.
func resolveEnv(baseURL, apiKey, kkey string) (resolvedConfig, error) {
	if baseURL == "" {
		baseURL = os.Getenv("BIFROST_URL")
	}
	if apiKey == "" {
		apiKey = os.Getenv("BIFROST_API_KEY")
	}
	if kkey == "" {
		kkey = os.Getenv("KINTSUGI_KEY")
	}
	if baseURL == "" {
		return resolvedConfig{}, errors.New("BIFROST_URL is not set (handoff needs the VPS endpoint)")
	}
	if kkey == "" {
		return resolvedConfig{}, errors.New("KINTSUGI_KEY is not set (run yashigatakae init --vps to generate one and propagate via secrets.env)")
	}
	// BIFROST_URL is like http://vps:8443/mcp — strip the /mcp; relay is on a
	// different port (8444) but reuses the host. For now require explicit
	// KINTSUGI_URL override OR derive by swapping :8443 → :8444 on default.
	relay := os.Getenv("KINTSUGI_URL")
	if relay == "" {
		relay = strings.TrimSuffix(baseURL, "/mcp")
		relay = strings.TrimSuffix(relay, "/")
		relay = strings.Replace(relay, ":8443", ":8444", 1)
	}
	return resolvedConfig{relayBase: relay, apiKey: apiKey, kintsugiKey: kkey}, nil
}
