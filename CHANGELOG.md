# Changelog

## Unreleased

- The tracker format is `MAJOR.MINOR`. A minor only adds optional settings that change no generated file, so a binary that meets a newer minor of its major reads the tracker, ignores the settings it does not know and warns once a run, instead of refusing it; a newer major still stops it. `"format"` stays a number while the minor is 0 (`init` still writes `1`), and a minor is a string, `"1.2"`. `taskroll version` prints the format as `1.0`.
- A settings key added in a minor must come with that minor declared in `"format"`, so older binaries know to ignore it rather than refuse it.
- No command rewrites `taskroll.json`; a test runs every writing command against a newer-minor file and holds it to that, since ignoring unknown settings is only safe while they cannot be dropped.
- Go API, additive: `Format`, `CurrentFormat`, `FormatMajor`, `FormatMinor`, `Settings.FormatMinor` and `Settings.Version`. `FormatVersion` is deprecated in favour of `FormatMajor`.

## v0.1.1 (2026-10-04)

Format 1, record schema 1. Install it on every machine before a release with format 2 exists: only binaries from this release on can tell you to upgrade.

- Trackers name their format: `taskroll.json` takes `"format"` (absent means 1, and `init` writes it), and `taskroll version` prints the newest format and record schema the binary supports.
- An older binary that meets a newer format or record schema stops before parsing and prints the command that upgrades it, instead of failing on an unknown key. Any other unknown key in the settings, the config or the records now suggests an upgrade too.
- Format 1 output is frozen in `testdata/format-1/` and checked on every build, so upgrading taskroll leaves existing trackers unchanged.

## v0.1.0 (2026-10-04)

First public release. Record schema 1.

- JSONL records as the only source; epic and collection markdown generated from them and gated by `index --check`.
- Commands: `init`, `epic new|list`, `add`, `new`, `status`, `done`, `edit`, `comment`, `list`, `show`, `velocity`, `burndown`, `index`, `archive`, `fmt`, `whoami`, `config user|merge-driver`, `version`.
- `taskroll.json` settings: sizes and points, task-line fields with patterns, markers, epic legend, closing full stops, collections with series, keys, required fields and vocabularies.
- An exclusive write lock around every change, shared by every worktree of a clone; allocation inside it.
- Branch-safe ids (other branches and worktrees counted) and a record-level git merge driver that keeps the losing side of a conflict as a comment. The driver names an in-repository binary relatively and falls back to conflict markers when it is missing.
- `serve`: a local web UI built with React, Tailwind CSS and shadcn/ui (board and list, a details panel with status and comments, new tasks, light and dark themes, an address per item at `/items/<ID>`) behind a loopback-only listener, a per-session token, host, origin and CSRF checks and a strict CSP with a per-page style nonce, writing through the locked store.
- Audit stamps (`created_at`/`created_by`, `closed_at`/`closed_by`) and velocity from them.
- Links to items: every task line in a generated epic file carries an anchor named after its id, and `browse_url` in `taskroll.json` gives each item a permanent link, printed by `show`, carried as `url` in the JSON and copied by the web UI.
- `examples/demo`: a sample project that is both the demo and the fixture of an end-to-end suite (`e2e/`) driving the built binary through the commands, git merges and the web server.
