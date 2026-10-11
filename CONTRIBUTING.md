# Contributing

Thanks for helping. Issues and pull requests are welcome; for anything larger than a fix, open an issue first so we can agree on the shape before you spend time on it. Security problems go through [private vulnerability reporting](SECURITY.md), not public issues.

## Building and testing

The Go code needs only Go (the version in `go.mod`).

```sh
go test -race ./...        # unit tests and the end-to-end suite in e2e/
golangci-lint run ./...
```

The end-to-end suite builds the binary and drives it against a copy of `examples/demo`. If your change alters the generated markdown, regenerate the demo with `go run ../../cmd/taskroll index` inside `examples/demo` and commit the result.

Releases keep rendering existing trackers byte for byte (see Compatibility in the README). `testdata/format-1/` is frozen and `TestFormat1RendersUnchanged` renders it with your build: never regenerate it. If your change has to alter what an existing tracker renders, changes what a setting means, or adds a key to the records, it is a new major format: bump `FormatMajor`, keep the old output for trackers on the old one, and add a `testdata/format-N/` fixture beside the old one. A new record schema is always a new major too: only epic records carry the schema, so the format in `taskroll.json` is what tells an old binary that a collection's records changed. Bump `SchemaVersion` and `FormatMajor` together and record the pair in `TestEachRecordSchemaHasItsOwnFormat`.

An optional `taskroll.json` key that changes no generated file, and whose absence keeps today's behaviour, is a new minor instead: bump `FormatMinor` and register the key in `settingsKeySince` with that minor, so a file that sets it must declare the minor and older builds read it with a warning. A minor goes out in a release that bumps the minor version (v0.2.0, not v0.1.2). If you add a command that writes files, add it to `TestNewerMinorSettingsSurviveEveryWrite`. How many old formats a release keeps rendering is decided with format 2, when `taskroll upgrade` arrives.

The web UI lives in `web/` (React, Tailwind CSS, shadcn/ui). Its build is committed in `internal/web/dist/` so that `go install` works without Node, and CI fails if the two disagree:

```sh
cd web && pnpm install && pnpm build    # or pnpm watch while you work
cd ../examples/demo && go run ../../cmd/taskroll serve
```

## Commit messages

Commits follow [Conventional Commits](https://www.conventionalcommits.org), and CI checks every commit that reaches `main` and every pull request title (a squash merge uses the title as the commit):

```
type(scope): summary in the imperative, lower case

Optional body: why the change is needed and what it changes.
```

Types:

| Type | For |
| --- | --- |
| `feat` | a new capability for users |
| `fix` | a bug fix |
| `perf` | faster or leaner, same behaviour |
| `refactor` | restructuring with no change in behaviour |
| `test` | tests only |
| `docs` | documentation only |
| `build` | dependencies, the build, the module |
| `ci` | the CI workflows |
| `chore` | anything else that changes nothing for users |
| `revert` | undoing an earlier commit |

Scopes are optional; use the part of the code the change is about: `core` (the record store, ids, settings, views), `cli`, `merge`, `web`, `e2e`, `demo`, `deps`.

A change that breaks the record format, the command line or the Go API adds `!` after the type or scope, `feat(core)!: ...`, and says what breaks in the body. Before v1.0 such a change bumps the minor version; after it, the major.
