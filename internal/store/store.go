// Package store keeps the library in a SQLite database: the folders, the files found in them
// with their checksums, the DATs and which file is which game.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	sqlite3driver "github.com/ncruces/go-sqlite3/driver"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is the database.
type Store struct {
	db *sql.DB
}

// Open opens or creates the database in the data folder and brings its tables up to date.
func Open(dataFolder string) (*Store, error) {
	path := filepath.Join(dataFolder, "completist.db")
	// _txlock=immediate: a transaction takes the write lock when it begins, so a scan and a DAT
	// import wait for each other instead of failing with "database is locked"
	dsn := "file:" + filepath.ToSlash(path) + "?_txlock=immediate&_pragma=busy_timeout(30000)&_pragma=journal_mode(wal)&_pragma=foreign_keys(1)&_pragma=synchronous(normal)"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("database %s: %w", path, err)
	}
	return s, nil
}

// OpenMemory opens a database that lives only in memory, for tests and the demo mode.
func OpenMemory() (*Store, error) {
	db, err := sql.Open("sqlite3", "file:completist?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	return s, s.migrate()
}

func (s *Store) Close() error {
	return s.db.Close()
}

// migrate runs the migrations not yet applied, in the order of their numbers.
func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	current := 0
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	type step struct {
		version int
		name    string
	}
	steps := []step{}
	for _, e := range entries {
		number, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(number)
		if err != nil {
			return fmt.Errorf("migration %s: no number", e.Name())
		}
		steps = append(steps, step{v, e.Name()})
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].version < steps[j].version })
	for _, st := range steps {
		if st.version <= current {
			continue
		}
		script, err := migrations.ReadFile("migrations/" + st.name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(script)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", st.name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, st.version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ---- settings ----

// GetSetting reads a value stored as JSON; ok is false when it is not set.
func (s *Store) GetSetting(key string, value any) (bool, error) {
	var raw string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), value)
}

// SetSetting stores a value as JSON.
func (s *Store) SetSetting(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(raw))
	return err
}

// ---- libraries ----

// Library is a folder with games.
type Library struct {
	ID       int64     `json:"id"`
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	ReadOnly bool      `json:"readOnly"`
	Created  time.Time `json:"created"`
	Files    int       `json:"files"`
}

func (s *Store) Libraries() ([]Library, error) {
	rows, err := s.db.Query(`SELECT l.id, l.path, l.name, l.read_only, l.created_at, (SELECT COUNT(*) FROM files f WHERE f.library_id = l.id)
		FROM libraries l ORDER BY l.name, l.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Library{}
	for rows.Next() {
		var l Library
		var created string
		if err := rows.Scan(&l.ID, &l.Path, &l.Name, &l.ReadOnly, &created, &l.Files); err != nil {
			return nil, err
		}
		l.Created, _ = time.Parse(time.RFC3339, created)
		out = append(out, l)
	}
	return out, rows.Err()
}

var ErrLibraryExists = errors.New("this folder is already a library")
var ErrLibraryNested = errors.New("this folder is inside another library, or contains one")

func (s *Store) AddLibrary(path string, name string, readOnly bool) (Library, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Library{}, err
	}
	path = filepath.Clean(abs)
	if name == "" {
		name = filepath.Base(path)
	}
	// a file in two libraries would be read twice and belong to whichever was scanned last
	existing, err := s.Libraries()
	if err != nil {
		return Library{}, err
	}
	for _, l := range existing {
		if l.Path == path {
			return Library{}, ErrLibraryExists
		}
		if within(path, l.Path) || within(l.Path, path) {
			return Library{}, ErrLibraryNested
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	res, err := s.db.Exec(`INSERT INTO libraries (path, name, read_only, created_at) VALUES (?, ?, ?, ?)`, path, name, readOnly, now.Format(time.RFC3339))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Library{}, ErrLibraryExists
		}
		return Library{}, err
	}
	id, _ := res.LastInsertId()
	return Library{ID: id, Path: path, Name: name, ReadOnly: readOnly, Created: now}, nil
}

// within reports whether path is inside folder.
func within(path, folder string) bool {
	rel, err := filepath.Rel(folder, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Store) UpdateLibrary(id int64, name string, readOnly bool) error {
	_, err := s.db.Exec(`UPDATE libraries SET name = ?, read_only = ? WHERE id = ?`, name, readOnly, id)
	return err
}

func (s *Store) RemoveLibrary(id int64) error {
	_, err := s.db.Exec(`DELETE FROM libraries WHERE id = ?`, id)
	return err
}

// ---- helpers ----

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) tx(ctx context.Context, work func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := work(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Snapshot writes a consistent copy of the database to a new file, while it is in use.
func (s *Store) Snapshot(ctx context.Context, path string) error {
	os.Remove(path)
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}

// Restore replaces the content of the database with a copy made by Snapshot, while the app
// runs, and brings its tables up to date.
func (s *Store) Restore(ctx context.Context, path string) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	err = conn.Raw(func(c any) error {
		raw, ok := c.(sqlite3driver.Conn)
		if !ok {
			return errors.New("the database driver cannot restore")
		}
		return raw.Raw().Restore("main", "file:"+filepath.ToSlash(path)+"?mode=ro")
	})
	conn.Close()
	if err != nil {
		return err
	}
	return s.migrate()
}

// Check reports whether a file is a database of Completist that is not newer than this
// version, before it is restored.
func Check(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var ok string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&ok); err != nil || ok != "ok" {
		return errors.New("damaged database")
	}
	var version int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
		return errors.New("not a database of Completist")
	}
	entries, _ := fs.ReadDir(migrations, "migrations")
	if version > len(entries) {
		return errors.New("made by a newer version")
	}
	return nil
}
