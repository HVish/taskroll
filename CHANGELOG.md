# Changelog

## Unreleased (v0.1.0)

First public release. Record schema 1.

- JSONL records as the only source; epic and collection markdown generated from them and gated by `index --check`.
- Commands: `init`, `epic new|list`, `add`, `new`, `status`, `done`, `edit`, `comment`, `list`, `show`, `velocity`, `burndown`, `index`, `archive`, `fmt`, `whoami`, `config user|merge-driver`, `version`.
- `taskroll.json` settings: sizes and points, task-line fields with patterns, markers, epic legend, closing full stops, collections with series, keys, required fields and vocabularies.
- An exclusive write lock around every change, shared by every worktree of a clone; allocation inside it.
- Branch-safe ids (other branches and worktrees counted) and a record-level git merge driver that keeps the losing side of a conflict as a comment. The driver names an in-repository binary relatively and falls back to conflict markers when it is missing.
- `serve`: a local web UI built with React, Tailwind CSS and shadcn/ui (board and list, a details panel with status and comments, new tasks, light and dark themes, an address per item at `/items/<ID>`) behind a loopback-only listener, a per-session token, host, origin and CSRF checks and a strict CSP with a per-page style nonce, writing through the locked store.
- Audit stamps (`created_at`/`created_by`, `closed_at`/`closed_by`) and velocity from them.
