# Completist

**The ROM library manager that tells you what's missing.**

Completist compares your ROMs with the No-Intro DATs and shows, system by system, what you have,
what is missing and which files are damaged, duplicated or unknown. It runs on your NAS, your
Unraid server or your computer, as one small program with its database inside.

[Español](README.es.md)

![Home: how complete the collection is, one tile per game](docs/screenshots/home.png)

> Completist is in its first version (phase 0 of the [plan](#roadmap)): cartridge systems,
> DATs, the collection and its counts. Disc systems, compression and renaming come next.

## What it does

- **Tells you what is missing.** Every system shows how many games you have, how many there are
  and which ones are missing, with the missing ones in place in the grid, greyed out.
- **Counts your way.** One version of each game (any region or revision counts), every official
  release, or the whole DAT with prototypes and homebrew. A default for all systems, and a choice
  for each one.
- **Finds games by their content, not their name.** Files are identified by their checksums, so a
  badly named file is still found. Copier headers (NES, SNES, Lynx, Atari 7800, PC Engine) and the
  byte orders of Nintendo 64 dumps are handled. Zip files are read too.
- **Shows what to review**: files with the name of a game but other content (bad dumps, hacks,
  damaged files), files not in the DAT, and duplicates.
- **Keeps the DATs up to date**: downloaded from libretro-database and updated every week, or
  imported from your own DAT files (Logiqx XML or clrmamepro).
- **Looks good**: box art, screenshots, a page for each game with its versions and regions, a
  dark interface that works on a phone, in English and Spanish.
- **Stays up to date**: Completist notices when files are added, removed or replaced and scans
  again; network shares are checked every two minutes. Unchanged files are not read twice, so
  10,000 files are checked in under a second.
- **Tells you the news** through Discord, Telegram or any webhook: games new in your collection,
  sets you complete, damaged files found and new entries in the DATs.
- **Keeps a backup** of the libraries, DATs, settings and users every week, and keeps the last five;
  a backup can also be downloaded and restored from the settings.
- **Built for a server**: user accounts (administrator or read only), protection against password
  guessing, live tasks, a JSON API, read-only libraries and a Docker image for amd64 and arm64.

| | |
| --- | --- |
| ![A system: covers, the missing games greyed out](docs/screenshots/system.png) | ![A game: its versions and your files](docs/screenshots/game.png) |

The screenshots were taken without access to the cover server, so they show the covers
Completist draws when a game has none.

## Systems

Cartridge systems with No-Intro DATs: NES, Famicom Disk System, Super Nintendo, Nintendo 64,
Virtual Boy, Game Boy, Game Boy Color, Game Boy Advance, Nintendo DS, Pokémon Mini, SG-1000,
Master System, Mega Drive, 32X, Game Gear, PC Engine, Atari 2600, Atari 7800, Lynx, Neo Geo Pocket
(and Color), WonderSwan (and Color), ColecoVision, Intellivision and Vectrex.

## Quick start

### Docker

```sh
docker run -d --name completist --restart unless-stopped \
  -p 3000:3000 \
  -e PUID=1000 -e PGID=1000 -e TZ=Europe/Madrid \
  -v /path/to/appdata:/data \
  -v /path/to/your/roms:/roms \
  delfus77/completist
```

Open http://localhost:3000. The folder mounted on `/roms` is added as a library on the first
start. Or use [docker-compose.yml](docker-compose.yml).

| Setting | Meaning |
| --- | --- |
| `/data` | The database and the settings |
| `/roms` | Your ROMs. Add `:ro` to mount it read only: Completist only reads them |
| `PUID`, `PGID` | The user and group that own your files (`id` on the host). The app never runs as root |
| `TZ` | Your time zone |

### Unraid

Install the template by hand until it is in Community Applications: in a terminal on the server

```sh
wget -O /boot/config/plugins/dockerMan/templates-user/my-completist.xml \
  https://raw.githubusercontent.com/DeLFuS77/completist/main/templates/completist.xml
```

then **Docker > Add Container** and pick `completist`. Set **ROMs** to your share.

### Windows, macOS and Linux

Build it (see [Building](#building)) and run `completist`. It keeps its data in a `data` folder
next to the program, or where `COMPLETIST_DATA_DIR` says. Open http://localhost:3000.

## First start

1. Add the folder with your ROMs in **Settings > Libraries**. Subfolders named after a system
   (`snes`, `Mega Drive`, `gba`...) tell Completist what each file is; otherwise the extension
   does. A file is found by its content anyway once its DAT is there.
2. Download the DATs of your systems: one click on the home page, or choose them in
   **Settings > DATs**.
3. Your collection appears, system by system.

## How games are counted

| Mode | Counts |
| --- | --- |
| One version of each game | Each game once, whatever its region or revision. Betas, demos, prototypes, unlicensed games, hacks and BIOS files are left out |
| Every official release | Each region and revision of the official games on its own |
| Every entry of the DAT | The whole set |

For one version of each game, versions are grouped by their title in the DAT, and the preferred
regions decide which one is shown when you have none.

## Users

Without users, anyone on your local network can use Completist, and it does not answer from
outside it. Create an administrator in **Settings > Users** to require a login, then add more
users, administrators or read only. Passwords are stored as PBKDF2 hashes; failed logins are slowed
down and then blocked.

`COMPLETIST_AUTH_USERNAME` and `COMPLETIST_AUTH_PASSWORD` set an administrator from the
environment, useful when a password was forgotten. Use HTTPS (a reverse proxy) when the app can
be reached from the internet.

## Settings in the environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `COMPLETIST_DATA_DIR` | `data` next to the program, `/data` in Docker | The database and settings |
| `COMPLETIST_PORT` | `3000` | Port of the web interface |
| `COMPLETIST_LIBRARIES` | | Folders added as libraries on the first start, separated by commas |
| `COMPLETIST_WATCH` | `true` | Scan when files change; `false` turns it off |
| `COMPLETIST_WATCH_INTERVAL_MINUTES` | `2` | How often the folders are checked for changes, for network shares that do not report them |
| `COMPLETIST_SCAN_INTERVAL_MINUTES` | `360` | A full scan as a safety net; `0` turns it off |
| `COMPLETIST_AUTO_BACKUP` | `true` | A copy of the state every week; `false` turns it off |
| `COMPLETIST_ALLOWED_HOSTS` | | More host names the app answers to while no user exists |
| `COMPLETIST_DAT_UPDATE_HOURS` | `168` | How often the downloaded DATs are updated; `0` turns it off |
| `COMPLETIST_SCAN_WORKERS` | up to 4 | Files read at the same time |
| `COMPLETIST_DAT_URL` | libretro-database on GitHub | Where the DATs are downloaded from, for a mirror |
| `COMPLETIST_AUTH_USERNAME`, `COMPLETIST_AUTH_PASSWORD` | | An administrator |
| `COMPLETIST_ALLOW_REMOTE_WITHOUT_LOGIN` | | `true` when another service protects the app |
| `COMPLETIST_TRUSTED_PROXIES` | | `cloudflare`, or proxy addresses, for the real address of visitors |

## API

The interface uses a JSON API that scripts can use too, with HTTP basic authentication when a login
is required (a read-only user is enough to read).

| Endpoint | Description |
| --- | --- |
| `GET /api/overview` | The summary and the progress of every system |
| `GET /api/systems/{id}/games?filter=missing` | The games of a system: `have`, `missing` or all |
| `GET /api/games/{id}` | A game, its versions and your files |
| `POST /api/scan` | Scan the libraries |
| `POST /api/dats/sync` | Download the DATs |
| `GET /api/tasks/events` | Running tasks, as server-sent events |
| `GET /healthz` | Health check, without login |

## Roadmap

1. **Phase 0, now**: the core, cartridge systems, DATs, counting modes, the new interface.
2. Covers stored by the server, statistics, a wishlist, and exports to ES-DE and RetroArch.
3. Disc systems: PlayStation, Saturn, Mega CD, Dreamcast, GameCube and Wii, with CHD and RVZ
   files identified by the checksums of their tracks.
4. Compression checked against the DAT before an original is deleted, renaming to the DAT names
   with undo, and the Switch plugin from Switch Library Manager Web.

## Building

Requirements: [Go](https://go.dev) 1.26 or newer and [Node.js](https://nodejs.org) 22 or newer.

```sh
git clone https://github.com/DeLFuS77/completist.git
cd completist
make ui      # the interface (Svelte, Tailwind, esbuild)
make build   # build/completist, with the interface inside
make test
```

`make dev` rebuilds the interface on every change and serves it without building Go again.

## License

[GNU AGPL 3.0](LICENSE). See [NOTICE](NOTICE.md) for the components and data it uses.
Completist contains no games and never downloads them: use it with backups of games you own.
