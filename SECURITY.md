# Security

`taskroll serve` runs a local web UI. It is meant for one person on their own machine: it listens on 127.0.0.1 only, requires a per-session token, checks Host, Origin and a CSRF header, and serves a strict Content-Security-Policy. A way around any of those is a vulnerability.

Report it privately through GitHub's [private vulnerability reporting](https://github.com/hvish/taskroll/security/advisories/new) rather than in a public issue. You should hear back within a week.

Only the latest release is supported.
