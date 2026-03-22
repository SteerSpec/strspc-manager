# strspc-manager

SteerSpec Rule Manager — core enforcement engine (rule-lint, rule-diff, rule-eval, rule-resolve). Go.

## Rule sources

The manager consumes rules and schemas published at
[steerspec.dev](https://steerspec.dev) from
[strspc-rules](https://github.com/SteerSpec/strspc-rules):

| Resource | URL |
| -------- | --- |
| Entity schema | `https://steerspec.dev/schemas/entity/v1.json` |
| Bootstrap schema | `https://steerspec.dev/schemas/entity/bootstrap.json` |
| Rules manifest | `https://steerspec.dev/rules/latest/index.json` |
| Versioned rules | `https://steerspec.dev/rules/v<version>/` |

## Architecture

The manager is the **core engine** in the SteerSpec 3-tier architecture:

```
strspc-rules          (data: JSON rules + schemas)
     │
     ▼
strspc-manager        (core engine, Go, OSS)   ◄── this repo
     │
     ├──────────────────┐
     ▼                  ▼
strspc-CLI           strspc-cloud
(developer tool)     (paid SaaS)
```

All validation and evaluation logic lives here. The CLI and cloud service
call into the manager — they never implement rule logic directly.

See [strspc-rules#17](https://github.com/SteerSpec/strspc-rules/issues/17)
for the full architecture decision.

## Modules

| Module | Type | Description |
| ------ | ---- | ----------- |
| `rule-lint` | deterministic, stateless | Validates entity files against schema and lifecycle rules |
| `rule-diff` | deterministic, stateful | Validates rule changes across PR diffs |
| `rule-eval` | AI, pluggable providers | Evaluates code compliance against rules |
| `rule-resolve` | deterministic | Fetches, caches, and resolves rules from configured sources |
| `realm-lint` | deterministic, stateless | Validates Realm structure, manifest, and EUID uniqueness |
| `realm-resolve` | deterministic | Resolves Realm dependencies and fetches remote Realms |

## Realms

Rules are organized into **Realms** — namespaces where Entity Unique Identifiers
(EUIDs) are unique. A Realm is any directory containing entity files and a
`realm.json` manifest:

- `strspc-rules/rules/core/` — the **core** Realm (ships with SteerSpec)
- An org spec repo — an **organizational** Realm
- `./rules/` in a consumer project — a **local** Realm

See [strspc-rules#19](https://github.com/SteerSpec/strspc-rules/issues/19)
for the full Realm architecture.

## References

- [strspc-rules#4](https://github.com/SteerSpec/strspc-rules/issues/4) — Rule Manager specification
- [strspc-rules#17](https://github.com/SteerSpec/strspc-rules/issues/17) — 3-tier platform architecture
- [strspc-rules#19](https://github.com/SteerSpec/strspc-rules/issues/19) — Realm formalization
