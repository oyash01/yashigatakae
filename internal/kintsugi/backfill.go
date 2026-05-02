package kintsugi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// BackfillOptions captures CLI flags for `yashigatakae backfill`.
type BackfillOptions struct {
	BaseURL     string
	APIKey      string
	KintsugiKey string

	DryRun      bool          // pack + size + report only; do NOT upload
	Limit       int           // upload at most N transcripts (0 = unlimited)
	Since       time.Duration // only consider transcripts mtime within this window (0 = all)
	Force       bool          // re-upload even if ledger says it's already there
	SkipDiskCheck bool        // bypass relay disk-free pre-check (not recommended)
}

// BackfillReport summarizes a backfill run.
type BackfillReport struct {
	Scanned    int   // transcripts visited
	Uploaded   int   // transcripts uploaded this run
	Skipped    int   // transcripts skipped (already in ledger or filtered)
	Failed     int   // transcripts that failed
	BytesIn    int64 // total plaintext bytes packed
	BytesOut   int64 // total ciphertext bytes uploaded
	StartedAt  time.Time
	FinishedAt time.Time
}

// LedgerEntry records one successful backfill upload so re-runs skip it.
type LedgerEntry struct {
	TranscriptPath  string `json:"transcript_path"`
	SessionID       string `json:"session_id"`
	SourceCWD       string `json:"source_cwd"`
	SizeLocal       int64  `json:"size_local"`
	MtimeNanos      int64  `json:"mtime_nanos"`
	SHA256          string `json:"sha256"`
	UploadedAt      string `json:"uploaded_at"`
	CiphertextBytes int    `json:"ciphertext_bytes"`
}

// Ledger is the on-disk record of past uploads.
type Ledger struct {
	Entries []LedgerEntry `json:"entries"`
}

// Backfill walks ~/.claude/projects/<encoded-cwd>/<uuid>.jsonl and uploads each
// past session to the kintsugi relay. Idempotent: a ledger at
// ~/.yashigatakae/backfill.json prevents re-uploading unchanged transcripts.
func Backfill(ctx context.Context, opts BackfillOptions) (BackfillReport, error) {
	rep := BackfillReport{StartedAt: time.Now().UTC()}
	resolved, err := resolveEnv(opts.BaseURL, opts.APIKey, opts.KintsugiKey)
	if err != nil {
		return rep, err
	}
	client := NewClient(resolved.relayBase, resolved.apiKey)

	// 1. Find all transcripts.
	transcripts, err := scanArchive(opts.Since)
	if err != nil {
		return rep, err
	}
	rep.Scanned = len(transcripts)
	if rep.Scanned == 0 {
		fmt.Println("  (no transcripts found in ~/.claude/projects/)")
		rep.FinishedAt = time.Now().UTC()
		return rep, nil
	}

	// 2. Load ledger.
	ledger := loadLedger()
	known := map[string]LedgerEntry{}
	for _, e := range ledger.Entries {
		known[e.TranscriptPath] = e
	}

	// 3. Filter against ledger + estimate disk impact.
	var todo []transcriptInfo
	var projectedBytes int64
	for _, t := range transcripts {
		if !opts.Force {
			if e, ok := known[t.Path]; ok && e.SizeLocal == t.Size && e.MtimeNanos == t.Mtime.UnixNano() {
				rep.Skipped++
				continue
			}
		}
		todo = append(todo, t)
		projectedBytes += t.Size
	}
	if len(todo) == 0 {
		fmt.Printf("  · scanned=%d already-uploaded=%d nothing to do\n", rep.Scanned, rep.Skipped)
		rep.FinishedAt = time.Now().UTC()
		return rep, nil
	}
	if opts.Limit > 0 && len(todo) > opts.Limit {
		todo = todo[:opts.Limit]
	}

	fmt.Printf("  · scanned=%d already-uploaded=%d to-upload=%d projected=%s\n",
		rep.Scanned, rep.Skipped, len(todo), humanBytes(projectedBytes))

	// 4. Disk-space sanity check.
	if !opts.SkipDiskCheck {
		if free, total, err := relayDiskFree(ctx, client); err == nil {
			fmt.Printf("  · relay free=%s total=%s\n", humanBytes(int64(free)), humanBytes(int64(total)))
			if int64(free) < projectedBytes*120/100 {
				return rep, fmt.Errorf(
					"relay disk free (%s) is less than 1.2× projected upload (%s); pass --skip-disk-check to override",
					humanBytes(int64(free)), humanBytes(projectedBytes*120/100))
			}
		}
	}

	if opts.DryRun {
		for _, t := range todo {
			fmt.Printf("    would upload: %s (size=%s sid=%s)\n", t.Path, humanBytes(t.Size), t.SessionID)
		}
		rep.FinishedAt = time.Now().UTC()
		return rep, nil
	}

	// 5. Pack + encrypt + upload + ledger-update one by one.
	for i, t := range todo {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		fmt.Printf("  [%d/%d] %s ", i+1, len(todo), filepath.Base(t.Path))
		entry, err := packAndUpload(ctx, t, resolved.kintsugiKey, client)
		if err != nil {
			fmt.Printf("FAIL: %v\n", err)
			rep.Failed++
			continue
		}
		fmt.Printf("✓ ciphertext=%s\n", humanBytes(int64(entry.CiphertextBytes)))
		ledger.Entries = append(ledger.Entries, entry)
		rep.Uploaded++
		rep.BytesIn += t.Size
		rep.BytesOut += int64(entry.CiphertextBytes)
		// Persist ledger after every successful upload — interrupted runs resume cleanly.
		if err := saveLedger(ledger); err != nil {
			fmt.Printf("    ! ledger save failed: %v\n", err)
		}
	}
	rep.FinishedAt = time.Now().UTC()
	return rep, nil
}

// transcriptInfo is one row produced by scanArchive.
type transcriptInfo struct {
	Path       string
	Size       int64
	Mtime      time.Time
	SessionID  string
	SourceCWD  string // decoded from the encoded-cwd dir name
	ProjectDir string // ~/.claude/projects/<encoded-cwd>
}

// scanArchive walks ~/.claude/projects/<encoded-cwd>/<uuid>.jsonl and returns one
// transcriptInfo per file. since != 0 filters by mtime.
func scanArchive(since time.Duration) ([]transcriptInfo, error) {
	claude, err := osdetect.ClaudeDir()
	if err != nil {
		return nil, err
	}
	projectsRoot := filepath.Join(claude, "projects")
	entries, err := os.ReadDir(projectsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	cutoff := time.Time{}
	if since > 0 {
		cutoff = time.Now().Add(-since)
	}
	var out []transcriptInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		projDir := filepath.Join(projectsRoot, e.Name())
		jsonls, err := filepath.Glob(filepath.Join(projDir, "*.jsonl"))
		if err != nil {
			continue
		}
		for _, p := range jsonls {
			info, err := os.Stat(p)
			if err != nil || info.IsDir() {
				continue
			}
			if !cutoff.IsZero() && info.ModTime().Before(cutoff) {
				continue
			}
			sid := strings.TrimSuffix(filepath.Base(p), ".jsonl")
			out = append(out, transcriptInfo{
				Path:       p,
				Size:       info.Size(),
				Mtime:      info.ModTime(),
				SessionID:  sid,
				SourceCWD:  decodeCWD(e.Name()),
				ProjectDir: projDir,
			})
		}
	}
	// Newest first so a --limit run upholds recency.
	sort.Slice(out, func(i, j int) bool { return out[i].Mtime.After(out[j].Mtime) })
	return out, nil
}

// decodeCWD reverses the encoded "-Users-rohit-Desktop-foo" → "/Users/rohit/Desktop/foo".
// Best-effort: there's no perfect round-trip because the encoding is lossy
// across separator collisions, but the common case (Unix paths) is exact.
func decodeCWD(encoded string) string {
	if !strings.HasPrefix(encoded, "-") {
		return encoded
	}
	return strings.ReplaceAll(encoded, "-", "/")
}

func packAndUpload(ctx context.Context, t transcriptInfo, key string, client *Client) (LedgerEntry, error) {
	pack := PackOptions{
		SessionID:      t.SessionID,
		TranscriptFile: t.Path,
		SourceCWD:      t.SourceCWD,
		SourceKind:     "archive",
		MemoryDir:      filepath.Join(t.ProjectDir, "memory"),
		MetaMemoryFile: filepath.Join(t.ProjectDir, "MEMORY.md"),
		SubagentFiles:  collectSubagents(t.SessionID, t.SourceCWD),
		TodoFiles:      collectTodos(t.SessionID),
	}
	body, mf, err := Pack(pack)
	if err != nil {
		return LedgerEntry{}, err
	}
	cipher, err := Encrypt(body, key)
	if err != nil {
		return LedgerEntry{}, err
	}
	if err := client.PostCheckpoint(ctx, t.SessionID, mf.SourceMachine, cipher); err != nil {
		return LedgerEntry{}, err
	}
	sum := sha256.Sum256(body)
	return LedgerEntry{
		TranscriptPath:  t.Path,
		SessionID:       t.SessionID,
		SourceCWD:       t.SourceCWD,
		SizeLocal:       t.Size,
		MtimeNanos:      t.Mtime.UnixNano(),
		SHA256:          hex.EncodeToString(sum[:]),
		UploadedAt:      time.Now().UTC().Format(time.RFC3339),
		CiphertextBytes: len(cipher),
	}, nil
}

// ledger I/O ────────────────────────────────────────────────────────────────

func ledgerPath() string {
	yash, _ := osdetect.YashigatakaeDir()
	return filepath.Join(yash, "backfill.json")
}

func loadLedger() Ledger {
	b, err := os.ReadFile(ledgerPath())
	if err != nil {
		return Ledger{}
	}
	var l Ledger
	_ = json.Unmarshal(b, &l)
	return l
}

func saveLedger(l Ledger) error {
	yash, _ := osdetect.YashigatakaeDir()
	if err := os.MkdirAll(yash, 0o755); err != nil {
		return err
	}
	tmp := ledgerPath() + ".tmp"
	b, _ := json.MarshalIndent(l, "", "  ")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, ledgerPath())
}

// relayDiskFree fetches the relay's data-dir disk stats. Returns 0,0,err if the
// relay is older than v0.8 (admin endpoint missing).
func relayDiskFree(ctx context.Context, client *Client) (uint64, uint64, error) {
	url := strings.TrimSuffix(client.BaseURL, "/kintsugi") + "/kintsugi/admin/diskfree"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if client.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+client.APIKey)
	}
	resp, err := client.HTTP.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var d struct {
		Free  uint64 `json:"free_bytes"`
		Total uint64 `json:"total_bytes"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return 0, 0, err
	}
	return d.Free, d.Total, nil
}

// humanBytes is shared with the CLI; tiny copy to avoid an internal/-graphify import.
func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1fMB", float64(n)/1024/1024)
	default:
		return fmt.Sprintf("%.2fGB", float64(n)/1024/1024/1024)
	}
}
