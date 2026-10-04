# Tasks

This directory is a work tracker kept in git. The records in `data/*.jsonl` are the source of truth: one JSON record per line, the epic record first in each file. Markdown under `epics/` is generated from them.

**Change it only through the CLI**, never by editing the files: every write validates the record, stamps who did it and when, and regenerates the views.

    taskroll epic new --slug payments --title 'Payments'
    taskroll add --epic epic-0-payments --id PAY-001 --title 'Card checkout' --size M --label checkout
    taskroll status PAY-001 in_progress
    taskroll done PAY-001
    taskroll list --ready --json
    taskroll show PAY-001
    taskroll whoami

What the tracker holds is defined in `taskroll.json` beside this file: the sizes and the points velocity counts them at, the fields and markers a task line shows, the legend a new epic starts with, and any collections of entries kept one file each beside the epics (debt, opportunities, a watch list), with the fields they need and the values those fields take.

Who you are comes from `git config taskroll.user`, then `git config user.name`; set it with `taskroll config user 'Your Name'`.

Run `taskroll config merge-driver` once per clone: git then merges the records by ID instead of by line, so branches that each add items merge cleanly. After a merge, regenerate the views with `taskroll index`.
