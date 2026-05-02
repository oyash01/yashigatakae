package hermes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/oyash01/yashigatakae/internal/mempalace"
	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// WorkerOptions tune the worker loop.
type WorkerOptions struct {
	PollInterval time.Duration // default 5s
	ClaudeBin    string        // default "claude"
	ExtraArgs    []string      // appended to claude invocation (e.g. --output-format=stream-json)
	WriteLessons bool          // default true — distill final response into mempalace as a "lesson" entry
}

// Worker runs the queue loop until ctx is cancelled.
type Worker struct {
	Store *Store
	Opts  WorkerOptions
}

// Run blocks. Polls the queue, runs each pending task synchronously, and
// distills the final response to mempalace. Doesn't parallelize — running
// multiple Claude sessions on the same machine fights for tokens + state.
func (w *Worker) Run(ctx context.Context) error {
	if w.Opts.PollInterval <= 0 {
		w.Opts.PollInterval = 5 * time.Second
	}
	if w.Opts.ClaudeBin == "" {
		w.Opts.ClaudeBin = "claude"
	}
	fmt.Printf("hermes worker: polling every %s, claude=%s\n", w.Opts.PollInterval, w.Opts.ClaudeBin)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		t, err := w.Store.ClaimNext(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(w.Opts.PollInterval):
					continue
				}
			}
			return err
		}
		w.runTask(ctx, t)
	}
}

func (w *Worker) runTask(ctx context.Context, t Task) {
	yash, _ := osdetect.YashigatakaeDir()
	logPath := filepath.Join(yash, "hermes", "logs", fmt.Sprintf("%d.log", t.ID))
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		_ = w.Store.Finish(ctx, t.ID, StatusFailed, 1, "")
		return
	}
	defer logFile.Close()

	fmt.Fprintf(logFile, "# hermes task %d — project=%s\n", t.ID, t.Project)
	fmt.Fprintf(logFile, "# started %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(logFile, "# prompt:\n%s\n\n", t.Prompt)

	args := append([]string{"-p", t.Prompt}, w.Opts.ExtraArgs...)
	cmd := exec.CommandContext(ctx, w.Opts.ClaudeBin, args...)
	if t.CWD != "" {
		cmd.Dir = t.CWD
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	// Tee output to log + an in-memory buffer so we can extract the final
	// assistant text for the lesson distillation step.
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

	err = cmd.Start()
	if err != nil {
		fmt.Fprintf(logFile, "# start error: %v\n", err)
		_ = w.Store.Finish(ctx, t.ID, StatusFailed, 1, logPath)
		return
	}
	err = cmd.Wait()
	pw.Close()
	exit := cmd.ProcessState.ExitCode()
	finalStatus := StatusDone
	if err != nil || exit != 0 {
		finalStatus = StatusFailed
	}

	// Optionally distill the captured final output into mempalace as a lesson.
	if w.Opts.WriteLessons && finalStatus == StatusDone {
		body := fmt.Sprintf("hermes task #%d (project=%s)\nprompt: %s\n\n--- final output ---\n%s",
			t.ID, t.Project, t.Prompt, captured.String())
		_, _ = mempalace.Remember(ctx, mempalace.RememberOptions{
			Body:    body,
			Source:  "hermes",
			Project: t.Project,
			Tags:    "lesson,hermes,task:" + fmt.Sprintf("%d", t.ID),
		})
	}

	_ = w.Store.Finish(ctx, t.ID, finalStatus, exit, logPath)
	fmt.Fprintf(logFile, "\n# finished %s status=%s exit=%d\n", time.Now().UTC().Format(time.RFC3339), finalStatus, exit)
}

// ringBuffer keeps the last N bytes written, useful for capturing the tail of
// a long stream without holding the whole thing in memory.
type ringBuffer struct {
	Cap  int
	buf  []byte
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.Cap {
		r.buf = r.buf[len(r.buf)-r.Cap:]
	}
	return len(p), nil
}

func (r *ringBuffer) String() string { return string(r.buf) }
