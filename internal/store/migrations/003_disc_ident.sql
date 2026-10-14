-- GameCube and Wii images that keep only what the game needs (RVZ, WIA, WBFS) are matched by
-- the code in their disc header: game, disc and version, as "GALE:0:1"
ALTER TABLE roms ADD COLUMN ident TEXT NOT NULL DEFAULT '';
ALTER TABLE files ADD COLUMN ident TEXT NOT NULL DEFAULT '';
CREATE INDEX roms_ident ON roms(ident) WHERE ident != '';
