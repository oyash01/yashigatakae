package audit

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// LimitConfig captures default per-key throttle rates.
type LimitConfig struct {
	RequestsPerMinute int           // bucket capacity per key
	BurstFactor       int           // bucket size = RequestsPerMinute * BurstFactor / 60 (allow short bursts)
	UnauthRPM         int           // separate rate for clients without an API key (typically lower)
	GCInterval        time.Duration // sweep idle buckets older than this every GCInterval
	IdleAfter         time.Duration // mark a bucket idle after no use for this long
}

// DefaultBifrost = 200 req/min/key, kintsugi POST = 60/min, GET = 600/min.
// Picked because Claude Code tools fire 5–10/sec at peak (= 300–600/min),
// so per-key bifrost limit must be near the upper bound for legitimate use,
// while kintsugi POST (handoff) is human-paced and tighter.
func DefaultBifrostLimits() LimitConfig {
	return LimitConfig{RequestsPerMinute: 200, BurstFactor: 4, UnauthRPM: 60, GCInterval: 5 * time.Minute, IdleAfter: 10 * time.Minute}
}

func DefaultKintsugiPOSTLimits() LimitConfig {
	return LimitConfig{RequestsPerMinute: 60, BurstFactor: 4, UnauthRPM: 10, GCInterval: 5 * time.Minute, IdleAfter: 10 * time.Minute}
}

func DefaultKintsugiGETLimits() LimitConfig {
	return LimitConfig{RequestsPerMinute: 600, BurstFactor: 2, UnauthRPM: 60, GCInterval: 5 * time.Minute, IdleAfter: 10 * time.Minute}
}

// Limiter is a simple token-bucket per key (key_id_sha8 from the Authorization
// header, falling back to the client IP if no auth). Goroutine-safe.
type Limiter struct {
	cfg    LimitConfig
	mu     sync.Mutex
	buckets map[string]*bucket
	stop    chan struct{}
}

type bucket struct {
	tokens  float64
	last    time.Time
	capRPM  int // remembered per-key cap (auth vs unauth)
}

// NewLimiter spins up the bucket map and a background GC goroutine that
// reaps idle buckets.
func NewLimiter(cfg LimitConfig) *Limiter {
	if cfg.RequestsPerMinute <= 0 {
		cfg.RequestsPerMinute = 60
	}
	if cfg.BurstFactor <= 0 {
		cfg.BurstFactor = 2
	}
	if cfg.UnauthRPM <= 0 {
		cfg.UnauthRPM = cfg.RequestsPerMinute / 4
	}
	if cfg.GCInterval <= 0 {
		cfg.GCInterval = 5 * time.Minute
	}
	if cfg.IdleAfter <= 0 {
		cfg.IdleAfter = 10 * time.Minute
	}
	l := &Limiter{
		cfg:     cfg,
		buckets: map[string]*bucket{},
		stop:    make(chan struct{}),
	}
	go l.gcLoop()
	return l
}

// Stop ends the background GC goroutine.
func (l *Limiter) Stop() { close(l.stop) }

// Allow returns true if the key is under its budget; false otherwise. The
// caller maps "key" to whatever scoping makes sense (key_id_sha8 typically;
// client IP when unauth).
func (l *Limiter) Allow(key string, authed bool) bool {
	cap := l.cfg.RequestsPerMinute
	if !authed {
		cap = l.cfg.UnauthRPM
	}
	burst := float64(cap*l.cfg.BurstFactor) / 60.0 // tokens-per-second
	burstCap := float64(cap)                       // bucket size = 1 minute of tokens

	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	now := time.Now()
	if b == nil {
		b = &bucket{tokens: burstCap, last: now, capRPM: cap}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += burst * elapsed
	if b.tokens > burstCap {
		b.tokens = burstCap
	}
	b.last = now
	if b.tokens < 1.0 {
		return false
	}
	b.tokens--
	return true
}

func (l *Limiter) gcLoop() {
	t := time.NewTicker(l.cfg.GCInterval)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			cutoff := time.Now().Add(-l.cfg.IdleAfter)
			l.mu.Lock()
			for k, b := range l.buckets {
				if b.last.Before(cutoff) {
					delete(l.buckets, k)
				}
			}
			l.mu.Unlock()
		}
	}
}

// HTTPMiddleware wraps an http.Handler with this rate limiter. On block,
// returns 429 + Retry-After=60.
func (l *Limiter) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		key := KeyIDFromBearer(auth)
		authed := key != ""
		if !authed {
			key = "ip:" + ClientIP(r)
		}
		if !l.Allow(key, authed) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Format human-readable description of the limiter (for `bifrost serve` startup print).
func (l *Limiter) String() string {
	return fmt.Sprintf("rate-limit: authed=%d/min unauth=%d/min burst×%d",
		l.cfg.RequestsPerMinute, l.cfg.UnauthRPM, l.cfg.BurstFactor)
}
