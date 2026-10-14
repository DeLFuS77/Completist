# Contributing to Completist

Completist is a self-hosted web app that compares a ROM collection with the No-Intro DATs and
shows, system by system, what is owned, missing, damaged or duplicated. Its generic code (task
log, users and login protection, security headers, Docker entrypoint) comes from Switch Library
Manager Web, by the same author.

Issues and pull requests are welcome. Discuss larger changes in an issue first.

## Choices of the project

- Name Completist, subtitle "ROM library manager"; AGPL-3.0; public, for the community.
- First systems: cartridges. No in-browser play: management only.
- Counting modes: one version of each game (1G1R, default), every official release, the whole
  DAT; per system if wanted.
- Look: Steam/Netflix style, dark; the home page leads with how complete the collection is.
- Target: NAS and Unraid with Docker; one Go binary with SQLite inside.

## Layout

- `cmd/completist`: main, environment settings, scheduler (rescans, weekly DAT updates).
- `internal/systems`: the known systems (DAT names, folders, extensions, headers).
- `internal/dat`: DAT parsers (Logiqx XML, clrmamepro) and No-Intro name tags.
- `internal/platform`: the plugin interface; `cartridge` hashes dumps (headers, N64 byte order),
  `disc` the first track of CD images.
- `internal/chd`: reads CHD v5 CD, GD-ROM and DVD images (LZMA, deflate, FLAC and Huffman hunks)
  without chdman.
- `internal/discimage`: GCZ and CSO read back as ISO; the disc header of RVZ, WIA and WBFS.
- `internal/store`: SQLite (ncruces/go-sqlite3, no cgo), migrations, DAT import, matching.
- `internal/scanner`: walks libraries, hashes new or changed files (zip too), in batches.
- `internal/collection`: progress per system and the three modes.
- `internal/datsync`: downloads DATs from libretro-database.
- `internal/watcher`: scans again when the library folders change.
- `internal/notify`: Discord, Telegram and webhook messages about the collection.
- `internal/backup`: weekly and manual backups, restored while the app runs.
- `internal/activity`: who changed what.
- `internal/auth`: users, sessions, login limiter, CSRF and host checks.
- `internal/server`: the JSON API and the embedded interface.
- `ui/`: Svelte 5 + Tailwind 4, built by `ui/build.mjs` (esbuild) into `internal/webui/dist`.

## Commands

```sh
make ui      # build the interface (npm install the first time)
make build   # build/completist
make test    # go vet and go test
make dev     # interface rebuilt on change, served from disk
```

## Conventions

- Texts in the interface are English sentences used as translation keys (`t('...')` in
  `ui/src/lib/i18n.svelte.ts`); Spanish is in `ui/src/lib/locales/es.ts`. Server error messages
  are translated the same way, so add them there too.
- Comments and identifiers in English; plain wording, sentence case, no jargon in the interface.
- Every new API route that changes something must be refused to read-only users (see
  `viewerAllowed` in `internal/auth/auth.go`).
- Keep the single binary: no external database, no CGO.

## Roadmap

1. Compression verified against the DAT before deleting originals; renaming to DAT names with
   undo; the Switch plugin from Switch Library Manager Web.

Known limits: 1G1R groups by title when the DAT has no parent/clone links (libretro DATs have
none), so a game released under different titles counts more than once.
