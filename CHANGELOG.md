# Changelog

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
