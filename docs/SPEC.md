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
| `dependencies` | array | Array of `{ realm_id, version }` objects referencing other Realms (RLMMNFST-003). |
| `rule_identifier_format` | object or null | Override RLIFRMT defaults, or `null` to inherit (RLMMNFST-004). |

The manifest MUST declare the Realm's identifier, title, and version (RLMMNFST-002). Dependencies use pinned version references (RLMMNFST-003).

The `realm.v1.schema.json` schema validates this manifest format. It is a hand-maintained axiom — the same bootstrap pattern as `bootstrap.schema.json`.

> See #19 for the Realm architecture discussion and phased implementation plan.

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
| `provider` | string | The AI provider (e.g., `claude`, `openai`, `ollama`, `static-only`). Required. |
| `endpoint` | string or null | Custom endpoint URL, or `null` for provider default. Optional. |

#### Cache

The `cache` object MAY declare a `ttl` property (time-to-live string) controlling how long resolved rules and evaluation results are kept locally.

#### Fail Conditions

The Configuration MAY declare fail conditions referencing rule lifecycle states (SPCFG-007). The `fail_on` array contains state codes (`D`, `A`, `P`, `I`, `R`, `T`) — when a rule in one of these states is violated, enforcement fails.

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
| `realm-resolve` | No | Resolves Realm dependencies and cross-realm references. Validates EUID uniqueness across dependent Realms. |
| `realm-lint` | No | Validates `realm.json` manifests against `realm.v1.schema.json`. Checks structural compliance with RLM rules. |

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
12. Cross-file references: `supersedes` and relation notes point to existing rule IDs within `rules/core/`.
13. `rule_set.version` is valid semver.

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
│   └── cache.db          ← local rule cache (SQLite)
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
  provider: claude          # or ollama, openai, static-only
  endpoint: null            # defaults per provider

cache:
  ttl: 24h

fail_on:
  - I                       # fail if an Implemented rule is violated
```

The configuration structure is formally defined by the `SPCFG` entity and its sub-entities (`SPCFGSRC`, `SPCFGEVAL`, `SPCFGCCH`, `SPCFGFAIL`). The schema at `config.v1.schema.json` is generated from these rules and validates this format.

The `fail_on` list references rule lifecycle state codes, not severity levels (since severity is a consumer concern, not a rule property — see SPCFG-008). A project might fail on Implemented (`I`) rules only, while treating Published (`P`) rules as warnings.

### 8.3 Evaluation Flow

1. Resolve applicable rule sets (from config sources).
2. Collect all rules in state `I` (Implemented) — or `P` and `I` depending on config.
3. Gather the code diff (pre-commit) or PR diff (CI).
4. For each rule, send to the AI evaluator: the rule body, its notes (rationale, examples, applies_to), and the diff.
5. The AI determines: compliant, violated, or not applicable.
6. Report results. Block merge if `fail_on` conditions are met.

### 8.4 Local Mode (Pre-commit Hook, Free Tier)

- CLI tool: `strspc init`, `strspc check`, `strspc sync`.
- Rules cached locally in `.strspc/cache.db`, keyed on rule ID + rule set hash.
- Evaluation results cached on `(rule_id, rule_revision, diff_hash)` — same diff against same rule skips re-evaluation.
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

