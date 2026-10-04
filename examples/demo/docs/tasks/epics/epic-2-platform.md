# EPIC 2: Platform
<!-- Generated from ../data/epic-2-platform.jsonl. Change it with the tracker CLI, never by hand. -->

> Sizes: S is about a day, M two to three days, L a week. A blocker holds up the tasks that depend on it.

Shared services the storefront runs on.

- [x] <a id="api-001"></a>**API-001** - Rate limiting on the public API `M` · Quarter: Q3 · Done: 2026-09-02
- [ ] <a id="api-002"></a>**API-002** - Inventory service · **[BLOCKER]** `L` · Quarter: Q3
- [ ] <a id="api-003"></a>**API-003** - Structured audit log `M` · Quarter: Q4
- [ ] <a id="api-004"></a>**API-004** - Webhook signature verification `M` · Depends: API-001 · Quarter: Q4
- [x] <a id="api-005"></a>**API-005** - Background job retries with backoff `S` · Quarter: Q4 · Done: 2026-09-30
