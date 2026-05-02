// Package hermes is the self-learning background agent. Pulls queued tasks
// from a sqlite queue, executes them via `claude -p`, distills the final
// output into mempalace as a "lesson" entry future sessions can recall.
package hermes

import (
	"context"
	"fmt"
	"io"
	"os"
)

func Help() string {
	return `hermes — self-learning background agent.

  yashigatakae hermes enqueue --project X --prompt "..."  [--cwd DIR] [--note T]
                              [--priority N] [--idempotency-key K]
                              [--max-retries N] [--depends-on ID]
  yashigatakae hermes ls       [--status pending|running|done|failed|cancelled|dlq|scheduled] [--limit N]
  yashigatakae hermes logs <id>
  yashigatakae hermes cancel <id>
  yashigatakae hermes serve    [--poll DURATION] [--claude PATH] [--concurrency N]
  yashigatakae hermes dlq ls
  yashigatakae hermes dlq retry <id>
  yashigatakae hermes schedule add "*/30 * * * *" --project X --prompt "..."
  yashigatakae hermes schedule ls
  yashigatakae hermes schedule rm <id>

Tasks run claude in non-interactive mode (` + "`claude -p \"<prompt>\"`" + `); after each
run, the captured output is written to mempalace tagged "lesson,hermes". Future
sessions on any machine call mempalace_recall and see what hermes learned.

Idempotency keys dedupe within a 7-day window. Failed tasks retry with
exponential backoff (30s × 10^attempt, capped at 8h, max 5 attempts). After
exhausting retries, tasks land in the DLQ for inspection / manual replay.`
}

// Enqueue is the CLI entry for ` + "`yashigatakae hermes enqueue`" + `. Returns the task id
// and a hit flag (true if an existing task was matched on idempotency_key).
func Enqueue(ctx context.Context, t Task) (int64, bool, error) {
	store, err := Open()
	if err != nil {
		return 0, false, err
	}
	defer store.Close()
	return store.Enqueue(ctx, t)
}

// List is the CLI entry for `yashigatakae hermes ls`.
func List(ctx context.Context, status string, limit int) ([]Task, error) {
	store, err := Open()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.List(ctx, status, limit)
}

// Cancel is the CLI entry for `yashigatakae hermes cancel`.
func Cancel(ctx context.Context, id int64) (bool, error) {
	store, err := Open()
	if err != nil {
		return false, err
	}
	defer store.Close()
	return store.Cancel(ctx, id)
}

// RetryDLQ moves a DLQ task back to pending.
func RetryDLQ(ctx context.Context, id int64) error {
	store, err := Open()
	if err != nil {
		return err
	}
	defer store.Close()
	return store.RetryDLQ(ctx, id)
}

// AddSchedule inserts a cron row.
func AddSchedule(ctx context.Context, sc Schedule) (int64, error) {
	store, err := Open()
	if err != nil {
		return 0, err
	}
	defer store.Close()
	return store.AddSchedule(ctx, sc)
}

// ListSchedules returns active cron rows.
func ListSchedules(ctx context.Context) ([]Schedule, error) {
	store, err := Open()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListSchedules(ctx)
}

// DeleteSchedule removes a cron row.
func DeleteSchedule(ctx context.Context, id int64) error {
	store, err := Open()
	if err != nil {
		return err
	}
	defer store.Close()
	return store.DeleteSchedule(ctx, id)
}

// LogsPath returns the on-disk log path for a task.
func LogsPath(ctx context.Context, id int64) (string, error) {
	store, err := Open()
	if err != nil {
		return "", err
	}
	defer store.Close()
	t, err := store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return t.LogPath, nil
}

// TailLogs writes the contents of the task's log file to w.
func TailLogs(ctx context.Context, id int64, w io.Writer) error {
	path, err := LogsPath(ctx, id)
	if err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("task %d has no log yet", id)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// Serve runs the worker loop. Blocks until ctx is cancelled.
func Serve(ctx context.Context, opts WorkerOptions) error {
	store, err := Open()
	if err != nil {
		return err
	}
	defer store.Close()
	w := &Worker{Store: store, Opts: opts}
	return w.Run(ctx)
}
