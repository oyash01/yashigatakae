package kintsugi

import (
	"context"
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

// RelayConfig is what `yashigatakae kintsugi serve` reads.
type RelayConfig struct {
	Listen string // ":8444"
	APIKey string // require Bearer (empty = unauthenticated, only safe behind a firewall)
	DataDir string // override storage root; default = ~/.yashigatakae/kintsugi
}

// ServeRelay runs the kintsugi relay HTTP server. Blocks until ctx is cancelled.
//
// Endpoints:
//   GET  /health
//   GET  /kintsugi/sessions                              → JSON array of session ids
//   GET  /kintsugi/sessions/{sid}/checkpoints            → JSON array of {ts, machine, size}
//   POST /kintsugi/sessions/{sid}/checkpoints            → body is the checkpoint blob
//   GET  /kintsugi/sessions/{sid}/checkpoints/latest     → returns the latest blob
//   GET  /kintsugi/sessions/{sid}/checkpoints/{ts}       → specific blob (for rollback)
//   DELETE /kintsugi/sessions/{sid}                      → wipe a session entirely
//
// Headers used on POST:
//   X-Yashi-Machine: <machine label>
//   Content-Type:    application/octet-stream
//
// Storage layout:
//   {dataDir}/{sid}/{ts-rfc3339nano}-{machine}.bin
func ServeRelay(ctx context.Context, cfg RelayConfig) error {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8444"
	}
	if cfg.DataDir == "" {
		yash, err := osdetect.YashigatakaeDir()
		if err != nil {
			return err
		}
		cfg.DataDir = filepath.Join(yash, "kintsugi")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}

	r := &relay{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/kintsugi/admin/diskfree", r.gateAuth(r.handleDiskfree))
	mux.HandleFunc("/kintsugi/", r.gateAuth(r.handleKintsugi))

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()
	fmt.Printf("kintsugi relay listening on http://%s/kintsugi/* (data: %s)\n", cfg.Listen, cfg.DataDir)

	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdown)
	case err := <-errCh:
		return err
	}
}

type relay struct {
	cfg RelayConfig
}

func (r *relay) gateAuth(h http.HandlerFunc) http.HandlerFunc {
	if r.cfg.APIKey == "" {
		return h
	}
	expected := "Bearer " + r.cfg.APIKey
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, req)
	}
}

func (r *relay) handleKintsugi(w http.ResponseWriter, req *http.Request) {
	// Parse: /kintsugi/sessions[/<sid>[/checkpoints[/<ts|"latest">]]]
	path := strings.TrimPrefix(req.URL.Path, "/kintsugi/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] != "sessions" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	switch {
	case len(parts) == 1: // /kintsugi/sessions
		if req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.listSessions(w)
	case len(parts) == 2: // /kintsugi/sessions/{sid}
		switch req.Method {
		case http.MethodDelete:
			r.deleteSession(w, parts[1])
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 3 && parts[2] == "checkpoints":
		switch req.Method {
		case http.MethodGet:
			r.listCheckpoints(w, parts[1])
		case http.MethodPost:
			r.postCheckpoint(w, req, parts[1])
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 4 && parts[2] == "checkpoints":
		if req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.getCheckpoint(w, parts[1], parts[3])
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// handleDiskfree returns disk-space stats for the relay's data dir, used by
// the backfill client to decide whether it's safe to push ~1 GB of archive.
func (r *relay) handleDiskfree(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	free, total, err := diskFree(r.cfg.DataDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data_dir":    r.cfg.DataDir,
		"free_bytes":  free,
		"total_bytes": total,
	})
}

func (r *relay) listSessions(w http.ResponseWriter) {
	entries, err := os.ReadDir(r.cfg.DataDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (r *relay) listCheckpoints(w http.ResponseWriter, sid string) {
	dir := filepath.Join(r.cfg.DataDir, sid)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type Checkpoint struct {
		TS      string `json:"ts"`
		Machine string `json:"machine"`
		Size    int64  `json:"size"`
		File    string `json:"file"`
	}
	out := []Checkpoint{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".bin") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".bin")
		// stem = "{ts}-{machine}". TS may contain colons; machine is whatever's after the last hyphen-not-in-ts. We split on the LAST '-' before a non-digit.
		// Simpler: split on the last "-" — works because we URL-encode machine names to alphanum.
		idx := strings.LastIndex(stem, "-")
		if idx < 0 {
			continue
		}
		info, _ := e.Info()
		out = append(out, Checkpoint{
			TS:      stem[:idx],
			Machine: stem[idx+1:],
			Size:    info.Size(),
			File:    e.Name(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (r *relay) postCheckpoint(w http.ResponseWriter, req *http.Request, sid string) {
	if !validSid(sid) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}
	machine := sanitizeLabel(req.Header.Get("X-Yashi-Machine"))
	if machine == "" {
		machine = "unknown"
	}
	dir := filepath.Join(r.cfg.DataDir, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ts := time.Now().UTC().Format("20060102T150405.000000000Z")
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.bin", ts, machine))
	f, err := os.Create(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	n, err := io.Copy(f, req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ts":      ts,
		"machine": machine,
		"size":    n,
		"file":    filepath.Base(path),
	})
}

func (r *relay) getCheckpoint(w http.ResponseWriter, sid, target string) {
	if !validSid(sid) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}
	dir := filepath.Join(r.cfg.DataDir, sid)
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var bins []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".bin") {
			bins = append(bins, e.Name())
		}
	}
	if len(bins) == 0 {
		http.Error(w, "no checkpoints", http.StatusNotFound)
		return
	}
	sort.Strings(bins)
	var pick string
	if target == "latest" {
		pick = bins[len(bins)-1]
	} else {
		// match by ts prefix
		for _, b := range bins {
			if strings.HasPrefix(b, target) {
				pick = b
				break
			}
		}
	}
	if pick == "" {
		http.Error(w, "checkpoint not found", http.StatusNotFound)
		return
	}
	full := filepath.Join(dir, pick)
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Yashi-Checkpoint", pick)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	_, _ = io.Copy(w, f)
}

func (r *relay) deleteSession(w http.ResponseWriter, sid string) {
	if !validSid(sid) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}
	dir := filepath.Join(r.cfg.DataDir, sid)
	if err := os.RemoveAll(dir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validSid keeps the relay safe from path traversal. Allow alphanumerics,
// hyphens, underscores, and uuids — block everything else.
func validSid(sid string) bool {
	if sid == "" || len(sid) > 128 {
		return false
	}
	for _, r := range sid {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

func sanitizeLabel(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			sb.WriteRune(r)
		}
		if sb.Len() >= 32 {
			break
		}
	}
	return sb.String()
}

