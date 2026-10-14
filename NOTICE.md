# Notices

Completist is free software under the GNU Affero General Public License 3.0 (see
[LICENSE](LICENSE)). Its source code is at https://github.com/DeLFuS77/completist.

## Code from other projects of the author

The task log, the user accounts and login protection, the security headers and the Docker
entrypoint are adapted from [Switch Library Manager Web](https://github.com/DeLFuS77/switch-library-manager-web),
written by the same author. Completist contains no code of the projects that one was forked from.

## Included in the binary

| Component | License |
| --- | --- |
| [Svelte](https://svelte.dev) (runtime in the interface) | MIT |
| [Tailwind CSS](https://tailwindcss.com) (generated styles) | MIT |
| [go-sqlite3](https://github.com/ncruces/go-sqlite3) by Nuno Cruces, with [SQLite](https://sqlite.org) compiled to WebAssembly | MIT; SQLite is in the public domain |
| [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys) | BSD-3-Clause |
| [Unbounded](https://github.com/googlefonts/unbounded) and [Figtree](https://github.com/erikdkennedy/figtree) typefaces | SIL Open Font License 1.1 ([Unbounded](ui/static/fonts/OFL-Unbounded.txt), [Figtree](ui/static/fonts/OFL-Figtree.txt)) |

esbuild (MIT) builds the interface but is not part of it.

The CHD reader (`internal/chd`) is written from [libchdr](https://github.com/rtissera/libchdr)
(BSD 3-Clause) and the [MAME](https://github.com/mamedev/mame) sources, the LZMA decoder from the
LZMA specification by Igor Pavlov (public domain) and the FLAC decoder from the FLAC format
specification. Its test images were made with chdman.

## Data used at run time

- **DATs** are downloaded from [libretro-database](https://github.com/libretro/libretro-database)
  (CC BY-SA 4.0), which mirrors the DATs of [No-Intro](https://no-intro.org) and
  [Redump](http://redump.org). They are not included in the binary or the image.
- **Box art, screenshots and title screens** are downloaded from
  [libretro-thumbnails](https://github.com/libretro-thumbnails/libretro-thumbnails) the first time
  a page shows them, and kept in the data folder. They are not included either.

## No games

Completist contains no games, BIOS files or other copyrighted content, and never downloads them.
Use it only with backups of games you own.
