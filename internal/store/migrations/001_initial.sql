CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

-- folders that hold games
CREATE TABLE libraries (
	id         INTEGER PRIMARY KEY,
	path       TEXT NOT NULL UNIQUE,
	name       TEXT NOT NULL,
	read_only  INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);

-- one DAT per system: the list of every known good dump
CREATE TABLE dats (
	id          INTEGER PRIMARY KEY,
	system      TEXT NOT NULL UNIQUE,
	name        TEXT NOT NULL,
	version     TEXT NOT NULL DEFAULT '',
	source      TEXT NOT NULL DEFAULT '',
	imported_at TEXT NOT NULL,
	games       INTEGER NOT NULL DEFAULT 0
);

-- the games of a DAT; entries with the same name are merged, their files are alternatives
CREATE TABLE games (
	id        INTEGER PRIMARY KEY,
	dat_id    INTEGER NOT NULL REFERENCES dats(id) ON DELETE CASCADE,
	system    TEXT NOT NULL,
	name      TEXT NOT NULL,
	title     TEXT NOT NULL,
	regions   TEXT NOT NULL DEFAULT '',
	languages TEXT NOT NULL DEFAULT '',
	flags     INTEGER NOT NULL DEFAULT 0,
	revision  INTEGER NOT NULL DEFAULT 0,
	clone_of  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX games_system ON games(system);
CREATE INDEX games_name ON games(system, name);

CREATE TABLE roms (
	id      INTEGER PRIMARY KEY,
	game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
	name    TEXT NOT NULL,
	stem    TEXT NOT NULL,
	size    INTEGER NOT NULL DEFAULT 0,
	crc     TEXT NOT NULL DEFAULT '',
	md5     TEXT NOT NULL DEFAULT '',
	sha1    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX roms_game ON roms(game_id);
CREATE INDEX roms_sha1 ON roms(sha1);
CREATE INDEX roms_crc ON roms(crc, size);
CREATE INDEX roms_stem ON roms(stem);

-- every game file found in the libraries; a file in an archive has the archive as path and
-- its name inside as entry
CREATE TABLE files (
	id         INTEGER PRIMARY KEY,
	library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
	path       TEXT NOT NULL,
	entry      TEXT NOT NULL DEFAULT '',
	stem       TEXT NOT NULL,
	size       INTEGER NOT NULL,
	mtime      INTEGER NOT NULL,
	-- system from the folder or the extension; replaced by the system of the DAT when matched
	system     TEXT NOT NULL DEFAULT '',
	crc        TEXT NOT NULL DEFAULT '',
	md5        TEXT NOT NULL DEFAULT '',
	sha1       TEXT NOT NULL DEFAULT '',
	-- checksums of the file without its header or in another byte order, when it has them
	alt_form   TEXT NOT NULL DEFAULT '',
	alt_size   INTEGER NOT NULL DEFAULT 0,
	alt_crc    TEXT NOT NULL DEFAULT '',
	alt_sha1   TEXT NOT NULL DEFAULT '',
	rom_id     INTEGER REFERENCES roms(id) ON DELETE SET NULL,
	-- ok: matches a DAT entry; bad: its name is in the DAT but its content differs;
	-- unknown: not in the DAT; nodat: no DAT for its system yet; error: could not be read
	status     TEXT NOT NULL DEFAULT 'unknown',
	error      TEXT NOT NULL DEFAULT '',
	scanned_at TEXT NOT NULL,
	UNIQUE(path, entry)
);
CREATE INDEX files_library ON files(library_id);
CREATE INDEX files_rom ON files(rom_id);
CREATE INDEX files_system ON files(system, status);
