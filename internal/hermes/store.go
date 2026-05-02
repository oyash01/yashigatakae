package hermes

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	StatusDLQ       = "dlq"       // exhausted retries
	StatusScheduled = "scheduled" // waiting for next_attempt_at
)

// Task is one row of the queue. Phase-2 columns added: Priority,
// IdempotencyKey, RetryCount, MaxRetries, NextAttemptAt, DependencyID,
// DLQReason, ScheduledAt.
type Task struct {
	ID             int64     `json:"id"`
	Project        string    `json:"project"`
	CWD            string    `json:"cwd,omitempty"`
	Prompt         string    `json:"prompt"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
	LogPath        string    `json:"log_path,omitempty"`
	ExitCode       int       `json:"exit_code,omitempty"`
	Note           string    `json:"note,omitempty"`
	Priority       int       `json:"priority"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	RetryCount     int       `json:"retry_count"`
	MaxRetries     int       `json:"max_retries"`
	NextAttemptAt  time.Time `json:"next_attempt_at,omitempty"`
	DependencyID   int64     `json:"dependency_id,omitempty"`
	DLQReason      string    `json:"dlq_reason,omitempty"`
	ScheduledAt    time.Time `json:"scheduled_at,omitempty"`
}

// Schedule is one cron-style row: every tick, the worker enqueues a fresh task
// from the row's project/prompt with a fresh idempotency key.
type Schedule struct {
	ID         int64     `json:"id"`
	Cron       string    `json:"cron"`
	Project    string    `json:"project"`
	Prompt     string    `json:"prompt"`
	CWD        string    `json:"cwd,omitempty"`
	Note       string    `json:"note,omitempty"`
	Priority   int       `json:"priority"`
	MaxRetries int       `json:"max_retries"`
	Active     bool      `json:"active"`
	LastFiredAt time.Time `json:"last_fired_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Store is the sqlite-backed queue.
type Store struct {
	db   *sql.DB
	path string
}

// Open returns a Store, creating ~/.yashigatakae/hermes.db if missing and
// running every pending migration step.
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
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Path returns the sqlite path.
func (s *Store) Path() string { return s.path }

// Close releases the handle.
func (s *Store) Close() error { return s.db.Close() }

// migrate creates the base tables if missing, then ADD COLUMNs each new
// Phase-2 field idempotently. SQLite's ALTER TABLE doesn't support
// IF NOT EXISTS, so we introspect via PRAGMA table_info first.
func (s *Store) migrate() error {
	if _, err := s.db.Exec(baseSchema); err != nil {
		return err
	}
	if err := s.addColumns("tasks", phase2TaskColumns); err != nil {
		return err
	}
	if _, err := s.db.Exec(scheduleSchema); err != nil {
		return err
	}
	if _, err := s.db.Exec(phase2Indexes); err != nil {
		return err
	}
	return nil
}

func (s *Store) addColumns(table string, cols []columnDef) error {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		have[name] = true
	}
	for _, c := range cols {
		if have[c.name] {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, c.name, c.typeAndDefault)
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("add column %s.%s: %w", table, c.name, err)
		}
	}
	return nil
}

type columnDef struct{ name, typeAndDefault string }

const baseSchema = `
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

var phase2TaskColumns = []columnDef{
	{"priority", "INTEGER NOT NULL DEFAULT 5"},
	{"idempotency_key", "TEXT"},
	{"retry_count", "INTEGER NOT NULL DEFAULT 0"},
	{"max_retries", "INTEGER NOT NULL DEFAULT 5"},
	{"next_attempt_at", "TEXT"},
	{"dependency_id", "INTEGER"},
	{"dlq_reason", "TEXT"},
	{"scheduled_at", "TEXT"},
}

const scheduleSchema = `
CREATE TABLE IF NOT EXISTS schedules (
  id            INTEGER PRIMARY KEY,
  cron          TEXT NOT NULL,
  project       TEXT NOT NULL,
  prompt        TEXT NOT NULL,
  cwd           TEXT,
  note          TEXT,
  priority      INTEGER NOT NULL DEFAULT 5,
  max_retries   INTEGER NOT NULL DEFAULT 5,
  active        INTEGER NOT NULL DEFAULT 1,
  last_fired_at TEXT,
  created_at    TEXT NOT NULL
);
`

const phase2Indexes = `
CREATE INDEX IF NOT EXISTS idx_tasks_ready ON tasks(status, priority DESC, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_tasks_idempo ON tasks(idempotency_key, created_at);
CREATE INDEX IF NOT EXISTS idx_tasks_dep ON tasks(dependency_id);
`

// Enqueue inserts a pending task and returns its ID. If t.IdempotencyKey is
// set AND a task with the same key exists in the last 7 days that isn't yet
// finished or that finished successfully, returns that task's id without
// creating a new row (the second-arg bool is true on hit).
func (s *Store) Enqueue(ctx context.Context, t Task) (int64, bool, error) {
	if t.Project == "" {
		return 0, false, fmt.Errorf("project is required")
	}
	if t.Prompt == "" {
		return 0, false, fmt.Errorf("prompt is required")
	}
	if t.MaxRetries == 0 {
		t.MaxRetries = 5
	}
	if t.Priority == 0 {
		t.Priority = 5
	}

	if t.IdempotencyKey != "" {
		cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339Nano)
		row := s.db.QueryRowContext(ctx, `
SELECT id FROM tasks
WHERE idempotency_key = ?
  AND created_at > ?
  AND status IN (?, ?, ?, ?)
ORDER BY id DESC LIMIT 1`,
			t.IdempotencyKey, cutoff, StatusPending, StatusRunning, StatusScheduled, StatusDone)
		var id int64
		if err := row.Scan(&id); err == nil {
			return id, true, nil
		}
	}

	now := time.Now().UTC()
	status := StatusPending
	var nextAt sql.NullString
	var schedAt sql.NullString
	if !t.ScheduledAt.IsZero() && t.ScheduledAt.After(now) {
		status = StatusScheduled
		nextAt = sql.NullString{String: t.ScheduledAt.Format(time.RFC3339Nano), Valid: true}
		schedAt = nextAt
	}
	var depID sql.NullInt64
	if t.DependencyID > 0 {
		depID = sql.NullInt64{Int64: t.DependencyID, Valid: true}
	}
	var idem sql.NullString
	if t.IdempotencyKey != "" {
		idem = sql.NullString{String: t.IdempotencyKey, Valid: true}
	}

	res, err := s.db.ExecContext(ctx, `
INSERT INTO tasks (project, cwd, prompt, status, created_at, note,
                   priority, idempotency_key, retry_count, max_retries,
                   next_attempt_at, dependency_id, scheduled_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		t.Project, t.CWD, t.Prompt, status, now.Format(time.RFC3339Nano), t.Note,
		t.Priority, idem, t.MaxRetries, nextAt, depID, schedAt,
	)
	if err != nil {
		return 0, false, err
	}
	id, err := res.LastInsertId()
	return id, false, err
}

// ClaimNext picks the highest-priority ready task (priority DESC, id ASC) and
// marks it running. "Ready" means: status=pending, OR status=scheduled with
// next_attempt_at <= now, AND any dependency is StatusDone. Returns
// sql.ErrNoRows if nothing is ready.
func (s *Store) ClaimNext(ctx context.Context) (Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	row := tx.QueryRowContext(ctx, `
SELECT t.id, t.project, t.cwd, t.prompt, t.created_at, t.note,
       t.priority, t.retry_count, t.max_retries, t.dependency_id
FROM tasks t
LEFT JOIN tasks d ON d.id = t.dependency_id
WHERE (t.status = ?
       OR (t.status = ? AND COALESCE(t.next_attempt_at, '0000') <= ?))
  AND (t.dependency_id IS NULL OR d.status = ?)
ORDER BY t.priority DESC, t.id ASC LIMIT 1`,
		StatusPending, StatusScheduled, now, StatusDone)

	var t Task
	var createdAt string
	var cwd, note sql.NullString
	var depID sql.NullInt64
	if err := row.Scan(&t.ID, &t.Project, &cwd, &t.Prompt, &createdAt, &note,
		&t.Priority, &t.RetryCount, &t.MaxRetries, &depID); err != nil {
		return Task{}, err
	}
	t.CWD = cwd.String
	t.Note = note.String
	t.DependencyID = depID.Int64
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if _, err := tx.ExecContext(ctx, `
UPDATE tasks SET status = ?, started_at = ? WHERE id = ?`,
		StatusRunning, now, t.ID); err != nil {
		return Task{}, err
	}
	t.Status = StatusRunning
	t.StartedAt, _ = time.Parse(time.RFC3339Nano, now)
	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	return t, nil
}

// Finish updates a task's status + exit code + log path on completion.
// Use FinishWithRetry from the worker for failed-with-retry semantics.
func (s *Store) Finish(ctx context.Context, id int64, status string, exitCode int, logPath string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE tasks SET status = ?, finished_at = ?, exit_code = ?, log_path = ? WHERE id = ?`,
		status, now.Format(time.RFC3339Nano), exitCode, logPath, id)
	return err
}

// FinishWithRetry is called when a task fails. If retry_count + 1 >= max_retries,
// the task transitions to dlq with the supplied reason. Otherwise it's bumped
// back to scheduled with next_attempt_at = now + backoff.
func (s *Store) FinishWithRetry(ctx context.Context, t Task, reason string, logPath string, exitCode int, backoff time.Duration) (string, error) {
	now := time.Now().UTC()
	newCount := t.RetryCount + 1
	if newCount >= t.MaxRetries {
		_, err := s.db.ExecContext(ctx, `
UPDATE tasks SET status = ?, finished_at = ?, retry_count = ?, exit_code = ?, log_path = ?, dlq_reason = ?
WHERE id = ?`,
			StatusDLQ, now.Format(time.RFC3339Nano), newCount, exitCode, logPath, reason, t.ID)
		return StatusDLQ, err
	}
	nextAt := now.Add(backoff).Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
UPDATE tasks SET status = ?, retry_count = ?, exit_code = ?, log_path = ?, next_attempt_at = ?, finished_at = NULL
WHERE id = ?`,
		StatusScheduled, newCount, exitCode, logPath, nextAt, t.ID)
	return StatusScheduled, err
}

// Cancel marks a pending or running task as cancelled.
func (s *Store) Cancel(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE tasks SET status = ?, finished_at = ?
WHERE id = ? AND status IN (?, ?, ?)`,
		StatusCancelled, time.Now().UTC().Format(time.RFC3339Nano), id,
		StatusPending, StatusRunning, StatusScheduled)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Get fetches one task.
func (s *Store) Get(ctx context.Context, id int64) (Task, error) {
	row := s.db.QueryRowContext(ctx, taskSelect+` WHERE id = ?`, id)
	return scanTask(row)
}

// List returns up to `limit` tasks, optionally filtered by status.
func (s *Store) List(ctx context.Context, status string, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 50
	}
	q := taskSelect
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

// RetryDLQ moves a DLQ task back to pending with retry_count reset.
func (s *Store) RetryDLQ(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE tasks SET status = ?, retry_count = 0, dlq_reason = NULL, next_attempt_at = NULL, finished_at = NULL
WHERE id = ? AND status = ?`,
		StatusPending, id, StatusDLQ)
	return err
}

const taskSelect = `
SELECT id, project, cwd, prompt, status, created_at, started_at, finished_at, log_path, exit_code, note,
       priority, idempotency_key, retry_count, max_retries, next_attempt_at, dependency_id, dlq_reason, scheduled_at
FROM tasks`

// scanner accepts both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (Task, error) {
	var t Task
	var cwd, startedAt, finishedAt, logPath, note sql.NullString
	var idemKey, nextAt, dlqReason, schedAt sql.NullString
	var depID sql.NullInt64
	var createdAt string
	var exitCode sql.NullInt64
	if err := s.Scan(&t.ID, &t.Project, &cwd, &t.Prompt, &t.Status, &createdAt,
		&startedAt, &finishedAt, &logPath, &exitCode, &note,
		&t.Priority, &idemKey, &t.RetryCount, &t.MaxRetries, &nextAt, &depID, &dlqReason, &schedAt); err != nil {
		return Task{}, err
	}
	t.CWD = cwd.String
	t.LogPath = logPath.String
	t.Note = note.String
	t.IdempotencyKey = idemKey.String
	t.DLQReason = dlqReason.String
	t.DependencyID = depID.Int64
	t.ExitCode = int(exitCode.Int64)
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if startedAt.Valid {
		t.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt.String)
	}
	if finishedAt.Valid {
		t.FinishedAt, _ = time.Parse(time.RFC3339Nano, finishedAt.String)
	}
	if nextAt.Valid {
		t.NextAttemptAt, _ = time.Parse(time.RFC3339Nano, nextAt.String)
	}
	if schedAt.Valid {
		t.ScheduledAt, _ = time.Parse(time.RFC3339Nano, schedAt.String)
	}
	return t, nil
}

// --- schedules ---

// AddSchedule inserts a cron row.
func (s *Store) AddSchedule(ctx context.Context, sc Schedule) (int64, error) {
	if strings.TrimSpace(sc.Cron) == "" {
		return 0, fmt.Errorf("cron is required")
	}
	if sc.Priority == 0 {
		sc.Priority = 5
	}
	if sc.MaxRetries == 0 {
		sc.MaxRetries = 5
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO schedules (cron, project, prompt, cwd, note, priority, max_retries, active, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		sc.Cron, sc.Project, sc.Prompt, sc.CWD, sc.Note, sc.Priority, sc.MaxRetries, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListSchedules returns the active schedules.
func (s *Store) ListSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, cron, project, prompt, cwd, note, priority, max_retries, active, last_fired_at, created_at
FROM schedules ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		var sc Schedule
		var cwd, note, lastFired sql.NullString
		var active int
		var created string
		if err := rows.Scan(&sc.ID, &sc.Cron, &sc.Project, &sc.Prompt, &cwd, &note,
			&sc.Priority, &sc.MaxRetries, &active, &lastFired, &created); err != nil {
			return nil, err
		}
		sc.CWD = cwd.String
		sc.Note = note.String
		sc.Active = active == 1
		if lastFired.Valid {
			sc.LastFiredAt, _ = time.Parse(time.RFC3339Nano, lastFired.String)
		}
		sc.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, sc)
	}
	return out, rows.Err()
}

// DeleteSchedule removes a schedule row.
func (s *Store) DeleteSchedule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	return err
}

// MarkScheduleFired updates last_fired_at on a schedule row.
func (s *Store) MarkScheduleFired(ctx context.Context, id int64, t time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE schedules SET last_fired_at = ? WHERE id = ?`,
		t.UTC().Format(time.RFC3339Nano), id)
	return err
}
