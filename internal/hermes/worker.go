package hermes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/oyash01/yashigatakae/internal/mempalace"
	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// WorkerOptions tune the worker loop.
type WorkerOptions struct {
	PollInterval time.Duration // default 5s
	ClaudeBin    string        // default "claude"
	ExtraArgs    []string      // appended to claude invocation
	WriteLessons bool          // distill final response into mempalace as a "lesson"
	Concurrency  int           // default 1; >1 spins parallel runners
	BaseBackoff  time.Duration // first retry delay (default 30s)
	MaxBackoff   time.Duration // cap on retry delay (default 8h)
}

// Worker runs the queue loop until ctx is cancelled.
type Worker struct {
	Store *Store
	Opts  WorkerOptions
}

// Run blocks. Spawns N goroutines that race for ClaimNext; a separate
// goroutine ticks the cron schedules every minute.
func (w *Worker) Run(ctx context.Context) error {
	if w.Opts.PollInterval <= 0 {
		w.Opts.PollInterval = 5 * time.Second
	}
	if w.Opts.ClaudeBin == "" {
		w.Opts.ClaudeBin = "claude"
	}
	if w.Opts.Concurrency <= 0 {
		w.Opts.Concurrency = 1
	}
	if w.Opts.BaseBackoff <= 0 {
		w.Opts.BaseBackoff = 30 * time.Second
	}
	if w.Opts.MaxBackoff <= 0 {
		w.Opts.MaxBackoff = 8 * time.Hour
	}

	fmt.Printf("hermes worker: polling every %s, concurrency=%d, claude=%s\n",
		w.Opts.PollInterval, w.Opts.Concurrency, w.Opts.ClaudeBin)

	var wg sync.WaitGroup
	for i := 0; i < w.Opts.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			w.runLoop(ctx, workerID)
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.cronLoop(ctx)
	}()
	wg.Wait()
	return ctx.Err()
}

func (w *Worker) runLoop(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		t, err := w.Store.ClaimNext(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(w.Opts.PollInterval):
					continue
				}
			}
			fmt.Fprintf(os.Stderr, "hermes worker[%d]: claim error: %v\n", id, err)
			time.Sleep(w.Opts.PollInterval)
			continue
		}
		w.runTask(ctx, t)
	}
}

// cronLoop fires once a minute, scanning the schedules table for any rows
// whose next fire time has passed since LastFiredAt and enqueuing fresh tasks
// with idempotency_key=schedule:<id>:<unix_minute> so duplicate ticks dedupe.
func (w *Worker) cronLoop(ctx context.Context) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	w.tickSchedules(ctx, time.Now()) // run once on start
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			w.tickSchedules(ctx, now)
		}
	}
}

func (w *Worker) tickSchedules(ctx context.Context, now time.Time) {
	scs, err := w.Store.ListSchedules(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hermes cron: list schedules: %v\n", err)
		return
	}
	for _, sc := range scs {
		if !sc.Active {
			continue
		}
		c, err := ParseCron(sc.Cron)
		if err != nil {
			fmt.Fprintf(os.Stderr, "hermes cron: schedule #%d bad cron %q: %v\n", sc.ID, sc.Cron, err)
			continue
		}
		if !c.Due(now, sc.LastFiredAt) {
			continue
		}
		key := fmt.Sprintf("schedule:%d:%d", sc.ID, now.Unix()/60)
		_, hit, err := w.Store.Enqueue(ctx, Task{
			Project:        sc.Project,
			CWD:            sc.CWD,
			Prompt:         sc.Prompt,
			Note:           "from schedule #" + fmt.Sprintf("%d", sc.ID),
			Priority:       sc.Priority,
			MaxRetries:     sc.MaxRetries,
			IdempotencyKey: key,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "hermes cron: enqueue schedule #%d: %v\n", sc.ID, err)
			continue
		}
		if !hit {
			_ = w.Store.MarkScheduleFired(ctx, sc.ID, now)
		}
	}
}

func (w *Worker) runTask(ctx context.Context, t Task) {
	yash, _ := osdetect.YashigatakaeDir()
	logPath := filepath.Join(yash, "hermes", "logs", fmt.Sprintf("%d.log", t.ID))
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		_, _ = w.Store.FinishWithRetry(ctx, t, fmt.Sprintf("open log: %v", err), "", 1, w.backoff(t.RetryCount))
		return
	}
	defer logFile.Close()

	fmt.Fprintf(logFile, "\n# hermes task %d — project=%s — attempt %d/%d\n",
		t.ID, t.Project, t.RetryCount+1, t.MaxRetries)
	fmt.Fprintf(logFile, "# started %s\n", time.Now().UTC().Format(time.RFC3339))
	if t.RetryCount == 0 {
		fmt.Fprintf(logFile, "# prompt:\n%s\n\n", t.Prompt)
	} else {
		fmt.Fprintf(logFile, "# (retry, prompt unchanged)\n\n")
	}

	args := append([]string{"-p", t.Prompt}, w.Opts.ExtraArgs...)
	cmd := exec.CommandContext(ctx, w.Opts.ClaudeBin, args...)
	if t.CWD != "" {
		cmd.Dir = t.CWD
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	var captured ringBuffer
	captured.Cap = 8192
	go func() {
		defer pr.Close()
		buf := make([]byte, 4096)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				_, _ = logFile.Write(buf[:n])
				captured.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(logFile, "# start error: %v\n", err)
		_, _ = w.Store.FinishWithRetry(ctx, t, "start: "+err.Error(), logPath, 1, w.backoff(t.RetryCount))
		return
	}
	err = cmd.Wait()
	pw.Close()
	exit := cmd.ProcessState.ExitCode()

	if err != nil || exit != 0 {
		reason := fmt.Sprintf("exit=%d", exit)
		if err != nil {
			reason = err.Error()
		}
		newStatus, _ := w.Store.FinishWithRetry(ctx, t, reason, logPath, exit, w.backoff(t.RetryCount))
		fmt.Fprintf(logFile, "\n# finished %s status=%s exit=%d (will %s)\n",
			time.Now().UTC().Format(time.RFC3339), newStatus, exit,
			map[string]string{StatusDLQ: "stay in DLQ", StatusScheduled: "retry after backoff"}[newStatus])
		return
	}

	if w.Opts.WriteLessons {
		body := fmt.Sprintf("hermes task #%d (project=%s)\nprompt: %s\n\n--- final output ---\n%s",
			t.ID, t.Project, t.Prompt, captured.String())
		_, _ = mempalace.Remember(ctx, mempalace.RememberOptions{
			Body:    body,
			Source:  "hermes",
			Project: t.Project,
			Tags:    "lesson,hermes,task:" + fmt.Sprintf("%d", t.ID),
		})
	}

	_ = w.Store.Finish(ctx, t.ID, StatusDone, exit, logPath)
	fmt.Fprintf(logFile, "\n# finished %s status=%s exit=%d\n",
		time.Now().UTC().Format(time.RFC3339), StatusDone, exit)
}

// backoff returns the wait before the next retry. Exponential, capped.
func (w *Worker) backoff(retryCount int) time.Duration {
	mult := math.Pow(10, float64(retryCount)) // 1, 10, 100, 1000, …
	d := time.Duration(float64(w.Opts.BaseBackoff) * mult)
	if d > w.Opts.MaxBackoff || d < 0 {
		d = w.Opts.MaxBackoff
	}
	return d
}

// ringBuffer keeps the last N bytes written.
type ringBuffer struct {
	Cap int
	buf []byte
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.Cap {
		r.buf = r.buf[len(r.buf)-r.Cap:]
	}
	return len(p), nil
}

func (r *ringBuffer) String() string { return string(r.buf) }
