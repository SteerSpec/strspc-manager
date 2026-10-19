# SteerSpec Rule Manager — Specification

**Version:** 0.2.0-draft
**Date:** 2026-03-23
**Status:** Draft

---

## 1. Overview

The SteerSpec Rule Manager is the system that governs how rules are defined, validated, versioned, and enforced across software projects. It operates at two levels: **rule authoring** (creating and maintaining rules) and **rule enforcement** (validating code against rules).

This document specifies the data model, lifecycle, tooling architecture, and CI/CD workflows that together form the Rule Manager.

### 1.1 Design Principles

- **JSON is the source of truth.** Rules are structured data. Markdown is a rendered view, never the authoritative format.
- **The system is self-recursive.** The schema that defines valid rules is itself expressed as rules within the Core Rule Set. The Rule Identifier Format, the Rule lifecycle states, the Note format — all are entities governed by their own rules.
- **Rules are immutable after Draft.** Once a rule leaves Draft state, its body cannot be changed. Modifications require supersession: a new rule that explicitly replaces the old one.
- **Enforcement is AI by default.** Rules describe behavioral contracts in natural language. Compliance is evaluated by an LLM that reads the rule and the code diff. Static checks are an optimization, not the baseline.
- **Severity belongs to the consumer, not the rule.** A rule defines what must be true. How critical that is depends on the project consuming it.

---

## 2. Data Model

### 2.1 Core Concepts

The system has four first-class concepts, all expressed through a single object type — the **Entity**.

| Concept | Description |
|---------|-------------|
| **Entity** | An object with a unique identity and lifecycle. The unit of organization. |
| **Rule** | A single-phrase behavioral constraint or property definition attached to an entity. |
| **Rule Set** | The collection of all rules for a given entity, versioned as a unit. |
| **Note** | A supplemental annotation attached to a specific rule. |

An **Entity file** is the atomic unit of storage. It contains one top-level entity, its rules, its sub-entities (nested), and its notes. One file per top-level entity.

### 2.2 Entity

An Entity is an object with a unique identity that persists over time. It may transition through multiple states during its lifecycle and may possess one or more properties. The behavior of an entity is defined entirely by the rules applicable to its identity, properties, and states.

Each entity has an **Entity Unique Identifier** (EUID):

- 3 to 18 characters in length.
- Alphanumeric characters only (letters and numbers).
- Unique within the Realm.

### 2.3 Rule

A Rule defines a single property of an entity or describes the behavior of that property. Rules are expressed as single phrases and are uniquely identified using the Rule Identifier Format.

Each rule carries:

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | The rule identifier, prefixed with the entity ID (e.g., `ENT-001`). |
| `revision` | integer | Monotonically incrementing edit counter. Starts at `0`. |
| `state` | enum | One of `D`, `A`, `P`, `I`, `R`, `T` (see §3 Lifecycle). |
| `body` | string | The rule statement — a single phrase. |
| `added_by` | string | Email address or `@github_handle` of the author. |
| `added_at` | string | ISO date when the rule was created. |
| `supersedes` | string or null | ID of the rule this one replaces, if any. |

**Immutability constraint:** Once a rule is in any state other than Draft (`D`), it is immutable. Modifications must be introduced by superseding it with a new rule.

**No ID reuse:** Rule identifiers must not be reused, even if the rule is deprecated, terminated, or abandoned.

### 2.4 Rule Set

A Rule Set groups all rules applicable to a specific entity. Every entity has exactly one Rule Set. A rule belongs to exactly one Rule Set (the one for its entity).

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Semantic versioning format. |
| `timestamp` | string | UTC ISO timestamp of the last change. |
| `hash` | string or null | `blake3:<hex>` hash of the canonical JSON content. |

**Version bumping:** The Rule Set version must bump (to a strictly higher semver value) whenever any rule in the set changes — whether a revision increment, a state transition, or a new rule added. The author chooses which semver component to increment; the system only enforces that `new_version > old_version`.

**Hash computation:** The Blake3 hash is computed over the entire entity file's canonical JSON (sorted keys, minified). Any change to any part of the file — rules, sub-entities, notes — changes the hash.

### 2.5 Note

A Note is a supplemental explanation attached to a specific rule. Notes provide clarification, context, or elaboration and must not modify the rule they supplement.

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Format: `<rule_id>/<incremental>` (e.g., `RUL-001/01`). |
| `rule_ref` | string | The rule identifier this note is attached to. |
| `type` | enum | One of the closed note types (see §2.6). |
| `content` | string | The note body. |
| `added_by` | string | Email address or `@github_handle`. |
| `added_at` | string | ISO date. |
| `revision` | integer | The rule revision at which this note was added. |

Notes are stored in the same entity file as the rules they reference. They are append-friendly: relation and changelog notes are append-only; rationale, example, and scope notes use latest-wins semantics for rendering (all versions preserved in JSON, renderer shows the most recent).

### 2.6 Note Types (Closed Enum)

| Type | Purpose | Applies to |
|------|---------|------------|
| `rationale` | Why this rule exists. | Rules, Rule Sets |
| `example` | Code or scenario demonstrating compliance. | Rules |
| `counter_example` | Code or scenario demonstrating violation. | Rules |
| `reference` | Link to external specification, RFC, or documentation. | Rules, Rule Sets |
| `applies_to` | Scope hint describing what this rule targets (language, module, API surface). Used by the AI evaluator as context, not as a hard filter. | Rules |
| `changelog` | What changed at this revision. | Rules, Rule Sets |
| `clarification` | Interpretive guidance added after initial authoring. | Rules |
| `deprecation_notice` | Explanation of why a rule is being retired. | Rules, Rule Sets |
| `supersedes` | Declares that this rule replaces another rule (content is the superseded rule ID). | Rules |
| `extends` | Declares that this rule builds on another rule. | Rules |
| `related` | Declares a relationship to another rule or entity. | Rules, Rule Sets |

### 2.7 Sub-Entities

Entities may contain sub-entities. A sub-entity has its own EUID, its own rules, its own Rule Set, and its own notes — but it is nested within its parent entity's file to maintain coherency.

Each sub-entity declares a `parent` field referencing the containing entity's ID.

Sub-entities follow the same schema as top-level entities. The nesting can be arbitrarily deep (e.g., Entity → Sub-Entity → Sub-Sub-Entity), though in practice one or two levels suffice.

### 2.8 Realm

A **Realm** is a namespace in which Entity Unique Identifiers are unique (RLM-001). It is the organizational boundary for rule authoring — every entity file resides in exactly one Realm's directory tree (RLM-007).

**Realm Identifier:** Each Realm has an identifier that follows reverse domain notation (e.g., `dev.steerspec.core`, `com.acme.security`) to ensure global uniqueness across organizations (RLM-008). The identifier pattern is `^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)*$`.

**Structure:** A Realm directory MUST contain:

- A `_schema/` directory with schema files used to validate its entities (RLM-003).
- A `realm.json` manifest at its root (RLM-004).
- One or more entity JSON files.

**Rule Identifier Format:** Each Realm HAS a Rule Identifier Format configuration (RLM-002) that determines how rule identifiers are rendered. If not overridden, the Realm inherits the RLIFRMT defaults (see §4).

**Dependencies:** A Realm MAY declare dependencies on other Realms, importing their rules as read-only references (RLM-005). When dependencies exist, the Realm MUST NOT define Entity Unique Identifiers that collide with any Realm it depends on (RLM-006).

**Cross-Realm References:** Entity references across Realm boundaries MUST use the qualified form `<realm-id>:<EUID>` (e.g., `dev.steerspec.core:ENT`). Within the same Realm, the short form (EUID only) is sufficient (RLM-009).

#### Realm Manifest

The Realm Manifest is a JSON file named `realm.json` at the root of a Realm directory (RLMMNFST-001). It declares the Realm's identity and configuration:

| Field | Type | Description |
|-------|------|-------------|
| `realm.id` | string | Reverse domain notation identifier. |
| `realm.title` | string | Human-readable name. |
| `realm.version` | string | Semantic version of the Realm. |
| `dependencies` | array | Array of `{ realm_id, version, source }` objects referencing other Realms (RLMMNFST-003). `source` is a path relative to the manifest's directory (`../core/`) or a remote reference (`github://SteerSpec/strspc-rules@v1.6.0/rules/core`, see §6.3). |
| `sub_realms` | array of strings or null | Subdirectory names of child Realms (RLMMNFST-005). Null or absent if none. |
| `rule_identifier_format` | object or null | Override RLIFRMT defaults, or `null` to inherit (RLMMNFST-004). |

The manifest MUST declare the Realm's identifier, title, and version (RLMMNFST-002). Dependencies use pinned version references (RLMMNFST-003): `realm-resolve` loads the dependency's own `realm.json` from its `source` and requires `realm.id` and `realm.version` to match the declared `realm_id` and `version` exactly (RR003, RR004). A dependency's `source` MUST be relative or remote; absolute paths are rejected (RR001).

The `realm.v1.schema.json` schema validates this manifest format. It is a hand-maintained axiom — the same bootstrap pattern as `bootstrap.schema.json`.

> See #19 for the Realm architecture discussion and phased implementation plan.

#### Sub-Realms

A Realm MAY contain child Realms (sub-realms) declared via the `sub_realms` field in its manifest (RLM-010, RLMMNFST-005). Each entry names a subdirectory that contains its own `realm.json`.

**Directory structure:**

```
platform/
  realm.json          # id: dev.steerspec.platform, sub_realms: ["sync"]
  ENT.json
  sync/
    realm.json        # id: dev.steerspec.platform.sync
    SYNC.json
```

**ID convention:** A sub-realm's identifier MUST equal `<parent-realm-id>.<subdirectory-name>` (RLM-011). For example, directory `sync/` under realm `dev.steerspec.platform` must have ID `dev.steerspec.platform.sync`.

**Nesting depth:** Sub-realms are limited to one level — a sub-realm MUST NOT declare its own `sub_realms` (RLM-012).

**EUID uniqueness:** EUID uniqueness (RLM-001) spans the parent Realm and all its declared sub-realms (RLM-013). No EUID may appear in both the parent and a sub-realm, or in two sibling sub-realms.

**Dependency inheritance:** A sub-realm inherits its parent's dependencies (RLM-014). It MAY declare additional dependencies of its own; a sub-realm dependency with the same `realm_id` as a parent dependency overrides it. `realm-resolve` resolves this effective list for each sub-realm — inherited entries reuse the parent's resolution, own entries are resolved relative to the sub-realm directory — checks sub-realm EUIDs against the effective dependency set (RR011), and checks each own dependency's EUIDs against the dependencies already in that set (RR005).

### 2.9 Configuration

The **Configuration** is the consumer-side complement to a Realm. Where a Realm defines rules, the Configuration declares which rules a project subscribes to and how they are enforced.

A Configuration IS a directory named `.strspc/` at the root of a project that consumes SteerSpec rules (SPCFG-001). It MUST contain a `config.yaml` manifest file (SPCFG-002) and MAY contain a local cache for resolved rules and evaluation results (SPCFG-003).

A project may have both a Configuration (consuming rules) and a local Realm (authoring rules). They are complementary — the Configuration MAY reference a local Realm as a rule source (SPCFG-005).

**Severity principle:** The severity of a rule violation IS determined by the consumer configuration, not by the rule itself (SPCFG-008). A rule defines what must be true; how critical a violation is depends on the consuming project's context.

#### Rule Sources

The Configuration MUST declare at least one rule source (SPCFG-004). Each source is an entry in the `rules` array of `config.yaml` with:

| Field | Type | Description |
|-------|------|-------------|
| `source` | string | Where rules are fetched from (remote URL or local path). |
| `scope` | string | Applicability: `global` (all files) or `local` (source's own directory tree). |

Source formats include remote references (`github://SteerSpec/strspc-rules@v1/rules/core`) and local Realm directories (`./rules/`).

#### Evaluator

The Configuration MAY configure an evaluator for rule enforcement (SPCFG-006). The `evaluator` object declares:

| Field | Type | Description |
|-------|------|-------------|
| `provider` | string | The AI provider (e.g., `claude`, `openai`, `ollama`, `static-only`). Required. Only `claude` is implemented today; `openai` and `ollama` are reserved and currently fail with an explicit not-implemented error. |
| `endpoint` | string or null | Custom endpoint URL, or `null` for provider default. Optional. |
| `model` | string | Model identifier passed to the provider (default `claude-sonnet-5-5` for `claude`). Optional. |
| `effort` | string | Reasoning effort hint: `low`, `medium` (default), `high`, `xhigh` or `max`. Optional. |

#### Cache

The `cache` object MAY declare a `ttl` property (time-to-live string) controlling how long resolved rules and evaluation results are kept locally.

#### Evaluation States

The Configuration MAY declare which rule lifecycle states are sent to the evaluator. The `evaluate_on` array contains state codes; it defaults to `[D, P, I]` (Draft, Published, Implemented). Rules in other states (`A`, `R`, `T`) are never evaluated.

#### Fail Conditions

The Configuration MAY declare fail conditions referencing rule lifecycle states (SPCFG-007). The `fail_on` array contains state codes (`D`, `A`, `P`, `I`, `R`, `T`) — when a rule in one of these states is violated, enforcement fails. Violations of evaluated rules whose state is not listed in `fail_on` are reported as warnings.

**Schema:** The `config.v1.schema.json` schema validates this configuration format. Unlike hand-maintained axioms (`bootstrap.schema.json`, `realm.v1.schema.json`), the config schema is **generated** from SPCFG entity rules by `tools/build-schema.py`. This follows the same self-referential pattern as `entity.v1.schema.json` — the rules define the schema that validates the artifact.

---

## 3. Rule Lifecycle

### 3.1 States

| Code | Name | Description |
|------|------|-------------|
| `D` | Draft | Initial state. The only state in which a rule's body can be edited. |
| `A` | Abandoned | Terminal state for rules that never left Draft. |
| `P` | Published | Validated and approved, but not yet enforced. |
| `I` | Implemented | In force. The rule is actively enforced. |
| `R` | Retired | Still present but scheduled for removal. Superseded by a newer rule. |
| `T` | Terminated | No longer in force. Terminal state. |

### 3.2 State Machine

```
D (Draft) ──────→ P (Published) ──→ I (Implemented) ──→ R (Retired) ──→ T (Terminated)
   │
   └─────────────→ A (Abandoned)
```

Transitions are forward-only. No state may be skipped. No backward transitions. Each state is applied at most once to a given rule.

### 3.3 Supersession

When an Implemented rule needs to change, the author creates a new rule with a `supersedes` field pointing to the original. The new rule enters Draft and follows the normal lifecycle. When the new rule reaches Implemented, the superseded rule transitions to Retired.

The PR validator enforces consistency: if a rule declares `supersedes` and reaches `I`, the superseded rule must move to `R` in the same PR or a subsequent linked PR.

### 3.4 Revision Semantics

- **Revision** is a simple integer starting at `0`.
- It increments by exactly `+1` on every edit to the rule.
- Revisions only change while the rule is in Draft state (since non-Draft rules are immutable).
- State transitions do not increment the revision — they change the `state` field only.

---

## 4. Rule Identifier Format

The displayed form of a rule identifier (e.g., `[ENT-001.0/D]`) is governed by the `RLIFRMT` entity in the Core Rule Set. This entity defines:

- The bracket pair: `()`, `{}`, or `[]` (default: `[]`).
- The position of the Entity Unique Identifier (opening or closing, default: opening).
- The position of the Rule Set Unique Number (opposing the EUID).
- The split character between EUID and number: `-` or `_` (default: `-`).
- The revision splitter: `.` or `#` (default: `.`).
- The state splitter: `/`, `\`, or `|` (default: `/`).

The JSON stores `id`, `revision`, and `state` as separate fields. The rendered identifier is assembled by the rendering layer according to `RLIFRMT` rules. Different Realms may define different formatting without changing the underlying data.

---

## 5. File Structure

### 5.1 Repository Layout

```
rules/core/
├── ENT.json              ← Entity + Entity Unique Identifier (nested)
├── KWRD.json             ← Key Word definitions (RFC-2119 derived)
├── NTE.json              ← Note entity
├── RLIFRMT.json          ← Rule Identifier Format
├── RLM.json              ← Realm entity + Realm Manifest (nested)
├── RST.json              ← Rule Set + Rule Set Version + Rule Set Hash (nested)
├── RUL.json              ← Rule + Rule State (nested)
├── SPCFG.json            ← Configuration entity + sub-entities (nested)
├── realm.json            ← Realm manifest (dev.steerspec.core)
└── _schema/
    ├── bootstrap.schema.json     ← Minimal schema for CI validation (hand-maintained axiom)
    ├── config.v1.schema.json     ← Config schema (generated from SPCFG rules)
    ├── entity.v1.schema.json     ← Full entity schema (generated from entity rules)
    └── realm.v1.schema.json      ← Realm manifest schema (hand-maintained axiom)
```

Each `.json` file is one top-level entity with its sub-entities nested. The `_schema/` directory holds the JSON Schemas — which are themselves governed by the Core rules (self-recursive validation). The `realm.json` file is the Realm manifest for the core namespace (`dev.steerspec.core`), not an entity file.

### 5.2 Entity File Structure

```json
{
  "$schema": "https://steerspec.dev/schemas/entity/v1.json",
  "entity": {
    "id": "<EUID>",
    "title": "<human-readable name>",
    "description": "<short description>"
  },
  "rule_set": {
    "version": "<semver>",
    "timestamp": "<UTC ISO timestamp>",
    "hash": "<blake3:hex or null>"
  },
  "rules": [
    {
      "id": "<EUID>-<NNN>",
      "revision": 0,
      "state": "D",
      "body": "<single-phrase rule statement>",
      "added_by": "<email or @handle>",
      "added_at": "<ISO date>",
      "supersedes": null
    }
  ],
  "sub_entities": [
    {
      "entity": {
        "id": "<SUB_EUID>",
        "title": "<name>",
        "parent": "<EUID>"
      },
      "rule_set": { "..." : "..." },
      "rules": [ "..." ],
      "sub_entities": [],
      "notes": []
    }
  ],
  "notes": [
    {
      "id": "<rule_id>/<NN>",
      "rule_ref": "<rule_id>",
      "type": "<note_type>",
      "content": "<note body>",
      "added_by": "<email or @handle>",
      "added_at": "<ISO date>",
      "revision": 0
    }
  ]
}
```

---

## 6. Tooling Architecture

### 6.1 Conceptual Layers

| Layer | Concern | Consumers |
|-------|---------|-----------|
| **Rule Schema** | What a valid entity file looks like. JSON Schema. | Linter, PR validator, CLI, AI evaluator. |
| **Rule Lifecycle** | Which state transitions are legal, revision semantics, supersession logic. | PR validator, CLI. |
| **Rule Evaluation** | Does a code change comply with applicable rules? | Pre-commit hook, CI action, IDE plugin. |

### 6.2 Module Breakdown

| Module | Stateful? | Description |
|--------|-----------|-------------|
| `rule-schema` | No | The JSON Schema definition. Consumed by all other modules. |
| `rule-lint` | No | Stateless validator. Takes a single entity file, validates against schema + business rules. Accepts Realm context for cross-entity EUID checks. |
| `rule-diff` | Yes | Stateful validator. Compares before/after of an entity file. Enforces lifecycle, versioning, immutability. |
| `rule-render` | No | Generates markdown (or other formats) from entity JSON. The display layer. |
| `realm-resolve` | Yes (cache) | Resolves Realm dependencies (local or `github://`) and cross-realm references. Validates EUID uniqueness across dependent Realms. |
| `realm-lint` | No | Validates `realm.json` manifests against `realm.v1.schema.json`. Checks structural compliance with RLM rules. |
| `rule-resolve` | Yes (cache) | Fetches the rule sources declared in a Configuration (local directories or `github://` references), verifies entity hashes, and detects EUID collisions across sources. |
| `remote` | Yes (cache) | Downloads `github://` references into the local cache. Shared by `rule-resolve` and `realm-resolve`. |

### 6.3 Remote Sources (`github://`)

Rule sources in a Configuration and dependency `source` fields in a Realm Manifest MAY point at a directory inside a GitHub repository:

```
github://<owner>/<repo>@<ref>[/<path>]
```

| Part | Meaning |
|------|---------|
| `<owner>/<repo>` | GitHub repository. |
| `@<ref>` | Mandatory. An exact tag (`v1.6.0`), a full commit SHA, or the literal `latest`. Every ref other than `latest` is treated as immutable and cached indefinitely, so branch names (`@main`) are discouraged: they resolve, but a forced sync is needed to pick up new commits. |
| `/<path>` | Optional directory inside the repository (`rules/core`). Omitted means the repository root. `..` segments are rejected. |

**Resolution.** Exact tags and SHAs are immutable: once downloaded they are served from the cache indefinitely (a forced sync re-downloads). `@latest` is resolved through the GitHub releases API (`GET /repos/{owner}/{repo}/releases/latest` → `tag_name`), the resolved tag is cached, and it is re-resolved once the configured cache TTL (default 24h) has elapsed. If the releases API is unreachable and a previously resolved tag exists, that tag is used so offline runs keep working.

**Download.** Content is fetched with `GET /repos/{owner}/{repo}/tarball/{ref}`. Only entries under `<path>/` are extracted; symlinks and other special entries are skipped, entries that would escape the target directory are rejected, and extraction stops once 50 MB of file data has been written. The directory is assembled in a temporary location and installed with an atomic rename, so a cache directory either exists completely or not at all.

**Cache layout.**

```
<cache-dir>/github/<owner>/<repo>/<ref>/<sha256(path)[:12]>/   # extracted content
<cache-dir>/github/<owner>/<repo>/.meta/latest.json             # resolved @latest tag + timestamp
```

`<cache-dir>` is the cache directory configured on the resolver (`WithCacheDir`). Consumers SHOULD point it at a git-ignored location such as `.strspc/cache/` so downloaded rules are never committed alongside the Configuration.

**Authentication.** Private repositories and higher rate limits need a token. The resolvers read `GITHUB_TOKEN`, then `GH_TOKEN`, from the environment and send it as `Authorization: Bearer <token>`. A 404 without a token is reported as "private repo or missing token".

**Validation after download.** A fetched directory is treated exactly like a local one: `rule-resolve` parses every entity file and verifies `rule_set.hash` (RSV003, RSV004); `realm-resolve` loads its `realm.json` and enforces the declared `realm_id` and `version` (RR003, RR004). Download failures are reported as RSV007 (rule sources) or RR012 (realm dependencies). A fetched directory holds only the requested subtree, so a relative dependency of a remote Realm that leaves it (`../core/` next to `rules/cli`, declared by the Realm or by one of its sub-realms) is rebased by `realm-resolve` onto the same repository and ref (`github://owner/repo@ref/rules/core`) when the consumer declares the Realm's origin (`WithOrigin`); a relative source that leaves the repository root is rejected (RR001).

---

## 7. CI/CD Workflows

### 7.1 Workflow I — Format Linter (`strspc-lint.yml`)

**Purpose:** Validate that entity JSON files conform to the schema and structural business rules.

**Trigger:** Push and pull request events touching `rules/**/*.json`.

**Checks performed:**

1. Valid JSON (parseable).
2. Passes JSON Schema validation against `entity.v1.schema.json`.
3. Entity ID conforms to EUID rules (3–18 alphanumeric characters).
4. Each rule's ID is prefixed with its entity ID.
5. Rule numbers are sequential within each entity — no gaps, no duplicates.
6. State values are in the allowed enum (`D`, `A`, `P`, `I`, `R`, `T`).
7. Sub-entity `parent` field matches containing entity ID.
8. Each note's `rule_ref` points to an existing rule in the same entity.
9. Note IDs follow the `<rule_id>/<incremental>` format.
10. Note types are in the closed enum.
11. If `rule_set.hash` is present, recompute Blake3 over canonical JSON and verify match.
12. Cross-file references: each rule's `supersedes` points to an existing rule ID within the scanned realm.
13. `rule_set.version` is valid semver.
14. Relation notes (`supersedes`, `extends`, `related`): every unqualified `<EUID>-<NNN>` reference in the note content points to an existing rule ID within the scanned realm. Qualified `<realm-id>:<EUID>-<NNN>` references target another realm and are not checked here.

**Output:** GitHub Actions annotations inline on PR files, plus a summary check.

### 7.2 Workflow II — PR Update Validator (`strspc-validate-pr.yml`)

**Purpose:** Verify that changes to entity files follow lifecycle rules, versioning requirements, and immutability constraints.

**Trigger:** Pull request events targeting `develop`, touching `rules/**/*.json`.

**Base comparison:** The PR's target branch (`develop` HEAD).

For each changed entity file, the validator compares the before and after states:

1. **Draft rule edited:** Revision must equal `previous_revision + 1`. State must remain `D`.
2. **Non-draft rule body edited:** Reject. The rule is immutable. The author must supersede it with a new rule.
3. **State transition:** Must follow the state machine (§3.2). No skipping states, no backward transitions.
4. **New rule added:** Must have `revision: 0`, `state: "D"`, and the next sequential number for that entity.
5. **Rule removed:** Reject. Rules are never removed from the file. They transition to `A` or `T`.
6. **Supersession consistency:** If a rule with `supersedes` reaches state `I`, the superseded rule must move to `R` in the same PR or a linked subsequent PR.
7. **Rule Set version:** Must be strictly higher than the previous version (semver comparison).
8. **Rule Set hash:** Must be recomputed and updated to match the new file content.
9. **Rule Set timestamp:** Must be updated.
10. **No ID reuse:** New rule numbers must not reuse any previously existing rule number in the entity's history.
11. **`added_by` present:** Required on new rules and new notes. Must be a valid email or `@handle`.
12. **Notes only added, not removed:** For append-only note types (`changelog`, `supersedes`, `extends`, `related`), notes present in the base version must still be present.

**Output:** GitHub Actions check run with pass/fail per entity file, detailed error messages for each violation.

---

## 8. Enforcement Architecture

### 8.1 Developer Experience

A developer's repository contains a `.strspc/` directory (see §2.9) that configures which rule sets apply and how they are evaluated.

```
project-repo/
├── .strspc/
│   ├── config.yaml       ← rule sources, evaluation settings
│   └── cache.db          ← local evaluation cache (embedded key-value store)
├── src/
│   └── ...
└── .github/workflows/
    └── strspc.yml        ← CI workflow (paid tier)
```

### 8.2 Configuration

```yaml
rules:
  - source: github://SteerSpec/strspc-rules@v1/rules/core
    scope: global
  - source: ./rules/
    scope: local

evaluator:
  provider: claude          # or static-only (openai, ollama reserved)
  endpoint: null            # defaults per provider
  model: claude-sonnet-5-5  # provider default when omitted
  effort: medium            # low | medium | high | xhigh | max

cache:
  ttl: 24h

evaluate_on:
  - D                       # states sent to the evaluator (default D, P, I)
  - P
  - I

fail_on:
  - I                       # fail if an Implemented rule is violated
```

The configuration structure is formally defined by the `SPCFG` entity and its sub-entities (`SPCFGSRC`, `SPCFGEVAL`, `SPCFGCCH`, `SPCFGFAIL`). The schema at `config.v1.schema.json` is generated from these rules and validates this format.

The `fail_on` list references rule lifecycle state codes, not severity levels (since severity is a consumer concern, not a rule property — see SPCFG-008). A project might fail on Implemented (`I`) rules only, while treating Published (`P`) rules as warnings.

### 8.3 Evaluation Flow

1. Resolve applicable rule sets (from config sources).
2. Collect all rules whose state is listed in `evaluate_on` (default `D`, `P`, `I`).
3. Gather the code diff (pre-commit) or PR diff (CI). An empty diff ends evaluation with an informational notice (`RE004`).
4. Look up each rule in the evaluation cache (§8.4); only cache misses are sent to the provider.
5. Group the remaining rules by owning entity and send one request per entity: a shared system prompt, the diff, and the rules with their notes (`applies_to`, `rationale`, `example`, `counter_example`, `clarification`). Providers split large diffs on file and hunk boundaries and merge verdicts, a violation in any chunk winning; a hunk too large to send is reported, never silently skipped.
6. The AI returns exactly one verdict per rule — compliant, violated, or not applicable — with an explanation and evidence locations (file, line, excerpt). `not_applicable` is the default when the rule's subject is absent from the diff.
7. Report results. Violations carry the first evidence location; a provider failure is a warning (`RE002`) for each affected rule and never blocks on its own. Block merge if `fail_on` conditions are met.

### 8.4 Local Mode (Pre-commit Hook, Free Tier)

- CLI tool: `strspc init`, `strspc check`, `strspc sync`.
- Resolved rules cached locally under `.strspc/cache/`; evaluation results in `.strspc/cache.db`, an embedded key-value store (no external database).
- Evaluation results are keyed on `(rule_id, rule_revision, rule_body, notes, source, scope, diff_hash, model, prompt_version)` — the same diff against the same rule skips re-evaluation, while a rule edit, model change or prompt revision invalidates the entry. A provider that cannot report its model and prompt version cannot be used with the cache. Entries expire after `cache.ttl`.
- If the cache file is locked by another process the check runs without a cache and says so; a corrupt cache file is recreated.
- The developer provides their own LLM (API key for Claude, local Ollama, etc.) via `.strspc/config.yaml`.
- Graceful degradation: if no LLM is configured, only JSON Schema validation runs (structural checks, no behavioral evaluation).

### 8.5 Cloud Mode (CI Action, Paid Tier)

- GitHub Action calls `api.steerspec.io/evaluate`.
- Sends: the diff, the resolved rule set references.
- SteerSpec cloud resolves rules, runs AI evaluation, returns results.
- Server-side caching across organizations using the same global rules.
- Dashboard: compliance trends, most-violated rules, per-repo status.

---

## 9. Self-Referential Bootstrapping

The Core Rule Set (`rules/core/`) contains entities that define the system itself:

| Entity | Sub-entity | Defines |
|--------|------------|---------|
| `ENT` | — | What an Entity is. |
| `ENT` | `ENTUQID` | Format of Entity Unique Identifiers. |
| `RUL` | — | What a Rule is, including immutability constraints. |
| `RUL` | `RULST` | Rule States and their transitions. |
| `RST` | — | What a Rule Set is. |
| `RST` | `RSTVRS` | Rule Set Version format. |
| `RST` | `RSTHSH` | Rule Set Hash requirements. |
| `RLIFRMT` | — | How Rule Identifiers are rendered. |
| `KWRD` | — | Key Word semantics (RFC-2119 derived: MUST, SHOULD, MAY, …). |
| `NTE` | — | What a Note is and how it relates to rules. |
| `RLM` | — | What a Realm is: namespace boundaries, identifiers, dependencies, cross-realm references. |
| `RLM` | `RLMMNFST` | Realm Manifest: identity, version, dependency declarations. |
| `SPCFG` | — | Consumer-side configuration (`.strspc/` directory and `config.yaml`). |
| `SPCFG` | `SPCFGSRC` | Rule Source entries in config.yaml. |
| `SPCFG` | `SPCFGEVAL` | Evaluator configuration. |
| `SPCFG` | `SPCFGCCH` | Cache configuration. |
| `SPCFG` | `SPCFGFAIL` | Fail condition configuration. |

The JSON Schema (`_schema/entity.v1.schema.json`) is the machine-readable expression of the entity rules. The config schema (`_schema/config.v1.schema.json`) is the machine-readable expression of the SPCFG rules. Changes to these schemas are governed by the same lifecycle: proposed as Draft, validated, published, implemented. The bootstrap schema (`_schema/bootstrap.schema.json`) and the realm schema (`_schema/realm.v1.schema.json`) are hand-maintained axioms — they are valid by convention, and everything else derives from them.

---

## 10. Glossary

| Term | Definition |
|------|------------|
| **Realm** | A namespace in which Entity Unique Identifiers are unique and Rule Identifier Format is consistent (RLM-001, RLM-002). Identified by reverse domain notation (RLM-008). Contains a `realm.json` manifest (RLM-004) and a `_schema/` directory (RLM-003). An Entity resides in exactly one Realm (RLM-007). See also: cross-realm references (RLM-009), Realm dependencies (RLM-005). |
| **Realm Manifest** | The `realm.json` file at a Realm's root declaring its identity, version, and dependencies (RLMMNFST). |
| **Configuration** | The `.strspc/` directory and its `config.yaml` manifest that declare which rules a project subscribes to and how they are enforced (SPCFG). |
| **Rule Source** | An entry in `config.yaml` specifying where rules are fetched from and their applicability scope (SPCFGSRC). |
| **Evaluator** | The AI provider configuration used for rule enforcement (SPCFGEVAL). |
| **Fail Condition** | A rule lifecycle state code that, when violated, causes enforcement failure (SPCFGFAIL). |
| **Cross-Realm Reference** | A qualified reference using `<realm-id>:<EUID>` format to identify an entity in another Realm (RLM-009). |
| **EUID** | Entity Unique Identifier. 3–18 alphanumeric characters. |
| **Supersession** | The process of replacing an immutable rule with a new rule that declares `supersedes`. |
| **Canonical JSON** | JSON serialized with sorted keys and no extraneous whitespace, used as input for Blake3 hashing. |
| **Rule Set Hash** | Blake3 hash of the canonical JSON content of an entity file, used for fast-track change detection. |

