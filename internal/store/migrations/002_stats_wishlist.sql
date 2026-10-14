-- when a file was first found, for the games added lately
ALTER TABLE files ADD COLUMN first_seen TEXT NOT NULL DEFAULT '';
UPDATE files SET first_seen = scanned_at;

-- how complete each system was, one row per day and system: the last count of the day
CREATE TABLE history (
	day    TEXT NOT NULL,
	system TEXT NOT NULL,
	have   INTEGER NOT NULL,
	total  INTEGER NOT NULL,
	files  INTEGER NOT NULL,
	bytes  INTEGER NOT NULL,
	PRIMARY KEY (day, system)
);

-- games you want, by their name in the DAT, so they stay when a DAT is updated
CREATE TABLE wishlist (
	system   TEXT NOT NULL,
	name     TEXT NOT NULL,
	added_at TEXT NOT NULL,
	PRIMARY KEY (system, name)
);
