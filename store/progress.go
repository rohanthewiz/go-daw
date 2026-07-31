// Tutorial progress persistence. Same bytdb discipline as scenes: one table,
// no foreign keys, native column types only.
//
// Design notes:
//   - Rows are keyed by lesson NAME, not catalog index. The catalog is
//     compiled-in and reordered/extended over time; names are the stable
//     identity, so progress survives inserting a new lesson mid-list.
//   - Unlike scenes, the row is fixed-shape (a few counters), so plain
//     columns beat a JSON blob: they stay queryable and there is no schema
//     churn risk — the shape of "how did a lesson run go" doesn't grow the
//     way channel parameters do.
//   - The upsert is read-modify-write in Go rather than arithmetic in an
//     ON CONFLICT clause (completions = completions + 1, min(best, new)).
//     The server is the only writer and handlers run one at a time per
//     browser action, so the race window is theoretical, and keeping SQL to
//     the same param-only INSERT shape scenes uses means no bet on which
//     expressions bytdb's DO UPDATE supports.
package store

import (
	"time"

	"github.com/rohanthewiz/serr"
)

// LessonProgress is one lesson's lifetime record, shaped for the UI.
type LessonProgress struct {
	Lesson      string    `json:"lesson"`
	Completions int       `json:"completions"`
	BestMisses  int       `json:"bestMisses"`
	LastMisses  int       `json:"lastMisses"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// migrateProgress creates the tutorial_progress table if absent, using the
// same catalog-probe pattern as the scenes migration (bytdb has no
// CREATE TABLE IF NOT EXISTS).
func (s *Store) migrateProgress() error {
	res, err := s.db.Exec(
		`SELECT count(*) FROM information_schema.tables WHERE table_name = $1`,
		"tutorial_progress")
	if err != nil {
		return serr.Wrap(err, "msg", "probing for tutorial_progress table")
	}
	if len(res.Rows) == 1 {
		if n, ok := res.Rows[0][0].(int64); ok && n > 0 {
			return nil // table already exists
		}
	}

	_, err = s.db.Exec(`CREATE TABLE tutorial_progress (
		lesson      TEXT PRIMARY KEY,
		completions INT,
		best_misses INT,
		last_misses INT,
		updated_at  INT
	)`)
	if err != nil {
		return serr.Wrap(err, "msg", "creating tutorial_progress table")
	}
	return nil
}

// RecordLessonPass upserts one completed lesson run: completions increments,
// best_misses keeps the lifetime minimum, last_misses records this run.
func (s *Store) RecordLessonPass(lesson string, misses int) error {
	completions, best := 0, misses
	res, err := s.db.Exec(
		`SELECT completions, best_misses FROM tutorial_progress WHERE lesson = $1`, lesson)
	if err != nil {
		return serr.Wrap(err, "lesson", lesson)
	}
	if len(res.Rows) == 1 {
		if n, ok := res.Rows[0][0].(int64); ok {
			completions = int(n)
		}
		if b, ok := res.Rows[0][1].(int64); ok && int(b) < best {
			best = int(b)
		}
	}

	_, err = s.db.Exec(
		`INSERT INTO tutorial_progress (lesson, completions, best_misses, last_misses, updated_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (lesson) DO UPDATE SET
		   completions = $2, best_misses = $3, last_misses = $4, updated_at = $5`,
		lesson, completions+1, best, misses, time.Now().Unix())
	if err != nil {
		return serr.Wrap(err, "lesson", lesson)
	}
	return nil
}

// ListProgress returns every lesson's record, most recently played first —
// the client indexes them by name, so order is cosmetic.
func (s *Store) ListProgress() ([]LessonProgress, error) {
	res, err := s.db.Exec(
		`SELECT lesson, completions, best_misses, last_misses, updated_at
		 FROM tutorial_progress ORDER BY updated_at DESC`)
	if err != nil {
		return nil, serr.Wrap(err)
	}
	list := make([]LessonProgress, 0, len(res.Rows))
	for _, row := range res.Rows {
		var p LessonProgress
		p.Lesson, _ = row[0].(string)
		if n, ok := row[1].(int64); ok {
			p.Completions = int(n)
		}
		if n, ok := row[2].(int64); ok {
			p.BestMisses = int(n)
		}
		if n, ok := row[3].(int64); ok {
			p.LastMisses = int(n)
		}
		if epoch, ok := row[4].(int64); ok {
			p.UpdatedAt = time.Unix(epoch, 0)
		}
		list = append(list, p)
	}
	return list, nil
}
