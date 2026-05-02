package mempalace

import (
	"database/sql"
	"fmt"
)

// migrate adds the v0.13.0 columns to entries: category, source_machine,
// merged_into. Idempotent — uses PRAGMA table_info to skip columns already
// present, so existing dbs get an in-place upgrade with no data loss.
func (s *Store) migrate() error {
	have, err := tableColumns(s.db, "entries")
	if err != nil {
		return err
	}
	additions := []struct{ name, def string }{
		{"category", "TEXT"},
		{"source_machine", "TEXT"},
		{"merged_into", "INTEGER"},
	}
	for _, a := range additions {
		if have[a.name] {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE entries ADD COLUMN %s %s", a.name, a.def)
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("add column entries.%s: %w", a.name, err)
		}
	}
	if _, err := s.db.Exec(`
CREATE INDEX IF NOT EXISTS idx_entries_category ON entries(category);
CREATE INDEX IF NOT EXISTS idx_entries_machine ON entries(source_machine);
CREATE INDEX IF NOT EXISTS idx_entries_merged ON entries(merged_into);
`); err != nil {
		return err
	}
	return nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		have[name] = true
	}
	return have, nil
}
