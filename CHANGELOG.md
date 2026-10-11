# Changelog

## Unreleased

- Dates must be days that exist. A ship date, a comment date or an archive record's `archived_on` of the right shape but no calendar day (`2026-02-30`) was accepted, from a hand edit and from `done --on` alike, and later charted wrong; it is now refused when written and when read, naming the file and line. A tracker that already holds one stops loading until that line is fixed by hand.
- A key given twice in one object, in a record, `taskroll.json` or `.taskroll.json`, is refused, naming the file and line. JSON readers keep the last and drop the first silently, and `fmt` or the next write to that file made the loss permanent; `index --check` saw only "not in canonical form". The check adds about 5% to loading a large tracker.

## v0.3.0 (2026-10-11)

Format 1.1, record schema 1. `init` now starts a tracker at format 1.1, which v0.1.0 and v0.1.1 cannot read: make sure everyone runs v0.1.2 or later before creating one. Existing trackers stay at their format until someone runs `taskroll upgrade` and commits it.

- `taskroll upgrade` moves a tracker to the newest format, or `--to` one, and `--dry-run` shows what it would do. It changes the format in `taskroll.json` and nothing else, every other byte included, lists the settings the new format adds with their defaults instead of writing them, and says which releases can read the result. It refuses a downgrade and a tracker newer than its build. A tracker without `taskroll.json` gets one with every setting at its default.
- `taskroll config show` lists every setting with the value in effect, marked `custom` where it differs from the default (so `init`'s file, which spells every default out, shows none), and which need a newer format; `--json` also says whether the file sets each key.
- `init` writes every setting at its default, as it said it did: `browse_url`, `fiscal_year_start`, the epic's `legend`, `fields`, `markers` and `close_titles`, and `collections` were left out. A new tracker is therefore format 1.1, which v0.1.0 and v0.1.1 cannot read.

## v0.2.0 (2026-10-11)

Format 1.1, record schema 1. A tracker that sets `fiscal_year_start` is format 1.1: v0.1.2 reads it with a warning, and v0.1.0 and v0.1.1 cannot read it, so upgrade every machine to v0.1.2 or later first. Trackers that do not set it stay format 1.

- `burndown --group-by day|week|month`: weeks are labelled by their Monday and months as `YYYY-MM`, and a week or month with nothing done still gets a row, so a stall shows.
- `burndown --period month|quarter|fy|last-month|last-quarter|last-fy` charts one reporting window, and `--since`/`--until` any other. Within a window `CUM` and the bar count from its start, the footer gives the window's figure beside the overall one, and `LEFT` stays the whole backlog.
- `fiscal_year_start` in `taskroll.json` (format 1.1) sets the month the financial year opens, which quarters and years count from; `--fy-start` overrides it for a run. A tracker that does not set it stays format 1.
- `burndown` reads the tracker once instead of twice, about twice as fast on a large one.
- `burndown` refuses a ship date of the right shape that is no calendar day (`2026-02-30`), naming the task, instead of charting it: records check only the shape, and in a week series such a date filled every week since year 1.
- Go API: `Burndown` and `RenderBurndown` take `BurndownOptions`, and `Burndown` returns a `BurndownReport`; `Period` resolves a named window.

## v0.1.2 (2026-10-11)

Format 1.0, record schema 1. Install it on every machine before any tracker declares a format minor such as `"1.1"`: v0.1.0 and v0.1.1 read only a whole-number format.

- Built with Go 1.26.9, which fixes GO-2026-6603, -6607, -6608, -6609, -6611, -6612, -6613 and -6617 in the standard library's `net/http`, `crypto/tls` and `net/textproto` (reachable from `serve`).
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
