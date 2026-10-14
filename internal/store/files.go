package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// File is a game file found in a library.
type File struct {
	ID        int64  `json:"id"`
	LibraryID int64  `json:"libraryId"`
	Path      string `json:"path"`
	Entry     string `json:"entry,omitempty"`
	Stem      string `json:"-"`
	Size      int64  `json:"size"`
	Mtime     int64  `json:"mtime"`
	System    string `json:"system"`
	CRC       string `json:"crc"`
	MD5       string `json:"md5"`
	SHA1      string `json:"sha1"`
	AltForm   string `json:"altForm,omitempty"`
	AltSize   int64  `json:"-"`
	AltCRC    string `json:"-"`
	AltSHA1   string `json:"-"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	GameID    int64  `json:"gameId,omitempty"`
}

// Key identifies a file and the entry inside it.
type Key struct {
	Path  string
	Entry string
}

// Cached is what is known of a file from an earlier scan.
type Cached struct {
	Size  int64
	Mtime int64
	Ok    bool // its checksums were read
}

// KnownFiles returns the files of a library from earlier scans, so unchanged files are not
// read again.
func (s *Store) KnownFiles(ctx context.Context, libraryID int64) (map[Key]Cached, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path, entry, size, mtime, status FROM files WHERE library_id = ?`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[Key]Cached{}
	for rows.Next() {
		var k Key
		var c Cached
		var status string
		if err := rows.Scan(&k.Path, &k.Entry, &c.Size, &c.Mtime, &status); err != nil {
			return nil, err
		}
		c.Ok = status != "error"
		out[k] = c
	}
	return out, rows.Err()
}

// SaveFiles adds or replaces files read by a scan.
func (s *Store) SaveFiles(ctx context.Context, files []File) error {
	if len(files) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return s.tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO files (library_id, path, entry, stem, size, mtime, system, crc, md5, sha1, alt_form, alt_size, alt_crc, alt_sha1, status, error, scanned_at, first_seen)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(path, entry) DO UPDATE SET library_id = excluded.library_id, stem = excluded.stem, size = excluded.size, mtime = excluded.mtime,
				system = excluded.system, crc = excluded.crc, md5 = excluded.md5, sha1 = excluded.sha1, alt_form = excluded.alt_form,
				alt_size = excluded.alt_size, alt_crc = excluded.alt_crc, alt_sha1 = excluded.alt_sha1, status = excluded.status,
				error = excluded.error, scanned_at = excluded.scanned_at, rom_id = NULL`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, f := range files {
			if _, err := stmt.ExecContext(ctx, f.LibraryID, f.Path, f.Entry, f.Stem, f.Size, f.Mtime, f.System, f.CRC, f.MD5, f.SHA1,
				f.AltForm, f.AltSize, f.AltCRC, f.AltSHA1, f.Status, f.Error, now, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveFiles deletes files that are gone from a library.
func (s *Store) RemoveFiles(ctx context.Context, libraryID int64, keys []Key) error {
	if len(keys) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `DELETE FROM files WHERE library_id = ? AND path = ? AND entry = ?`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, k := range keys {
			if _, err := stmt.ExecContext(ctx, libraryID, k.Path, k.Entry); err != nil {
				return err
			}
		}
		return nil
	})
}

// FilesOfGame returns the files that are a game of a DAT.
func (s *Store) FilesOfGame(ctx context.Context, gameID int64) ([]File, error) {
	return s.queryFiles(ctx, `WHERE f.rom_id IN (SELECT id FROM roms WHERE game_id = ?) ORDER BY f.path`, gameID)
}

// FilesWithStatus returns the files of a system with a status, such as the unknown ones.
func (s *Store) FilesWithStatus(ctx context.Context, system string, status string, limit int) ([]File, error) {
	return s.queryFiles(ctx, `WHERE f.system = ? AND f.status = ? ORDER BY f.path LIMIT ?`, system, status, limit)
}

func (s *Store) queryFiles(ctx context.Context, where string, args ...any) ([]File, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, f.library_id, f.path, f.entry, f.size, f.mtime, f.system, f.crc, f.md5, f.sha1, f.alt_form, f.status, f.error,
		COALESCE((SELECT r.game_id FROM roms r WHERE r.id = f.rom_id), 0) FROM files f `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.LibraryID, &f.Path, &f.Entry, &f.Size, &f.Mtime, &f.System, &f.CRC, &f.MD5, &f.SHA1, &f.AltForm, &f.Status, &f.Error, &f.GameID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SystemFileCounts counts the files of every system by status, and the duplicates: files
// that are the same game as another file.
type SystemFileCounts struct {
	Files      int   `json:"files"`
	Ok         int   `json:"ok"`
	Bad        int   `json:"bad"`
	Unknown    int   `json:"unknown"`
	NoDat      int   `json:"noDat"`
	Errors     int   `json:"errors"`
	Duplicates int   `json:"duplicates"`
	Bytes      int64 `json:"bytes"`
}

func (s *Store) FileCounts(ctx context.Context) (map[string]*SystemFileCounts, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT system, status, COUNT(*), COALESCE(SUM(size), 0) FROM files GROUP BY system, status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*SystemFileCounts{}
	for rows.Next() {
		var system, status string
		var n int
		var bytes int64
		if err := rows.Scan(&system, &status, &n, &bytes); err != nil {
			return nil, err
		}
		c := out[system]
		if c == nil {
			c = &SystemFileCounts{}
			out[system] = c
		}
		c.Files += n
		c.Bytes += bytes
		switch status {
		case "ok":
			c.Ok += n
		case "bad":
			c.Bad += n
		case "unknown":
			c.Unknown += n
		case "nodat":
			c.NoDat += n
		case "error":
			c.Errors += n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	dups, err := s.db.QueryContext(ctx, `SELECT g.system, COUNT(*) - COUNT(DISTINCT r.game_id) FROM files f JOIN roms r ON r.id = f.rom_id JOIN games g ON g.id = r.game_id GROUP BY g.system`)
	if err != nil {
		return nil, err
	}
	defer dups.Close()
	for dups.Next() {
		var system string
		var n int
		if err := dups.Scan(&system, &n); err != nil {
			return nil, err
		}
		if c := out[system]; c != nil {
			c.Duplicates = n
		}
	}
	return out, dups.Err()
}

// GameRow is a DAT game with how many of its files are in the libraries.
type GameRow struct {
	ID        int64
	Name      string
	Title     string
	Regions   string
	Languages string
	Flags     int64
	Revision  int
	CloneOf   string
	Have      int
}

// Games returns every game of a system's DAT with the count of matching files.
func (s *Store) Games(ctx context.Context, system string) ([]GameRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.id, g.name, g.title, g.regions, g.languages, g.flags, g.revision, g.clone_of,
		(SELECT COUNT(*) FROM files f JOIN roms r ON r.id = f.rom_id WHERE r.game_id = g.id)
		FROM games g WHERE g.system = ? ORDER BY g.name`, system)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GameRow{}
	for rows.Next() {
		var g GameRow
		if err := rows.Scan(&g.ID, &g.Name, &g.Title, &g.Regions, &g.Languages, &g.Flags, &g.Revision, &g.CloneOf, &g.Have); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Game returns one game of a DAT.
func (s *Store) Game(ctx context.Context, id int64) (GameRow, string, error) {
	var g GameRow
	var system string
	err := s.db.QueryRowContext(ctx, `SELECT g.id, g.name, g.title, g.regions, g.languages, g.flags, g.revision, g.system,
		(SELECT COUNT(*) FROM files f JOIN roms r ON r.id = f.rom_id WHERE r.game_id = g.id)
		FROM games g WHERE g.id = ?`, id).Scan(&g.ID, &g.Name, &g.Title, &g.Regions, &g.Languages, &g.Flags, &g.Revision, &system, &g.Have)
	return g, system, err
}

// RomOfGame is a file a DAT lists for a game.
type RomOfGame struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	CRC  string `json:"crc"`
	SHA1 string `json:"sha1"`
}

func (s *Store) RomsOfGame(ctx context.Context, gameID int64) ([]RomOfGame, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, size, crc, sha1 FROM roms WHERE game_id = ? ORDER BY name`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RomOfGame{}
	for rows.Next() {
		var r RomOfGame
		if err := rows.Scan(&r.Name, &r.Size, &r.CRC, &r.SHA1); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GameExists tells if a DAT lists a game of that name, and the name of its parent.
func (s *Store) GameExists(ctx context.Context, system, name string) (bool, string, error) {
	var cloneOf string
	err := s.db.QueryRowContext(ctx, `SELECT clone_of FROM games WHERE system = ? AND name = ? LIMIT 1`, system, name).Scan(&cloneOf)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	return err == nil, cloneOf, err
}
