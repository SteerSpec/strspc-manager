# CLAUDE.md

This file provides guidance to Claude Code when working with this repository.

## Rule Manager Specification

The canonical specification for this project lives at [`docs/SPEC.md`](docs/SPEC.md).
All modules implement against this spec. When implementing or modifying any module,
consult the relevant spec sections:

- **§2** Data Model — entities, rules, rule sets, notes, realms, configuration
- **§6** Module Breakdown — what each module does
- **§7** CI/CD Workflows — validation checks rule-lint and rule-diff must perform
- **§8** Enforcement Architecture — how rule-eval and rule-resolve work

## Project

strspc-manager — core enforcement engine (Layer 2) in the SteerSpec 3-tier architecture.
Go library consumed by strspc-CLI (OSS) and strspc-cloud (SaaS).

## Build & Test

```bash
make build        # build binary
make test         # run tests with -race
make lint         # golangci-lint
```

## Conventions

- [Conventional Commits](https://www.conventionalcommits.org/): `<type>(<scope>): <description>`
- Semantic versioning. Automated via release-please + goreleaser.
- See `AGENTS.md` for workflow instructions (beads issue tracking, session completion).
