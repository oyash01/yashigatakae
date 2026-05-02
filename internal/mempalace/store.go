package mempalace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// Entry is a single memory record.
type Entry struct {
	ID            int64     `json:"id"`
	Timestamp     time.Time `json:"timestamp"`
	Source        string    `json:"source"`
	SourceMachine string    `json:"source_machine,omitempty"`
	Project       string    `json:"project,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
	Body          string    `json:"body"`
	Category      string    `json:"category,omitempty"`
	MergedInto    int64     `json:"merged_into,omitempty"`
	Embedding     []float32 `json:"-"`
}

// Hit is an Entry returned from Recall, with its similarity score.
type Hit struct {
	Entry
	Score float32 `json:"score"`
}

// Store wraps the sqlite database with the schema mempalace needs.
type Store struct {
	db   *sql.DB
	path string
}

// Open creates ~/.yashigatakae/mempalace.db (and parent dir) if missing,
// applies the schema, and returns a ready Store.
func Open() (*Store, error) {
	yashDir, err := osdetect.YashigatakaeDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(yashDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(yashDir, "mempalace.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	store := &Store{db: db, path: path}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return store, nil
}

// Path returns the on-disk location of the store (useful for doctor + sync).
func (s *Store) Path() string { return s.path }

// Close releases the underlying database handle.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS entries (
  id             INTEGER PRIMARY KEY,
  ts             TEXT NOT NULL,
  source         TEXT NOT NULL,
  project        TEXT,
  tags           TEXT,
  body           TEXT NOT NULL,
  embedding_json TEXT,
  embed_dim      INTEGER,
  embed_model    TEXT
);
CREATE INDEX IF NOT EXISTS idx_entries_project ON entries(project);
CREATE INDEX IF NOT EXISTS idx_entries_ts ON entries(ts);

CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

// Insert adds a new entry. The caller is responsible for embedding (pass nil
// if no embedding available — entry will be keyword-search only).
func (s *Store) Insert(ctx context.Context, e Entry, embedModel string) (int64, error) {
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	tagsJSON, _ := json.Marshal(e.Tags)
	var embedJSON sql.NullString
	var embedDim sql.NullInt64
	var embedMod sql.NullString
	if len(e.Embedding) > 0 {
		b, _ := json.Marshal(e.Embedding)
		embedJSON.String = string(b)
		embedJSON.Valid = true
		embedDim.Int64 = int64(len(e.Embedding))
		embedDim.Valid = true
		embedMod.String = embedModel
		embedMod.Valid = embedModel != ""
	}

	res, err := s.db.ExecContext(ctx, `
INSERT INTO entries (ts, source, project, tags, body, embedding_json, embed_dim, embed_model,
                     category, source_machine)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts.Format(time.RFC3339Nano),
		e.Source,
		nullIfEmpty(e.Project),
		string(tagsJSON),
		e.Body,
		embedJSON,
		embedDim,
		embedMod,
		nullIfEmpty(e.Category),
		nullIfEmpty(e.SourceMachine),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Delete removes an entry by ID. Returns the number of rows affected.
func (s *Store) Delete(ctx context.Context, id int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AllEntries streams every entry into the caller's callback. Used by Recall
// to compute brute-force cosine across the full corpus. For corpora larger
// than ~50k entries, swap in sqlite-vec.
//
// Filters: optional project, optional category, optional source_machine.
// Entries that have been merged into another row (merged_into IS NOT NULL)
// are skipped — Recall returns the canonical merge target instead.
func (s *Store) AllEntries(ctx context.Context, project string, cb func(Entry) error) error {
	return s.AllEntriesFiltered(ctx, EntryFilter{Project: project}, cb)
}

// EntryFilter selects which rows to stream out of AllEntriesFiltered.
type EntryFilter struct {
	Project       string
	Category      string
	SourceMachine string
	IncludeMerged bool // false = skip rows whose merged_into is set
}

// AllEntriesFiltered is the v0.13 entry point used by Recall, dedupe, and
// consolidate. AllEntries forwards here with EntryFilter{Project: project}.
func (s *Store) AllEntriesFiltered(ctx context.Context, f EntryFilter, cb func(Entry) error) error {
	q := `SELECT id, ts, source, project, tags, body, embedding_json,
	             category, source_machine, merged_into
	      FROM entries`
	args := []any{}
	clauses := []string{}
	if f.Project != "" {
		clauses = append(clauses, "project = ?")
		args = append(args, f.Project)
	}
	if f.Category != "" {
		clauses = append(clauses, "category = ?")
		args = append(args, f.Category)
	}
	if f.SourceMachine != "" {
		clauses = append(clauses, "source_machine = ?")
		args = append(args, f.SourceMachine)
	}
	if !f.IncludeMerged {
		clauses = append(clauses, "merged_into IS NULL")
	}
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += ` ORDER BY ts DESC`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id       int64
			ts, src  string
			proj     sql.NullString
			tagsRaw  sql.NullString
			body     string
			embedRaw sql.NullString
			category sql.NullString
			machine  sql.NullString
			merged   sql.NullInt64
		)
		if err := rows.Scan(&id, &ts, &src, &proj, &tagsRaw, &body, &embedRaw,
			&category, &machine, &merged); err != nil {
			return err
		}
		e := Entry{
			ID:            id,
			Source:        src,
			Project:       proj.String,
			Body:          body,
			Category:      category.String,
			SourceMachine: machine.String,
			MergedInto:    merged.Int64,
		}
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			e.Timestamp = t
		}
		if tagsRaw.Valid && tagsRaw.String != "" {
			_ = json.Unmarshal([]byte(tagsRaw.String), &e.Tags)
		}
		if embedRaw.Valid && embedRaw.String != "" {
			_ = json.Unmarshal([]byte(embedRaw.String), &e.Embedding)
		}
		if err := cb(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

// UpdateMergedRefresh is called by dedupe when a near-duplicate insert is
// folded into an existing row: refresh ts, append new tags, optionally
// extend body. The on-disk merged_into column points at this row from any
// future near-dupes the dedupe pass might catch.
func (s *Store) UpdateMergedRefresh(ctx context.Context, id int64, addTags []string) error {
	row := s.db.QueryRowContext(ctx, `SELECT tags FROM entries WHERE id = ?`, id)
	var tagsRaw sql.NullString
	if err := row.Scan(&tagsRaw); err != nil {
		return err
	}
	existing := []string{}
	if tagsRaw.Valid && tagsRaw.String != "" {
		_ = json.Unmarshal([]byte(tagsRaw.String), &existing)
	}
	merged := mergeTags(existing, addTags)
	mergedJSON, _ := json.Marshal(merged)
	_, err := s.db.ExecContext(ctx, `
UPDATE entries SET ts = ?, tags = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), string(mergedJSON), id)
	return err
}

// MarkMerged sets merged_into on `srcID` to point at `dstID`. Used when
// dedupe consolidates an existing inserted row into a canonical earlier row.
func (s *Store) MarkMerged(ctx context.Context, srcID, dstID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE entries SET merged_into = ? WHERE id = ?`, dstID, srcID)
	return err
}

// SetCategory writes the category column for an entry.
func (s *Store) SetCategory(ctx context.Context, id int64, cat string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE entries SET category = ? WHERE id = ?`, cat, id)
	return err
}

func mergeTags(a, b []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range a {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			out = append(out, t)
			seen[t] = true
		}
	}
	for _, t := range b {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			out = append(out, t)
			seen[t] = true
		}
	}
	return out
}

// Stats reports counts useful for debug + doctor.
type StatsResult struct {
	TotalEntries  int64    `json:"total_entries"`
	WithEmbedding int64    `json:"with_embedding"`
	Projects      []string `json:"projects"`
	Path          string   `json:"path"`
	SizeBytes     int64    `json:"size_bytes"`
}

func (s *Store) Stats(ctx context.Context) (StatsResult, error) {
	var out StatsResult
	out.Path = s.path
	if info, err := os.Stat(s.path); err == nil {
		out.SizeBytes = info.Size()
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries`).Scan(&out.TotalEntries); err != nil {
		return out, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE embedding_json IS NOT NULL`).Scan(&out.WithEmbedding); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT project FROM entries WHERE project IS NOT NULL AND project <> '' ORDER BY project`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return out, err
		}
		out.Projects = append(out.Projects, p)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// stringTags is a small helper for CLI tag inputs ("foo,bar" → ["foo","bar"]).
func stringTags(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
