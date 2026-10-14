package store

import (
	"context"
	"database/sql"
	"time"
)

// ---- history ----

// HistoryRow is how complete a system was on a day.
type HistoryRow struct {
	Day    string `json:"day"`
	System string `json:"system"`
	Have   int    `json:"have"`
	Total  int    `json:"total"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

// SaveHistory replaces the rows of a day: the last count of the day is the one kept. Systems
// not in rows are removed from that day, so a removed DAT stops counting.
func (s *Store) SaveHistory(ctx context.Context, day string, rows []HistoryRow) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM history WHERE day = ?`, day); err != nil {
			return err
		}
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx, `INSERT INTO history (day, system, have, total, files, bytes) VALUES (?, ?, ?, ?, ?, ?)`,
				day, r.System, r.Have, r.Total, r.Files, r.Bytes); err != nil {
				return err
			}
		}
		return nil
	})
}

// History returns the rows from a day on, oldest first.
func (s *Store) History(ctx context.Context, since string) ([]HistoryRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT day, system, have, total, files, bytes FROM history WHERE day >= ? ORDER BY day, system`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryRow{}
	for rows.Next() {
		var r HistoryRow
		if err := rows.Scan(&r.Day, &r.System, &r.Have, &r.Total, &r.Files, &r.Bytes); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- wishlist ----

// Wish is a game you want.
type Wish struct {
	System string `json:"system"`
	Name   string `json:"name"`
	Added  string `json:"added"`
}

func (s *Store) Wishes(ctx context.Context) ([]Wish, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT system, name, added_at FROM wishlist ORDER BY added_at DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Wish{}
	for rows.Next() {
		var w Wish
		if err := rows.Scan(&w.System, &w.Name, &w.Added); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) AddWish(ctx context.Context, system, name string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO wishlist (system, name, added_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
		system, name, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) RemoveWish(ctx context.Context, system, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wishlist WHERE system = ? AND name = ?`, system, name)
	return err
}

// ---- games added lately ----

// Recent is a game of a DAT and when its first file was found.
type Recent struct {
	GameID int64  `json:"id"`
	System string `json:"system"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Since  string `json:"since"`
}

// RecentGames returns the games whose files were found last.
func (s *Store) RecentGames(ctx context.Context, limit int) ([]Recent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.id, g.system, g.name, g.title, MIN(f.first_seen) AS since
		FROM files f JOIN roms r ON r.id = f.rom_id JOIN games g ON g.id = r.game_id
		GROUP BY g.id ORDER BY since DESC, g.name LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recent{}
	for rows.Next() {
		var r Recent
		if err := rows.Scan(&r.GameID, &r.System, &r.Name, &r.Title, &r.Since); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- exports ----

// Matched is a file that is a game of a DAT, with what an export needs.
type Matched struct {
	System  string
	Library string
	Path    string
	Entry   string
	Name    string
	Title   string
	CRC     string
}

// MatchedFiles returns the files that are games of a DAT, by system and name.
func (s *Store) MatchedFiles(ctx context.Context) ([]Matched, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.system, l.path, f.path, f.entry, g.name, g.title, r.crc
		FROM files f JOIN roms r ON r.id = f.rom_id JOIN games g ON g.id = r.game_id JOIN libraries l ON l.id = f.library_id
		WHERE f.status = 'ok' ORDER BY g.system, g.name, f.path, f.entry`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Matched{}
	for rows.Next() {
		var m Matched
		if err := rows.Scan(&m.System, &m.Library, &m.Path, &m.Entry, &m.Name, &m.Title, &m.CRC); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
