// Small key-value settings persistence — UI preferences like the metronome
// BPM that should survive a restart but are too slight to deserve a table
// each. Same bytdb discipline as scenes: one table, no foreign keys, native
// column types only.
//
// Design notes:
//   - Values are TEXT even when they hold numbers. Settings are written and
//     read by name, one at a time, never aggregated or compared in SQL, so a
//     typed column buys nothing — while a single TEXT column means the table
//     never migrates when a future setting isn't numeric.
//   - No Delete/List: a setting reverts to its default by being absent, and
//     defaults live in code (the page renderer), not in the table. Callers
//     get (value, found) and fall back themselves, which keeps the store
//     ignorant of what any key means.
package store

import (
	"time"

	"github.com/rohanthewiz/serr"
)

// migrateSettings creates the settings table if absent, using the same
// catalog-probe pattern as the scenes migration (bytdb has no
// CREATE TABLE IF NOT EXISTS).
func (s *Store) migrateSettings() error {
	res, err := s.db.Exec(
		`SELECT count(*) FROM information_schema.tables WHERE table_name = $1`,
		"settings")
	if err != nil {
		return serr.Wrap(err, "msg", "probing for settings table")
	}
	if len(res.Rows) == 1 {
		if n, ok := res.Rows[0][0].(int64); ok && n > 0 {
			return nil // table already exists
		}
	}

	_, err = s.db.Exec(`CREATE TABLE settings (
		key        TEXT PRIMARY KEY,
		value      TEXT,
		updated_at INT
	)`)
	if err != nil {
		return serr.Wrap(err, "msg", "creating settings table")
	}
	return nil
}

// SetSetting upserts one key. Last write wins — settings are single-owner
// UI state, so there is nothing to merge.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = $3`,
		key, value, time.Now().Unix())
	if err != nil {
		return serr.Wrap(err, "key", key)
	}
	return nil
}

// GetSetting returns the stored value and whether the key exists. Absence is
// not an error — it just means "use the default", and only the caller knows
// what that default is.
func (s *Store) GetSetting(key string) (value string, found bool, err error) {
	res, err := s.db.Exec(`SELECT value FROM settings WHERE key = $1`, key)
	if err != nil {
		return "", false, serr.Wrap(err, "key", key)
	}
	if len(res.Rows) != 1 {
		return "", false, nil
	}
	value, _ = res.Rows[0][0].(string)
	return value, true, nil
}
