package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/DeLFuS77/completist/internal/dat"
)

// DatInfo describes an imported DAT.
type DatInfo struct {
	System   string    `json:"system"`
	Name     string    `json:"name"`
	Version  string    `json:"version"`
	Source   string    `json:"source"`
	Imported time.Time `json:"imported"`
	Games    int       `json:"games"`
}

func (s *Store) Dats() ([]DatInfo, error) {
	rows, err := s.db.Query(`SELECT system, name, version, source, imported_at, games FROM dats ORDER BY system`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DatInfo{}
	for rows.Next() {
		var d DatInfo
		var imported string
		if err := rows.Scan(&d.System, &d.Name, &d.Version, &d.Source, &imported, &d.Games); err != nil {
			return nil, err
		}
		d.Imported, _ = time.Parse(time.RFC3339, imported)
		out = append(out, d)
	}
	return out, rows.Err()
}

// DatChange is what a new version of a DAT changed.
type DatChange struct {
	System   string `json:"system"`
	Version  string `json:"version"`
	Previous string `json:"previous"`
	Games    int    `json:"games"`
	Added    int    `json:"added"`
	Removed  int    `json:"removed"`
}

// ImportDat replaces the DAT of a system and matches the files again.
func (s *Store) ImportDat(ctx context.Context, system string, d *dat.Dat, source string) (DatChange, error) {
	// merge entries with the same name: their files are alternatives, such as a dump with
	// and without its header
	type merged struct {
		game dat.Game
		info dat.Info
	}
	order := []string{}
	games := map[string]*merged{}

	for _, g := range d.Games {
		if len(g.Roms) == 0 || g.Name == "" {
			continue
		}
		m, ok := games[g.Name]
		if !ok {
			m = &merged{game: dat.Game{Name: g.Name, CloneOf: g.CloneOf, Region: g.Region}, info: dat.ParseName(g.Name)}
			games[g.Name] = m
			order = append(order, g.Name)
		}
		for _, r := range g.Roms {
			if r.CRC == "" && r.SHA1 == "" {
				// listed but never dumped: no file can match it
				continue
			}
			// tags such as (Unl) or (Aftermarket) are sometimes only in the file name
			m.info.Flags |= dat.ParseName(stem(r.Name)).Flags
			if r.Status == "baddump" {
				m.info.Flags |= dat.FlagBadDump
			}
			m.game.Roms = append(m.game.Roms, r)
		}
	}

	// games whose files were all never dumped are left out
	kept := order[:0]
	for _, name := range order {
		if len(games[name].game.Roms) > 0 {
			kept = append(kept, name)
		}
	}
	order = kept

	change := DatChange{System: system, Version: d.Version, Games: len(order)}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		previous := map[string]bool{}
		var oldID int64
		err := tx.QueryRow(`SELECT id, version FROM dats WHERE system = ?`, system).Scan(&oldID, &change.Previous)
		if err == nil {
			rows, err := tx.Query(`SELECT name FROM games WHERE dat_id = ?`, oldID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					rows.Close()
					return err
				}
				previous[name] = true
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			if _, err := tx.Exec(`DELETE FROM dats WHERE id = ?`, oldID); err != nil {
				return err
			}
		} else if err != sql.ErrNoRows {
			return err
		}
		name := d.Name
		if name == "" {
			name = d.Description
		}
		res, err := tx.Exec(`INSERT INTO dats (system, name, version, source, imported_at, games) VALUES (?, ?, ?, ?, ?, ?)`,
			system, name, d.Version, source, time.Now().UTC().Format(time.RFC3339), len(order))
		if err != nil {
			return err
		}
		datID, _ := res.LastInsertId()
		insertGame, err := tx.Prepare(`INSERT INTO games (dat_id, system, name, title, regions, languages, flags, revision, clone_of) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer insertGame.Close()
		insertRom, err := tx.Prepare(`INSERT INTO roms (game_id, name, stem, size, crc, md5, sha1, ident) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer insertRom.Close()
		for _, gameName := range order {
			m := games[gameName]
			regions := m.info.Regions
			if len(regions) == 0 && m.game.Region != "" {
				regions = []string{m.game.Region}
			}
			res, err := insertGame.Exec(datID, system, gameName, m.info.Title, strings.Join(regions, ","), strings.Join(m.info.Languages, ","),
				int64(m.info.Flags), m.info.Revision, m.game.CloneOf)
			if err != nil {
				return err
			}
			gameID, _ := res.LastInsertId()
			for _, r := range m.game.Roms {
				if _, err := insertRom.Exec(gameID, r.Name, Stem(r.Name), r.Size, r.CRC, r.MD5, r.SHA1, discIdent(system, r.Serial, m.info)); err != nil {
					return err
				}
			}
			if previous[gameName] {
				delete(previous, gameName)
			} else if oldID != 0 {
				change.Added++
			}
		}
		if oldID != 0 {
			change.Removed = len(previous)
		}
		// in the same transaction, so files never point to a DAT that is gone
		return rematch(ctx, tx)
	})
	return change, err
}

// RemoveDat deletes the DAT of a system.
func (s *Store) RemoveDat(ctx context.Context, system string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM dats WHERE system = ?`, system); err != nil {
			return err
		}
		return rematch(ctx, tx)
	})
}

// Rematch finds again which DAT entry every file is, from the checksums already known, and
// sets its status. It runs after a scan and after a DAT changes.
func (s *Store) Rematch(ctx context.Context) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		return rematch(ctx, tx)
	})
}

func rematch(ctx context.Context, tx *sql.Tx) error {
	{
		steps := []string{
			// by SHA-1, as is or in its other form
			// the same dump can be in several DATs (Game Boy and Game Boy Color, for one): the DAT
			// of the system the file's folder says wins
			`UPDATE files SET rom_id = (SELECT r.id FROM roms r JOIN games g ON g.id = r.game_id
				WHERE r.sha1 != '' AND (r.sha1 = files.sha1 OR (files.alt_sha1 != '' AND r.sha1 = files.alt_sha1))
				ORDER BY (g.system = files.system) DESC, r.id LIMIT 1)`,
			// by CRC and size, for DATs without SHA-1
			`UPDATE files SET rom_id = (SELECT r.id FROM roms r JOIN games g ON g.id = r.game_id
				WHERE r.sha1 = '' AND ((r.crc = files.crc AND r.size = files.size) OR (files.alt_crc != '' AND r.crc = files.alt_crc AND r.size = files.alt_size))
				ORDER BY (g.system = files.system) DESC, r.id LIMIT 1) WHERE rom_id IS NULL AND crc != ''`,
			// by the code in the disc header, for images that keep only what the game needs
			`UPDATE files SET rom_id = (SELECT r.id FROM roms r JOIN games g ON g.id = r.game_id
				WHERE r.ident = files.ident ORDER BY (g.system = files.system) DESC, r.id LIMIT 1) WHERE rom_id IS NULL AND ident != ''`,
			// a matched file belongs to the system of its DAT, whatever its folder says
			`UPDATE files SET system = (SELECT g.system FROM roms r JOIN games g ON g.id = r.game_id WHERE r.id = files.rom_id) WHERE rom_id IS NOT NULL`,
			`UPDATE files SET status = CASE
				WHEN status = 'error' THEN 'error'
				WHEN rom_id IS NOT NULL THEN 'ok'
				WHEN system = '' THEN 'unknown'
				WHEN NOT EXISTS (SELECT 1 FROM dats d WHERE d.system = files.system) THEN 'nodat'
				WHEN EXISTS (SELECT 1 FROM roms r JOIN games g ON g.id = r.game_id WHERE g.system = files.system AND r.stem = files.stem) THEN 'bad'
				ELSE 'unknown' END`,
		}
		for _, q := range steps {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}
}

// stem is a file name without folder and extension.
func stem(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// Stem is exported for the scanner, so files and DAT entries are compared the same way. The
// first track of a disc compares as the disc: "Game (USA) (Track 1).bin" as "Game (USA)".
func Stem(name string) string {
	s := strings.ToLower(stem(name))
	for _, suffix := range []string{" (track 1)", " (track 01)"} {
		s = strings.TrimSuffix(s, suffix)
	}
	return s
}

var serialCode = regexp.MustCompile(`(?:^|-)([A-Z0-9]{4})(?:-|$)`)

// discIdent is how a GameCube or Wii disc is known by its header: the game code from its
// serial ("DL-DOL-GALE-USA", "RVL-RMCP-EUR"), the disc and the version.
func discIdent(system, serial string, info dat.Info) string {
	if system != "gamecube" && system != "wii" {
		return ""
	}
	m := serialCode.FindStringSubmatch(serial)
	if m == nil {
		return ""
	}
	disc := 0
	if info.Disc > 0 {
		disc = info.Disc - 1
	}
	return fmt.Sprintf("%s:%d:%d", m[1], disc, info.Revision)
}
