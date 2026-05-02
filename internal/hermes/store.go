package hermes

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// Status values used in the tasks table.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Task is one row of the queue.
type Task struct {
	ID         int64     `json:"id"`
	Project    string    `json:"project"`
	CWD        string    `json:"cwd,omitempty"`
	Prompt     string    `json:"prompt"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	LogPath    string    `json:"log_path,omitempty"`
	ExitCode   int       `json:"exit_code,omitempty"`
	Note       string    `json:"note,omitempty"`
}

// Store is the sqlite-backed queue.
type Store struct {
	db   *sql.DB
	path string
}

// Open returns a Store, creating ~/.yashigatakae/hermes.db if missing.
func Open() (*Store, error) {
	yash, err := osdetect.YashigatakaeDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(yash, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(yash, "hermes", "logs"), 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(yash, "hermes.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

// Path returns the sqlite path.
func (s *Store) Path() string { return s.path }

// Close releases the handle.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS tasks (
  id           INTEGER PRIMARY KEY,
  project      TEXT NOT NULL,
  cwd          TEXT,
  prompt       TEXT NOT NULL,
  status       TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  started_at   TEXT,
  finished_at  TEXT,
  log_path     TEXT,
  exit_code    INTEGER,
  note         TEXT
);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at);
`

// Enqueue inserts a pending task and returns its ID.
func (s *Store) Enqueue(ctx context.Context, t Task) (int64, error) {
	if t.Project == "" {
		return 0, fmt.Errorf("project is required")
	}
	if t.Prompt == "" {
		return 0, fmt.Errorf("prompt is required")
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
INSERT INTO tasks (project, cwd, prompt, status, created_at, note)
VALUES (?, ?, ?, ?, ?, ?)`,
		t.Project, t.CWD, t.Prompt, StatusPending, now.Format(time.RFC3339Nano), t.Note,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ClaimNext atomically picks the oldest pending task and marks it running.
// Returns sql.ErrNoRows if the queue is empty.
func (s *Store) ClaimNext(ctx context.Context) (Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `
SELECT id, project, cwd, prompt, created_at, note
FROM tasks WHERE status = ? ORDER BY id ASC LIMIT 1`, StatusPending)
	var t Task
	var createdAt string
	var cwd, note sql.NullString
	if err := row.Scan(&t.ID, &t.Project, &cwd, &t.Prompt, &createdAt, &note); err != nil {
		return Task{}, err
	}
	t.CWD = cwd.String
	t.Note = note.String
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
UPDATE tasks SET status = ?, started_at = ? WHERE id = ?`,
		StatusRunning, now.Format(time.RFC3339Nano), t.ID); err != nil {
		return Task{}, err
	}
	t.Status = StatusRunning
	t.StartedAt = now
	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	return t, nil
}

// Finish updates a task's status + exit code + log path on completion.
func (s *Store) Finish(ctx context.Context, id int64, status string, exitCode int, logPath string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE tasks SET status = ?, finished_at = ?, exit_code = ?, log_path = ? WHERE id = ?`,
		status, now.Format(time.RFC3339Nano), exitCode, logPath, id)
	return err
}

// Cancel marks a pending or running task as cancelled.
func (s *Store) Cancel(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE tasks SET status = ?, finished_at = ?
WHERE id = ? AND status IN (?, ?)`,
		StatusCancelled, time.Now().UTC().Format(time.RFC3339Nano), id, StatusPending, StatusRunning)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Get fetches one task.
func (s *Store) Get(ctx context.Context, id int64) (Task, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, project, cwd, prompt, status, created_at, started_at, finished_at, log_path, exit_code, note
FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

// List returns up to `limit` tasks, optionally filtered by status.
func (s *Store) List(ctx context.Context, status string, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, project, cwd, prompt, status, created_at, started_at, finished_at, log_path, exit_code, note FROM tasks`
	args := []any{}
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// scanTask is shared between Get + List; the *sql.Row and *sql.Rows both
// implement Scan(...) the same way.
type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (Task, error) {
	var t Task
	var cwd, startedAt, finishedAt, logPath, note sql.NullString
	var createdAt string
	var exitCode sql.NullInt64
	if err := s.Scan(&t.ID, &t.Project, &cwd, &t.Prompt, &t.Status, &createdAt,
		&startedAt, &finishedAt, &logPath, &exitCode, &note); err != nil {
		return Task{}, err
	}
	t.CWD = cwd.String
	t.LogPath = logPath.String
	t.Note = note.String
	t.ExitCode = int(exitCode.Int64)
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if startedAt.Valid {
		t.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt.String)
	}
	if finishedAt.Valid {
		t.FinishedAt, _ = time.Parse(time.RFC3339Nano, finishedAt.String)
	}
	return t, nil
}
