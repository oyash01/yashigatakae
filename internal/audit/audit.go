// Package audit centralizes the per-request structured logging that gets
// wired in front of every public HTTP endpoint (bifrost MCP gateway,
// kintsugi relay). One JSONL line per request, machine-readable for
// fail2ban / observability tools.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// Entry is one line in the audit log.
type Entry struct {
	TS         string `json:"ts"`
	Service    string `json:"service"` // "bifrost" | "kintsugi"
	IP         string `json:"ip"`
	KeyIDSHA8  string `json:"key_id_sha8,omitempty"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status"`
	LatencyMS  int64  `json:"latency_ms"`
	BytesIn    int64  `json:"bytes_in,omitempty"`
	BytesOut   int64  `json:"bytes_out,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
}

// Writer is a thread-safe JSONL appender. Default path:
//   /var/log/yashigatakae/audit.log on Linux when writable
//   ~/.yashigatakae/audit.log otherwise (dev / Mac)
type Writer struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

var defaultWriter *Writer

// Default returns a process-wide singleton Writer. Lazy-initialized on first
// call. Failures fall back to a stderr-only logger that the middleware will
// still call without crashing the handler.
func Default() *Writer {
	if defaultWriter != nil {
		return defaultWriter
	}
	w, err := openWriter()
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit: opening log failed (%v); falling back to stderr only\n", err)
		w = &Writer{} // f nil; entries go to stderr
	}
	defaultWriter = w
	return w
}

func openWriter() (*Writer, error) {
	candidates := []string{"/var/log/yashigatakae/audit.log"}
	if yash, err := osdetect.YashigatakaeDir(); err == nil {
		candidates = append(candidates, filepath.Join(yash, "audit.log"))
	}
	var lastErr error
	for _, p := range candidates {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			lastErr = err
			continue
		}
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
		if err != nil {
			lastErr = err
			continue
		}
		return &Writer{f: f, path: p}, nil
	}
	return nil, lastErr
}

// Path returns the on-disk audit log path (empty if writer is stderr-only).
func (w *Writer) Path() string { return w.path }

// Write appends one Entry as a JSONL line. Never returns error to caller —
// audit failures must not break user-facing requests.
func (w *Writer) Write(e Entry) {
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	b = append(b, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f != nil {
		_, _ = w.f.Write(b)
	} else {
		_, _ = os.Stderr.Write(b)
	}
}

// KeyIDFromBearer reduces a Bearer token to a stable 8-hex-char identifier
// for log correlation without leaking the secret. Uses sha256 prefix.
func KeyIDFromBearer(authHeader string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return ""
	}
	tok := strings.TrimSpace(authHeader[len(prefix):])
	if tok == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:4])
}

// ClientIP returns the originating IP for an HTTP request, honoring
// X-Forwarded-For (first hop) and X-Real-IP set by reverse proxies.
func ClientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i > 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return strings.TrimSpace(v)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ────────────────────────────────────────────────────────────────────────────
// Middleware
// ────────────────────────────────────────────────────────────────────────────

// statusRecorder captures status + bytes-out for the audit entry.
type statusRecorder struct {
	http.ResponseWriter
	status   int
	bytesOut int64
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytesOut += int64(n)
	return n, err
}

// Middleware wraps an http.Handler so every request is logged. service is the
// label written into Entry.Service ("bifrost" or "kintsugi").
func Middleware(service string, h http.Handler) http.Handler {
	w := Default()
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: rw}
		h.ServeHTTP(rec, r)
		w.Write(Entry{
			Service:   service,
			IP:        ClientIP(r),
			KeyIDSHA8: KeyIDFromBearer(r.Header.Get("Authorization")),
			Method:    r.Method,
			Path:      r.URL.Path,
			Status:    rec.status,
			LatencyMS: time.Since(start).Milliseconds(),
			BytesIn:   r.ContentLength,
			BytesOut:  rec.bytesOut,
			UserAgent: r.UserAgent(),
		})
	})
}
