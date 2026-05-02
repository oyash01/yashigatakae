package caveman

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TruncateOptions controls how a single tool result is shrunk before being
// returned to the model. Bytes is the cap (0 = no cap). KeepLines is the
// number of lines to retain on each end (default 30/30).
type TruncateOptions struct {
	Tool      string
	Bytes     int
	KeepHead  int
	KeepTail  int
	OverflowDir string // where the full output is dumped; "" => $TMPDIR/caveman
}

// TruncateResult is the value returned to the caller. Truncated == false means
// the input was already under the cap (or no cap configured) and Output ==
// input verbatim.
type TruncateResult struct {
	Truncated     bool   `json:"truncated"`
	Output        string `json:"output"`
	OverflowPath  string `json:"overflow_path,omitempty"`
	OriginalBytes int    `json:"original_bytes"`
	OutputBytes   int    `json:"output_bytes"`
}

// Truncate applies the size cap to a tool result. If the result is over the
// cap, it keeps the first KeepHead lines + the last KeepTail lines + a marker
// pointing at the overflow file on disk.
//
// The full output is written to overflowDir/<sha8>.txt so the user (or Claude
// via a follow-up Read) can recover everything.
func Truncate(input string, opts TruncateOptions) (TruncateResult, error) {
	res := TruncateResult{
		Output:        input,
		OriginalBytes: len(input),
		OutputBytes:   len(input),
	}
	if opts.Bytes <= 0 || len(input) <= opts.Bytes {
		return res, nil
	}
	if opts.KeepHead <= 0 {
		opts.KeepHead = 30
	}
	if opts.KeepTail <= 0 {
		opts.KeepTail = 30
	}

	// Spill the full payload first so we have a stable filename to reference.
	dir := opts.OverflowDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "caveman")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	sum := sha256.Sum256([]byte(input))
	hash := hex.EncodeToString(sum[:8])
	overflowPath := filepath.Join(dir, hash+".txt")
	if err := os.WriteFile(overflowPath, []byte(input), 0o644); err != nil {
		return res, err
	}

	lines := strings.Split(input, "\n")
	if len(lines) <= opts.KeepHead+opts.KeepTail {
		// Long single-line case (e.g. minified JSON / logs without newlines):
		// fall back to byte-window slice instead of line slice.
		half := opts.Bytes / 2
		if half < 1024 {
			half = 1024
		}
		head := input[:half]
		tail := input[len(input)-half:]
		out := head +
			fmt.Sprintf("\n…(truncated %d bytes — full output at %s)…\n", len(input)-2*half, overflowPath) +
			tail
		res.Truncated = true
		res.Output = out
		res.OverflowPath = overflowPath
		res.OutputBytes = len(out)
		return res, nil
	}

	headLines := lines[:opts.KeepHead]
	tailLines := lines[len(lines)-opts.KeepTail:]
	dropped := len(lines) - opts.KeepHead - opts.KeepTail

	var b strings.Builder
	for _, l := range headLines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	b.WriteString(fmt.Sprintf("…(truncated %d lines / %d bytes — full output at %s)…\n",
		dropped, res.OriginalBytes-len(strings.Join(headLines, "\n"))-len(strings.Join(tailLines, "\n")), overflowPath))
	for _, l := range tailLines {
		b.WriteString(l)
		b.WriteByte('\n')
	}

	res.Truncated = true
	res.Output = b.String()
	res.OverflowPath = overflowPath
	res.OutputBytes = len(res.Output)
	return res, nil
}

// TruncateForTool is the convenience entry point used by the PreToolUse hook.
// It looks up the per-tool cap from the active config and runs Truncate.
func TruncateForTool(tool, input string) (TruncateResult, error) {
	cfg, err := Load()
	if err != nil {
		return TruncateResult{Output: input}, err
	}
	return Truncate(input, TruncateOptions{
		Tool:  tool,
		Bytes: cfg.CapForTool(tool),
	})
}
