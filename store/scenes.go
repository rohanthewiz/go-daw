// Package store persists mixer scenes ("digital memory") in bytdb — a pure-Go
// embedded relational store, so scene persistence adds zero cgo weight.
//
// Design notes:
//   - One table, no foreign keys: a scene snapshot is naturally a single
//     JSON document; relational decomposition would only add join overhead
//     and schema churn every time a channel parameter is added.
//   - The state column is TEXT holding JSON and updated_at is epoch seconds,
//     keeping the schema to types every bytdb version handles natively.
package store

import (
	"time"

	"github.com/rohanthewiz/bytdb"
	bsql "github.com/rohanthewiz/bytdb/sql"
	"github.com/rohanthewiz/logger"
	"github.com/rohanthewiz/serr"
)

// Store wraps the bytdb engine + SQL layer.
type Store struct {
	eng *bytdb.Engine
	db  *bsql.DB
}

// SceneInfo is a listing row for the UI.
type SceneInfo struct {
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Open opens (or creates) the database file and ensures the schema exists.
func Open(path string) (*Store, error) {
	eng, err := bytdb.Open(path)
	if err != nil {
		return nil, serr.Wrap(err, "path", path)
	}
	s := &Store{eng: eng, db: bsql.New(eng)}

	if err := s.migrate(); err != nil {
		eng.Close()
		return nil, serr.Wrap(err)
	}
	if err := s.migrateProgress(); err != nil {
		eng.Close()
		return nil, serr.Wrap(err)
	}
	logger.Info("Scene store ready", "path", path)
	return s, nil
}

// migrate creates the scenes table if absent. bytdb has no
// CREATE TABLE IF NOT EXISTS, so we probe the catalog first.
func (s *Store) migrate() error {
	res, err := s.db.Exec(
		`SELECT count(*) FROM information_schema.tables WHERE table_name = $1`, "scenes")
	if err != nil {
		return serr.Wrap(err, "msg", "probing for scenes table")
	}
	if len(res.Rows) == 1 {
		if n, ok := res.Rows[0][0].(int64); ok && n > 0 {
			return nil // table already exists
		}
	}

	_, err = s.db.Exec(`CREATE TABLE scenes (
		name       TEXT PRIMARY KEY,
		updated_at INT,
		state      TEXT
	)`)
	if err != nil {
		return serr.Wrap(err, "msg", "creating scenes table")
	}
	return nil
}

// Save upserts a scene snapshot (state is the JSON-encoded SceneState).
func (s *Store) Save(name string, stateJSON []byte) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`INSERT INTO scenes (name, updated_at, state) VALUES ($1, $2, $3)
		 ON CONFLICT (name) DO UPDATE SET updated_at = $2, state = $3`,
		name, now, string(stateJSON))
	if err != nil {
		return serr.Wrap(err, "scene", name)
	}
	return nil
}

// Load returns the JSON state of a named scene.
func (s *Store) Load(name string) ([]byte, error) {
	res, err := s.db.Exec(`SELECT state FROM scenes WHERE name = $1`, name)
	if err != nil {
		return nil, serr.Wrap(err, "scene", name)
	}
	if len(res.Rows) == 0 {
		return nil, serr.New("scene not found", "scene", name)
	}
	state, ok := res.Rows[0][0].(string)
	if !ok {
		return nil, serr.New("scene state has unexpected type", "scene", name)
	}
	return []byte(state), nil
}

// List returns all scenes, most recently updated first.
func (s *Store) List() ([]SceneInfo, error) {
	res, err := s.db.Exec(`SELECT name, updated_at FROM scenes ORDER BY updated_at DESC`)
	if err != nil {
		return nil, serr.Wrap(err)
	}
	infos := make([]SceneInfo, 0, len(res.Rows))
	for _, row := range res.Rows {
		name, _ := row[0].(string)
		epoch, _ := row[1].(int64)
		infos = append(infos, SceneInfo{Name: name, UpdatedAt: time.Unix(epoch, 0)})
	}
	return infos, nil
}

// Delete removes a scene by name; deleting a missing scene is not an error.
func (s *Store) Delete(name string) error {
	_, err := s.db.Exec(`DELETE FROM scenes WHERE name = $1`, name)
	if err != nil {
		return serr.Wrap(err, "scene", name)
	}
	return nil
}

// Close flushes and closes the underlying engine. Check the error: bytdb
// surfaces deferred background write problems here.
func (s *Store) Close() error {
	if err := s.eng.Close(); err != nil {
		return serr.Wrap(err, "msg", "closing scene store")
	}
	return nil
}
