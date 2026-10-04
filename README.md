# taskroll

A work tracker kept in git. Epics, tasks and entries are JSONL records in the repository; the markdown people read is generated from them; every change goes through the CLI, which validates it, stamps who made it and when, and regenerates the views under a lock. It is built for a team of people and AI agents working in one repository, often in parallel branches and worktrees.

## Try it

The repository carries a sample project, a fictional storefront with three epics, work in every status, dependencies, comments, a debt list and some ideas:

```sh
git clone https://github.com/hvish/taskroll && cd taskroll
go run ./cmd/taskroll --help
cd examples/demo
go run ../../cmd/taskroll list --ready
go run ../../cmd/taskroll serve      # open the printed URL
```

Changes you make there are ordinary file changes; `git checkout -- examples` puts the demo back.

## Why records, not markdown

Hand-edited markdown task lists drift: two branches renumber the same task, a status changes in one file and not in the summary that copies it, and an agent rewriting a table drops a row nobody notices. taskroll keeps one source and derives everything else from it:

- **Records are the only source.** Each item is one JSON object on one line, keys in a fixed order, so a change is a one-line diff and a merge is per record.
- **Only the CLI writes them.** Every write validates against a published JSON Schema, refuses unknown keys, stamps who and when, and regenerates the views before releasing the lock.
- **Views are generated and gated.** `taskroll index --check` fails CI when a generated file differs from what the records produce, so a hand edit cannot survive review.
- **The core knows nothing about your project.** Sizes, fields, markers and collections come from `taskroll.json`; extra views plug in through hooks.

## Start a tracker in a repository

```sh
go install github.com/hvish/taskroll/cmd/taskroll@latest   # or build it: go build ./cmd/taskroll
cd your-repo
taskroll init                       # writes .taskroll.json and docs/tasks/ (use --dir for elsewhere)
taskroll config user 'Your Name'    # optional; defaults to git config user.name
git add .taskroll.json .gitattributes docs/tasks && git commit -m 'Start taskroll'
```

`init` also installs the record merge driver in the clone's `.git/config` and routes the tracker's files to it in `.gitattributes`. Every other clone runs `taskroll config merge-driver` once: git never takes a merge driver from a committed file, because drivers run commands. A binary inside the repository is named by its path relative to the repository root, so every worktree runs its own build; if that binary is missing, the merge falls back to git's conflict markers rather than silently keeping one side.

Then:

```sh
taskroll epic new --slug payments --title 'Payments'
taskroll add --epic epic-0-payments --id PAY-001 --title 'Card checkout' --size M
taskroll add --epic epic-0-payments --series PAY --title 'Refunds' --depends PAY-001
taskroll status PAY-001 in_progress
taskroll done PAY-001 --on 2026-10-02        # the date it shipped
taskroll comment PAY-002 --body 'Waiting on the PSP contract.'
taskroll list --ready --json                 # every read command takes --json
taskroll show PAY-002
taskroll velocity --weeks 8
taskroll index --check                       # the CI gate
```

## What is in the tracker directory

| Path | What it is |
| --- | --- |
| `taskroll.json` | The tracker's definition: sizes and the points velocity counts them at, the task-line fields and markers, the legend a new epic starts with, and the collections of entries kept beside the epics. Edit it by hand; unknown keys are refused. |
| `data/<epic>.jsonl` | One epic per file, its record first, its tasks after it in the order they were written. The source of truth. |
| `data/<collection>.jsonl` | One file per collection (debt, say), no epic record. |
| `epics/`, `<collection>/` | Generated markdown. Never edit it; the gate fails if it differs from what the records produce. |
| `archive/YYYY-QN.jsonl` | Closed entries and completed epics, swept at a release by `taskroll archive`. Append-only. |

A record is one JSON object per line, keys in a fixed order, so a change to one item is a one-line diff. The schema is [`item.schema.json`](item.schema.json); `taskroll version` prints the schema version the binary reads, and a file written by a newer schema is refused rather than half-read.

## Settings

A tracker with no `taskroll.json` gets the defaults, which `init` writes out in full. A project that sizes work by story points, tracks a quarter per task and keeps a list of ideas beside its epics would write:

```json
{
  "sizes": [{ "name": "1", "points": 1 }, { "name": "3", "points": 3 }, { "name": "8", "points": 8 }],
  "epic": {
    "legend": "> Sizes are story points.",
    "fields": [{ "key": "quarter", "label": "Quarter", "pattern": "^Q[1-4]$" }],
    "markers": [{ "label": "urgent", "marker": "Urgent" }],
    "close_titles": false
  },
  "collections": [
    { "dir": "ideas", "type": "opportunity", "keys": ["title", "type", "status", "source", "opened"],
      "required": ["source"], "values": { "source": ["customer", "team"] } }
  ]
}
```

Each epic field becomes a flag (`--quarter Q3`) and each marker a switch (`--urgent`) on `add` and `edit`; each collection is filed with `taskroll new <type>` and its fields become flags too. A collection with a `series` numbers its entries (`TD-001`); one without names them by a slug from the title. The workflow is fixed (`todo`, `in_progress`, `in_review`, `done`, `dropped`): merging, velocity and every view depend on what the statuses mean.

## The web UI

```sh
taskroll serve             # prints http://127.0.0.1:PORT/?token=...; open that URL
```

A board (drag a card to change its status) and a filterable list, with each item's details, comments and status in a side panel, and a form for new tasks. Every item has its own address, `/items/PAY-002`, so a link opens it directly; the copy-link button in the panel gives you one. It writes through the same locked store as the commands, so the generated files stay current, and it follows your system's light or dark setting.

It is for one person on their own machine: it listens on 127.0.0.1 only, needs the per-session token in the printed URL, refuses other hosts and origins, and runs under a strict Content-Security-Policy with no inline script (the one inline style its components need is admitted by a per-page nonce).

## Working in parallel

- **Ids never collide.** Allocation counts past every id in the records, the archive, every local and remote-tracking branch, and the uncommitted files of other worktrees.
- **Writes never lose each other.** Every write holds an exclusive lock on the tracker for the few milliseconds it takes, so two agents cannot overwrite each other's change. Every worktree of a clone shares the lock, because allocation reads the other worktrees' uncommitted records.
- **Merges are by record.** Two branches appending to one epic merge cleanly; a record changed on both sides merges field by field. A conflict keeps our side, stops the merge for a person, and leaves the other side's record as a comment on the item, so nothing is lost if the file is added without reading. After a merge that touched `data/`, run `taskroll index`.

## Embedding

A host program mounts the commands and plugs in what is its own (an index page, a status report, a ledger of retired ids):

```go
root.AddCommand(cli.Commands(cli.Hooks{
	Invocation: "mytool tasks",
	Open:       func(dir string) (*taskroll.Project, error) { /* OpenProject, then set Views and Reserved */ },
	Views:      []string{"INDEX.md"},
	CheckViews: func(dir string) error { /* fail on a stale view */ },
})...)
```

## Contributing

The Go code needs only Go. The web UI is React, Tailwind CSS and shadcn/ui under `web/`; its build is committed in `internal/web/dist/` so that `go install` works without Node, and CI fails if the two disagree. After changing anything under `web/`:

```sh
cd web && pnpm install && pnpm build    # or pnpm watch while you work
cd ../examples/demo && go run ../../cmd/taskroll serve
```

`go test ./...` includes an end-to-end suite (`e2e/`) that builds the binary and drives it against a copy of the demo: the commands, a merge of two branches through the merge driver, and the web server with its security checks. A change that alters the generated files must regenerate the demo (`taskroll index` in `examples/demo`), or the suite fails.

## Releases

Tags are `vX.Y.Z`; binaries are built with `go build -ldflags "-X main.version=vX.Y.Z" ./cmd/taskroll`. [CHANGELOG.md](CHANGELOG.md) records each release. A change to the record format bumps `SchemaVersion` and the schema's `$id`; older binaries then refuse the files rather than drop fields they do not know.

## License

[MIT](LICENSE)
