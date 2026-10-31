# strspc-manager

SteerSpec Rule Manager — core enforcement engine. Go.

## Architecture

The manager is the **core engine** (Layer 2) in the SteerSpec 3-tier architecture:

```mermaid
graph TD
	RULES["strspc-rules<br/><i>Layer 1 — JSON rules + schemas (Python)</i>"]
	MGR["strspc-manager<br/><i>Layer 2 — rule-lint, rule-diff, rule-eval, rule-resolve (Go)</i>"]
	CLI["strspc-CLI<br/><i>Layer 3a — OSS CLI (Go)</i>"]
	CLOUD["strspc-cloud<br/><i>Layer 3b — Paid SaaS: API, server-side eval, dashboard</i>"]

	RULES -->|consumed by| MGR
	MGR -->|wrapped by| CLI
	MGR -->|wrapped by| CLOUD
```

All validation and evaluation logic lives here. The CLI and cloud service
import the manager's public Go packages — they never implement rule logic directly.

See [strspc-rules#17](https://github.com/SteerSpec/strspc-rules/issues/17)
for the full architecture decision.

## Packages

Public packages under `src/` — importable by strspc-CLI and strspc-cloud:

```go
import "github.com/SteerSpec/strspc-manager/src/entity"       // shared domain types
import "github.com/SteerSpec/strspc-manager/src/entityops"    // entity file mutations
import "github.com/SteerSpec/strspc-manager/src/result"       // shared diagnostic types
import "github.com/SteerSpec/strspc-manager/src/schema"       // schema fetch + cache
import "github.com/SteerSpec/strspc-manager/src/remote"       // github:// fetch + cache
import "github.com/SteerSpec/strspc-manager/src/render"       // entity → Markdown
import "github.com/SteerSpec/strspc-manager/src/rulelint"     // stateless entity validator
import "github.com/SteerSpec/strspc-manager/src/rulediff"     // PR lifecycle validator
import "github.com/SteerSpec/strspc-manager/src/ruleeval"     // AI-powered evaluation
import "github.com/SteerSpec/strspc-manager/src/evalcache"    // eval verdict cache
import "github.com/SteerSpec/strspc-manager/src/ruleresolve"  // rule fetch + cache
import "github.com/SteerSpec/strspc-manager/src/realmlint"    // Realm directory validator
import "github.com/SteerSpec/strspc-manager/src/realmresolve" // Realm dependency resolver
```

### Foundation

| Package | Description |
| ------- | ----------- |
| `entity` | Shared domain types: `File`, `Entity`, `RuleSet`, `Rule`, `Note`, `RealmFile`. Includes `Parse([]byte)` and `Load(path)` for both file and HTTP consumers, the rule state machine, and the closed note-type enum (`NoteTypes`, `IsAppendOnlyNoteType`). |
| `entityops` | Mutations of an entity file that keep it valid: create an entity, add, edit, promote, retire, abandon and supersede rules, add and revise notes. `NewFile` starts a file at `rule_set.version` 0.1.0; every other operation changes an existing file and bumps that version (minor on promote/retire, patch otherwise). All of them refresh the timestamp and hash (§2.4). Operations act on the `File` they are given; callers bump the ancestors of a sub-entity. |
| `result` | Shared `Diagnostic`, `Severity`, and `Result` types returned by all modules. |
| `schema` | Fetches and caches JSON schemas from `steerspec.dev` at runtime. No embedded copies — single source of truth via strspc-www. |
| `remote` | Fetches `github://` rule directories through the GitHub REST tarball endpoint and caches them locally (§6.4). Shared by `ruleresolve` and `realmresolve`. |
| `render` | Renders entity files to human-readable formats; Markdown today, with an optional custom template (`WithTemplate`). Rule IDs are displayed in the RLIFRMT default format unless a Realm's `rule_identifier_format` is passed through `NewRuleIDFormatter` and `WithRuleIDFormatter`. |

### Modules

| Package | Type | Description |
| ------- | ---- | ----------- |
| `rulelint` | deterministic, stateless | Validates entity files against schema and business rules (§7.1) |
| `rulediff` | deterministic, stateful | Validates rule lifecycle transitions across PR diffs (§7.2) |
| `ruleeval` | AI, pluggable providers | Evaluates code compliance against rules (§8.3). Batches rules per entity, caches verdicts, and degrades to static-only mode when no provider is configured. See [Evaluation providers](#evaluation-providers). |
| `evalcache` | deterministic | Persists `ruleeval` verdicts in an embedded key-value store at `.strspc/cache.db` with TTL expiry (§8.4) |
| `ruleresolve` | deterministic | Fetches, caches, and resolves rules from configured sources (§8.2) |
| `realmlint` | deterministic, stateless | Validates Realm structure, `realm.json` manifest, and EUID uniqueness |
| `realmresolve` | deterministic | Resolves Realm dependencies declared in `realm.json` and fetches remote Realms |

## Evaluation providers

`ruleeval/providers.New(name, opts)` builds a provider from the `evaluator.provider`
value in `.strspc/config.yaml`:

| Provider | Status | Notes |
| -------- | ------ | ----- |
| `claude` | available | Anthropic Messages API via `ruleeval/claude`. Default model `claude-sonnet-5-5`, effort `medium`; structured outputs (verdicts keyed by rule id, then one detail request per violated rule that confirms or withdraws it); prompt caching on the system prompt and diff; API key from `ANTHROPIC_API_KEY` (workspace-scoped). |
| `static-only` (also `static`, `null` or omitted) | available | No AI calls; `ruleeval` emits `RE003` and only structural checks run. |
| `openai` | planned | Returns `ErrProviderNotImplemented` ([#67](https://github.com/SteerSpec/strspc-manager/issues/67)). |
| `ollama` | planned | Returns `ErrProviderNotImplemented` ([#67](https://github.com/SteerSpec/strspc-manager/issues/67)). |

Provider tests replay recorded HTTP cassettes from `src/ruleeval/claude/testdata/cassettes/`
and never call the network in CI. Set `STRSPC_RECORD=1` with an API key to re-record,
or run the live smoke test with `go test -tags live ./src/ruleeval/claude/`.

## Rule sources

The manager consumes rules and schemas published at
[steerspec.dev](https://steerspec.dev) from
[strspc-rules](https://github.com/SteerSpec/strspc-rules):

| Resource | URL |
| -------- | --- |
| Entity schema | `https://steerspec.dev/schemas/entity/v1.json` |
| Realm schema | `https://steerspec.dev/schemas/realm/v1.json` |
| Bootstrap schema | `https://steerspec.dev/schemas/entity/bootstrap.json` |
| Rules manifest | `https://steerspec.dev/rules/latest/index.json` |
| Versioned rules | `https://steerspec.dev/rules/v<version>/` |

## Realms

Rules are organized into **Realms** — namespaces where Entity Unique Identifiers
(EUIDs) are unique. A Realm is any directory containing entity files and a
`realm.json` manifest:

- `strspc-rules/rules/core/` — the **core** Realm (ships with SteerSpec)
- An org spec repo — an **organizational** Realm
- `./rules/` in a consumer project — a **local** Realm
- `rules/` in this repository — `dev.steerspec.manager`, the engine's own rules,
	validated and evaluated in CI with the released CLI

See [strspc-rules#19](https://github.com/SteerSpec/strspc-rules/issues/19)
for the full Realm architecture.

## Branching and releases

`develop` is the default branch and collects the changes for the next release;
`main` only moves when a release is prepared:

1. Pull requests target `develop` and are **squashed**. The pull request title
	becomes the commit release-please reads, so it decides the release note and
	the version bump. A merge commit would list the change twice.
2. A `chore: promote develop to main` pull request carries the bundle to `main`.
	Merge it with a **merge commit**: release-please reads the individual
	conventional commits, and a squash would hide them behind a single `chore:`.
3. release-please opens its release pull request against `main`; merging it tags
	the version and GoReleaser publishes it.

The `merge-method-develop` and `merge-method-main` rulesets enforce both merge
methods. `develop` is not back-merged, so it stays behind `main` by the release
commits.

## References

- [`docs/SPEC.md`](docs/SPEC.md) — canonical Rule Manager specification; section numbers (§) above refer to it
- [strspc-rules#4](https://github.com/SteerSpec/strspc-rules/issues/4) — Rule Manager specification
- [strspc-rules#17](https://github.com/SteerSpec/strspc-rules/issues/17) — 3-tier platform architecture
- [strspc-rules#19](https://github.com/SteerSpec/strspc-rules/issues/19) — Realm formalization
