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
  yashigatakae hermes ls       [--status pending|running|done|failed|cancelled] [--limit N]
  yashigatakae hermes logs <id>
  yashigatakae hermes cancel <id>
  yashigatakae hermes serve    [--poll DURATION] [--claude PATH]

Tasks run claude in non-interactive mode (` + "`claude -p \"<prompt>\"`" + `); after each
run, the captured output is written to mempalace tagged "lesson,hermes". Future
sessions on any machine call mempalace_recall and see what hermes learned.`
}

// Enqueue is the CLI entry for `yashigatakae hermes enqueue`.
func Enqueue(ctx context.Context, t Task) (int64, error) {
	store, err := Open()
	if err != nil {
		return 0, err
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
