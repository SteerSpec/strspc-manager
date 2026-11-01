# CLAUDE.md

This file provides guidance to Claude Code when working with this repository.

## Rule Manager Specification

The canonical specification for this project lives at [`docs/SPEC.md`](docs/SPEC.md).
All modules implement against this spec. When implementing or modifying any module,
consult the relevant spec sections:

- **§2** Data Model — entities, rules, rule sets, notes, realms, configuration
- **§6** Module Breakdown — what each module does
- **§7** CI/CD Workflows — validation checks rule-lint and rule-diff must perform
- **§8** Enforcement Architecture — how rule-eval and realm-resolve work

## Project

strspc-manager — core enforcement engine (Layer 2) in the SteerSpec 3-tier architecture.
Go library consumed by strspc-CLI (OSS) and strspc-cloud (SaaS).

## Build & Test

```bash
make build        # build binary
make test         # run go test ./...
make cover        # run the tests with -race and the coverage floor, as CI does
make lint         # golangci-lint
```

Every package under `src/` keeps at least 85% statement coverage from its own
tests (GOTEST-001). `make cover` fails any package below that.

## Conventions

- [Conventional Commits](https://www.conventionalcommits.org/): `<type>(<scope>): <description>`
- Semantic versioning. Automated via release-please + goreleaser.
- See `AGENTS.md` for workflow instructions (beads issue tracking, session completion).

<!-- STEERSPEC:BEGIN:org-standards -->
## SteerSpec org standards

Managed by [SteerSpec/.claude](https://github.com/SteerSpec/.claude) through
[strspc-sync](https://github.com/SteerSpec/strspc-sync). Change
`templates/CLAUDE.md.mustache` there; edits made here are overwritten by the
next sync.

- **Tiers.** strspc-rules (rule definitions) → strspc-manager (Go engine) →
  strspc-CLI and the other consumers of strspc-manager. Dependencies point
  downward only. Release and tag upstream before bumping a downstream `go.mod`
  or core pin.
- **Pull requests only.** Org rulesets reject direct pushes to the default
  branch and require verified commit signatures; Copilot review and
  pr-auto-approve gate the merge. Agents open PRs and never merge them.
- **Conventional commits** (`type(scope): subject`). Where a repo uses
  release-please, it derives versions and changelogs from them; other repos
  follow the release process in their own README or AGENTS.md.
- **Rules live next to the code.** Each repo owns a realm under `rules/` that
  depends on `dev.steerspec.core`. Author rules through `strspc rule …` so the
  content hashes stay valid; never hand-edit rule JSON.
- **Rule states.** Draft text may still change. Published text is final while
  the code may still violate it. Implemented is enforced in CI (`fail_on: [I]`);
  promote to Implemented only once the code complies.
- **Work tracking.** Multi-session work is tracked in beads (`bd`), not in
  markdown todo files.
- **Org agents.** Architect, Agile PM, Code Review and Release come from the
  `strspc-agents` plugin: `/plugin marketplace add SteerSpec/.claude`, then
  `/plugin install strspc-agents@steerspec`.
<!-- STEERSPEC:END:org-standards -->