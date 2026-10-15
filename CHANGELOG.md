# Changelog

## 0.6.0 (unreleased)

Space, fixdats and a light theme.

- Compress PSP ISO files into CSO, and GameCube and Wii ISO files into GCZ (the lossless format of
  Dolphin), from the Organize page. Blocks are compressed on every core; each image is read back
  whole and compared with the ISO before the ISO is removed, and the change can be undone.
- GCZ and CSO files are no longer read again on every scan.
- A Space page: what each system takes on the disk by format, the space in duplicates, in files
  not in the DATs and in damaged files, and what could be compressed, with a link to do it.
- A fixdat of the missing games of each system, in its counting mode, as a Logiqx DAT.
- A light theme. It follows the system, or a choice kept on the device in Settings > Account.
- RVZ is not written: it needs a Zstandard encoder and the Wii keys. GCZ is offered instead,
  and Dolphin converts it to RVZ if wanted.

## 0.5.0 (unreleased)

Organizing the files.

- Rename verified files to the names of their DATs: a zip or a CHD after its game, the tracks
  of a disc after its first track, with the CUE sheet rewritten to match.
- Pack cartridge dumps in zip files, one per game, each read back and checked against the DAT
  before its original is removed.
- Turn BIN/CUE dumps and ISO files of disc systems into CHD images, written by Completist itself
  (deflate, as chdman's cdzl and zlib codecs, read by every emulator), read back file by file and
  checked against the DAT before the originals are removed; undoing it writes the originals again.
- Undoing never removes the only copy: a zip or a CHD made while keeping the originals stays
  when the originals are gone, and what could not be undone can be tried again.
- Damaged or crafted CHD, GCZ and CSO files can no longer make a scan use too much memory.
- Every change is shown before it is made, runs as a task, is kept in a list and can be undone.
  Read-only libraries are left alone.

## 0.4.0 (unreleased)

The disc systems.

- PlayStation, Saturn and Mega CD, with the Redump DATs of libretro-database.
- A disc is found by its first track: a BIN file, or a CHD, read by Completist itself (LZMA,
  deflate and FLAC hunks), so no chdman is needed. The other tracks of a disc are not listed as
  files to review.
- A game on several discs is one game, complete when every disc is there; the grid tells how
  many discs you have.
- Exports point to the CUE sheet, the GDI file or the CHD of each disc.
- Dreamcast, PlayStation 2, PSP, GameCube and Wii. CHD images of GD-ROMs, DVDs and UMDs, GDI
  dumps, and GCZ and CSO images are read back as the original; RVZ, WIA and WBFS images are
  recognised by the game code in their disc header.
- Only the tracks the DATs list are read: the CUE and GDI files tell which ones are audio.

## 0.3.0 (unreleased)

The end of phase 1, and the statistics, the wishlist and the exports.

- Box art, screenshots and title screens are downloaded by the server once and kept in the data
  folder, so pages load at once and work without internet. A clone without its own image shows
  its parent's. The settings of the libraries show how much space they take and can remove them.
- Box art can be kept as it comes, as a JPEG of high quality (about a quarter of the space, no
  difference to see) or as a smaller JPEG; the images already kept are converted. Screenshots,
  title screens and box art with transparent parts always stay as they come.
- Statistics: how complete the collection was day by day, by maker and by region, the systems
  closest to complete, the space of each one and the games added lately.
- A wishlist: add a missing game from its page; it is marked in the grid of its system, and a
  notification can tell you when it reaches your collection.
- Exports to RetroArch playlists and ES-DE gamelists, in the settings, with the paths of the
  device that plays the games.
- The empty activity log no longer breaks the settings of the users.
- Notification messages are in the language of the interface until you choose one.

## 0.2.0 (unreleased)

The rest of phase 0.

- Libraries are scanned again when their files change; network shares are checked every two
  minutes.
- Notifications through Discord, Telegram or a webhook: games new in the collection, complete
  sets, damaged files and new entries in the DATs.
- Backups of the database, users and activity log: one every week, the last five kept, and
  downloaded or restored from the settings while the app runs.
- An activity log of who changed what, in the settings of the users.

## 0.1.0 (unreleased)

The first version: phase 0 of the plan.

- Libraries: folders of ROMs, read only if you want; files in zip archives are read too.
- Files identified by their checksums against the DATs, with copier headers and the byte orders
  of Nintendo 64 dumps handled; unchanged files are not read again.
- DATs downloaded from libretro-database (No-Intro) and updated every week, or imported from
  Logiqx XML and clrmamepro files.
- How complete each system is, counted as one version of each game, every official release or
  every entry of the DAT, for all systems or each one.
- Files to review: damaged or modified dumps, files not in the DAT, duplicates.
- A new interface: the collection as a mosaic, a grid of covers with the missing games in place,
  a page for each game, live tasks, settings; in English and Spanish.
- User accounts, administrator or read only, with the login protection of Switch Library
  Manager Web.
- One binary with SQLite inside, a Docker image for amd64 and arm64, and an Unraid template.
