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

An Entity is what rules are written about (ENT-001 to ENT-004).

Each entity carries its **Entity Unique Identifier** (EUID, ENT-005):

- 3 to 18 characters in length.
- Alphanumeric characters only (letters and numbers).
- Unique within the Realm.

An entity MAY declare `inherits`, the EUID of one other entity whose effective rule set it inherits (ENT-006): the short EUID in the same Realm, or `<realm-id>:<EUID>` for an entity in a declared dependency (ENT-007). Chains MUST NOT cycle or exceed three hops (ENT-008). The field is omitted when the entity inherits nothing.

**Effective rule set.** An inheriting entity's effective rule set is computed by the `inherit` package from its resolved chain. Starting from the root-most ancestor, each ancestor contributes its Published and Implemented rules after removing the inherited rules its own Published and Implemented rules override; then the entity's own rules that pass the consumer's evaluate states (default `D`, `P`, `I`) are added after removing the rules they override. Filtering before overriding lets an inherited rule reappear once the rule overriding it is Retired or Terminated. Inherited rules stay owned by their entity (ENT-009) and never count towards the inheriting entity's hash (ENT-010). Given an entity's resolved chain (`render.WithInheritance`), the Markdown renderer adds an "Inherited rules" section listing each inherited rule with its owner, Realm and state, and marking those the entity's own rules override; the rules are listed, never copied into the entity.

**Visibility.** An entity MAY declare `visibility`, which limits who may build on it (ENT-011). When it is absent, a sub-entity takes the visibility of the entity containing it and any other entity is `public`.

| Visibility | Same Realm | Same Realm tree | Any other Realm |
|------------|------------|-----------------|-----------------|
| `public` | inherit, override | inherit, override | inherit, override |
| `protected` | inherit, override | inherit, override | inherit |
| `private` | inherit, override | inherit | nothing |

A Realm tree is a top-level Realm and its declared sub-realms, so a parent, a sub-realm and its siblings are in the same tree but are different Realms (RLM-016). `inherits` needs the permission of the inherited entity. `overrides` needs that of every entity from the inherited entity up to the owner of the rule, measured from the declaring Realm, so a `protected` entity shields the rules it inherits from a `public` one (ENT-016). An entity MUST NOT be wider than the entity it inherits or the entity containing it (ENT-015): it would otherwise re-expose what those entities hold back. An `extends` note follows the inherit column, and its target's entity MUST NOT be narrower than the note's, since whoever inherits the note reads the target in evaluation (NTE-012). `related` notes and bodies are citations and aren't limited. Rules that are Abandoned, Retired or Terminated are exempt, because their `overrides` and notes can't change; a superseded rule isn't, because it stays in force until it is Retired.

`realm-resolve` enforces these (RR022 to RR025), so they run in `strspc check` and `strspc render`, not `strspc realm validate`. Each chain is checked hop by hop, a dependency's hops included, so a dependency that breaks these rules fails its consumers too. `rule-lint` checks the value and the limit from the containing entity (RL023, RL024). Tree checks hold from a top-level Realm: a sub-realm's manifest doesn't list its siblings, so a sub-realm used as a source or a dependency on its own sees them as other Realms. Realm identity is the Realm ID at any version, and the ID in a manifest is trusted (RLM-008). Visibility is a reference contract, not secrecy.

**Chain resolution.** `realm-resolve` resolves the chain of every entity of a Realm and its sub-realms. Each hop resolves its target against the Realm holding that hop's entity: a short EUID, or one qualified with that Realm or a sub-realm of it, names an entity of the same Realm tree; any other qualifier must be a declared dependency of that Realm, with that Realm's version pin. Dependencies of dependencies are therefore loaded on demand when a chain reaches them, relative to the dependency's own directory (rebased onto its repository when it was fetched), and cached by `realm_id@version`. Entities inside a dependency's sub-realm directories resolve their targets against the dependency's root `dependencies`.

### 2.3 Rule

A Rule is the unit of the model (RUL-001). Rules are expressed as single phrases and are uniquely identified using the Rule Identifier Format.

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
| `overrides` | string (optional) | ID of the inherited rule this one replaces within the inheriting entity's effective rule set (RUL-009), short or `<realm-id>:<EUID>-<NNN>`. Omitted when the rule overrides none. |

**Immutability constraint:** Once a rule is in any state other than Draft (`D`), it is immutable. Modifications must be introduced by superseding it with a new rule.

**No ID reuse:** Rule identifiers must not be reused, even if the rule is deprecated, terminated, or abandoned.

### 2.4 Rule Set

A Rule Set groups all rules applicable to a specific entity. Every entity has exactly one Rule Set. A rule belongs to exactly one Rule Set (the one for its entity).

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Semantic versioning format. |
| `timestamp` | string | UTC ISO timestamp of the last change. |
| `hash` | string or null | `blake3:<hex>` hash of the canonical JSON content. |

**Version bumping:** The Rule Set version must bump (to a strictly higher semver value) whenever any rule or note in the set changes — whether a revision increment, a state transition, a new rule, or a note added or revised. The system only enforces that `new_version > old_version`. The `entityops` operations follow this convention: minor on a state transition that promotes or retires a rule (D→P, P→I, I→R, R→T); patch on everything else (rule add, body update, abandon, supersede, note add, note update).

**Visibility and `realm.version`:** narrowing an entity's `visibility` can break Realms that inherit it, so it needs a MAJOR bump of `realm.version`; widening needs a MINOR one. `strspc diff` says which (RD020); it can't enforce it, because it doesn't read `realm.json`.

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
| `revision` | integer | Starts at `0` when the note is added and increments by one on each change to its content or type. |

Notes are stored in the same entity file as the rules they reference and are edited in place, never deleted:

- A note is never removed once added. A note that no longer applies is revised in place.
- Notes of type `changelog`, `supersedes`, `extends` and `related` are immutable: neither their content nor their type may change once added.
- Any other note may be revised; each change to its content or type increments its `revision` by exactly one.
- Note IDs under a rule are sequential from `01`. Since no note is ever removed, an ID is never reused.

Every note is rendered and sent to the evaluator; there is no latest-wins selection.

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
| `extends` | Declares that this rule builds on another rule. The AI evaluator reads the body of each live rule it names alongside this rule, one hop only. | Rules |
| `related` | Declares a relationship to another rule or entity. | Rules, Rule Sets |

### 2.7 Sub-Entities

Entities may contain sub-entities. A sub-entity has its own EUID, its own rules, its own Rule Set, and its own notes — but it is nested within its parent entity's file to maintain coherency.

Each sub-entity declares a `parent` field referencing the containing entity's ID.

Sub-entities follow the same schema as top-level entities. The nesting can be arbitrarily deep (e.g., Entity → Sub-Entity → Sub-Sub-Entity), though in practice one or two levels suffice.

### 2.8 Realm

A **Realm** is the namespace of Entity Unique Identifiers (RLM-001). It is the organizational boundary for rule authoring — every entity file resides in exactly one Realm's directory tree (RLM-007).

**Realm Identifier:** Each Realm has an identifier that follows reverse domain notation (e.g., `dev.steerspec.core`, `com.acme.security`) to ensure global uniqueness across organizations (RLM-008). The identifier pattern is `^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)*$`.

**Structure:** A Realm directory holds:

- Its `_schema/` directory (RLM-003).
- Its `realm.json` manifest (RLM-004).
- One or more entity JSON files.

**Rule Identifier Format:** Each Realm carries a Rule Identifier Format (RLM-002) that determines how rule identifiers are rendered. If not overridden, the Realm inherits the RLIFRMT defaults (see §4). A sub-realm inherits its parent Realm's format (RLM-015) and leaves `rule_identifier_format` null (RM013); a Realm never inherits the format of a Realm it depends on.

**Dependencies:** A Realm can import the rules of other Realms read-only (RLM-005), and its EUIDs stay clear of theirs (RLM-006).

**Cross-Realm References:** Entity references across Realm boundaries MUST use the qualified form `<realm-id>:<EUID>` (e.g., `dev.steerspec.core:ENT`). Within the same Realm, the short form (EUID only) is sufficient (RLM-009).

#### Realm Manifest

The Realm Manifest is `realm.json` (RLMMNFST-001). It declares the Realm's identity and configuration:

| Field | Type | Description |
|-------|------|-------------|
| `realm.id` | string | Reverse domain notation identifier. |
| `realm.title` | string | Human-readable name. |
| `realm.version` | string | Semantic version of the Realm. |
| `realm.language` | string | Natural language of the Realm as a BCP 47 tag; `en` is the only accepted value (RLMMNFST-006). Absent means `en` for a top-level Realm (RLM-017). A sub-realm leaves it out and inherits its parent's language (RLM-018, RM017). |
| `dependencies` | array | Array of `{ realm_id, version, source }` objects referencing other Realms (RLMMNFST-003). `source` is a path relative to the manifest's directory (`../core/`) or a remote reference (`github://SteerSpec/strspc-rules@v1.13.0/rules/core`, see §6.4). |
| `sub_realms` | array of strings or null | Subdirectory names of child Realms (RLMMNFST-005). Null or absent if none. |
| `rule_identifier_format` | object or null | Override RLIFRMT defaults (RLMMNFST-004): optional `brackets`, `euid_position`, `id_splitter`, `revision_splitter` and `state_splitter` (§4); an absent field keeps its default. `null` keeps all defaults, or for a sub-realm the parent's format. |

RLMMNFST-002 sets the required fields. Dependencies use pinned version references (RLMMNFST-003): `realm-resolve` loads the dependency's own `realm.json` from its `source` and requires `realm.id` and `realm.version` to match the declared `realm_id` and `version` exactly (RR003, RR004). A dependency's `source` MUST be relative or remote; absolute paths are rejected (RR001).

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

**EUID uniqueness:** RLM-013 extends RLM-001 to a Realm tree. No EUID may appear in both the parent and a sub-realm, or in two sibling sub-realms.

**Dependency inheritance:** A sub-realm inherits its parent's dependencies (RLM-014). It MAY declare additional dependencies of its own; a sub-realm dependency with the same `realm_id` as a parent dependency overrides it. `realm-resolve` resolves this effective list for each sub-realm — inherited entries reuse the parent's resolution, own entries are resolved relative to the sub-realm directory — checks sub-realm EUIDs against the effective dependency set (RR011), and checks each own dependency's EUIDs against the dependencies already in that set (RR005).

### 2.9 Configuration

The **Configuration** is the consumer-side complement to a Realm. Where a Realm defines rules, the Configuration declares which rules a project subscribes to and how they are enforced.

A Configuration is the `.strspc/` directory of a project (SPCFG-001), with its `config.yaml` (SPCFG-002) and an optional local cache (SPCFG-003).

A project may have both a Configuration (consuming rules) and a local Realm (authoring rules). They are complementary — the Configuration MAY reference a local Realm as a rule source (SPCFG-005).

**Severity principle:** Severity belongs to the consumer, not the rule (SPCFG-008). A rule defines what must be true; how critical a violation is depends on the consuming project's context.

#### Rule Sources

The Configuration lists its rule sources (SPCFG-004), each one in the `rules` array of `config.yaml` (SPCFGSRC-001) with:

| Field | Type | Description |
|-------|------|-------------|
| `source` | string | Where rules are fetched from (remote URL or local path). |
| `scope` | string | Applicability: `global` (all files) or `local` (source's own directory tree). |

Source formats include remote references (`github://SteerSpec/strspc-rules@v1/rules/core`) and local Realm directories (`./rules/`).

#### Evaluator

The optional `evaluator` object (SPCFG-006) declares:

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

The optional `fail_on` array (SPCFG-007) contains state codes (`D`, `A`, `P`, `I`, `R`, `T`) — when a rule in one of these states is violated, enforcement fails. Violations of evaluated rules whose state is not listed in `fail_on` are reported as warnings.

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
- Notes carry their own revision counter with the same semantics, independent of the rule's (§2.5).

---

## 4. Rule Identifier Format

The displayed form of a rule identifier (e.g., `[ENT-001.0/D]`) is governed by the `RLIFRMT` entity in the Core Rule Set. This entity defines:

- The bracket pair: `()`, `{}`, or `[]` (default: `[]`). Manifest field `brackets`: `"(,)"`, `"{,}"` or `"[,]"`.
- The position of the Entity Unique Identifier (opening or closing, default: opening). Field `euid_position`: `"opening"` or `"closing"`.
- The position of the Rule Set Unique Number (opposing the EUID).
- The split character between EUID and number: `-` or `_` (default: `-`). Field `id_splitter`.
- The revision splitter: `.` or `#` (default: `.`). Field `revision_splitter`.
- The state splitter: `/`, `\`, or `|` (default: `/`). Field `state_splitter`.

The JSON stores `id`, `revision`, and `state` as separate fields. The rendered identifier is assembled by the rendering layer according to `RLIFRMT` rules. Different Realms may define different formatting without changing the underlying data: the format affects only the rendered identifier, never the stored `<EUID>-<NNN>` form that entity files, note references and cross-realm references carry (RLIFRMT-008). For example, `{"brackets": "{,}", "euid_position": "closing", "revision_splitter": "#"}` renders rule `ENT-001` at revision 2 in state `I` as `{001-ENT#2/I}`.

`render.NewRuleIDFormatter` builds the formatter from a Realm's `rule_identifier_format`, and `render.WithRuleIDFormatter` passes it to the renderer. The caller resolves the Realm: for an entity of a sub-realm, it passes the parent Realm's format.

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
| `remote` | Yes (cache) | Downloads `github://` references into the local cache, and reports what it holds without the network. Shared by `rule-resolve` and `realm-resolve`. |
| `rule-refs` | No | Finds rule identifiers cited in text, such as prompts and READMEs, and judges each one against a set of known rules. Also reports text that copies a rule's body. Takes the text from its caller. |

### 6.3 Diagnostic Codes

Every diagnostic code a module emits is listed below (MOD-006). The test `src/result/codes_doc_test.go`
scans the module sources and fails when a code is missing from these tables, or listed here but no longer
emitted. Severities are those assigned at emission: `rule-lint`, `rule-diff`, `realm-lint` and `rule-refs` promote
every Warning to Error when run with `WithStrict`.

Each module emits codes only from the prefix named in its heading (MOD-008), and a code that has shipped is
never reused or renumbered (MOD-003). The registry `src/result/testdata/reserved_codes.txt` lists every
shipped code as `active` or `retired` with a stable meaning slug; `src/result/codes_lifecycle_test.go`
enforces both rules against it and compares the registry with the one shipped by the latest release tag, so
a shipped line can neither be deleted nor change meaning. A retired check keeps its row below, marked
retired, and its code is never emitted again.

`result.Codes()` and `result.LookupCode()` in `src/result/codes.go` export every code below to consumers, with
a short title and a fix (MOD-009). `docs/diagnostic-codes.json` is the same data for consumers without a Go
toolchain; regenerate it with `go test ./src/result -run TestCodesJSONIsCurrent -update`. `Severity` and
`Summary` repeat the cells below word for word, and `src/result/codes_doc_test.go` fails when the registry
differs from these tables, from `reserved_codes.txt`, or from the JSON file.

The test-only package `src/qa` implements MOD-010. It runs a clean Realm and a set of deliberately broken ones
through every module and compares the exact diagnostics with each scenario's `expected.json`; what no file can
provoke, such as a cancelled context or a failed provider, is a table of Go cases. `TestEveryCodeIsCovered` fails when a code in `result.Codes()` has no scenario, no Go case and no
exception with a stated reason, so a new code needs a scenario in the change that adds it.
`src/qa/testdata/README.md` says how to add one.

#### `rule-lint` (RL)

| Code | Severity | When it fires |
|------|----------|---------------|
| RL000 | Error | The entity file is nil, or a file or directory cannot be read while scanning. |
| RL001 | Error | The file is not valid JSON or does not parse into the entity structure. |
| RL002 | Info / Error | Info if no schema fetcher is set; Error if the schema fails to load or validation fails. |
| RL003 | Error | The EUID is not 3–18 alphanumeric characters. |
| RL004 | Error | A rule ID is not `<EUID>-<NNN>`, or its prefix differs from the entity ID. |
| RL005 | Error | Rule numbers are duplicated or not sequential from 001. |
| RL006 | Error | A rule state is outside `D`, `A`, `P`, `I`, `R`, `T`. |
| RL007 | Error | A sub-entity `parent` differs from the containing entity ID. |
| RL008 | Error | A note `rule_ref` names a rule that does not exist in the same entity. |
| RL009 | Error | A note ID is not `<rule_id>/<NN>`, or its prefix differs from `rule_ref`. |
| RL010 | Error | A note type is outside the closed enum. |
| RL011 | Error | `rule_set.hash` is malformed, cannot be computed, or differs from the Blake3 hash of the file. |
| RL012 | Warning | A rule's `supersedes` names a rule ID not found in the scanned files. |
| RL013 | Error | `rule_set.version` is not valid semver. |
| RL014 | Warning | A `supersedes`/`extends`/`related` note references an unqualified rule ID that was not found. |
| RL015 | Error | Note numbers under a rule are duplicated or not sequential from 01. |
| RL016 | Warning | An unqualified `inherits` target is not an entity in the scanned files, or an unqualified `overrides` target is not a rule in them. |
| RL017 | Error | Two live rules of one entity override the same target. A rule is live unless it is `A` or `T` or a rule that is not `A` supersedes it. |
| RL018 | Error | An `inherits` chain followed through the scanned files (or, for a single file, through its own entities) is cyclic or longer than three hops. |
| RL019 | Warning | The body of a `D`, `P` or `I` rule references an unqualified rule ID that was not found. Text in backticks is ignored. |
| RL020 | Warning | The body of a `D`, `P` or `I` rule, or an `extends`/`related` note on one, references an unqualified rule ID in state `A`. Text in backticks is ignored in bodies. |
| RL021 | Warning | A `D` rule's body has no key word from KWRD-002 to KWRD-006, or `DEFINES`. Text in backticks and rule IDs are ignored. |
| RL022 | Warning | A `D` rule's body has more than one sentence (RUL-004). Text in backticks is ignored. |
| RL023 | Error | An entity declares a `visibility` other than `private`, `protected` or `public` (ENT-011). |
| RL024 | Error | A sub-entity declares a `visibility` wider than the effective visibility of the entity containing it (ENT-015). |
| RL025 | Error | A rule's `supersedes` names a rule of another entity (RUL-012). |

#### `rule-diff` (RD)

| Code | Severity | When it fires |
|------|----------|---------------|
| RD000 | Error | The base or head entity file is nil, cannot be read, or cannot be parsed. |
| RD001 | Error | A Draft rule body was edited without incrementing the revision, or together with a state change. |
| RD002 | Error | The body of a rule that has left Draft was edited. |
| RD003 | Error | A state transition is not allowed by the state machine (§3.2). |
| RD004 | Error | A new rule has revision ≠ 0 or state ≠ `D`, or new rule numbers are not consecutive. |
| RD005 | Error | A rule, sub-entity or entity file present in the base version was deleted. |
| RD006 | Warning | A superseding rule reached `I` but the superseded rule is missing or not in state `R`. |
| RD007 | Error | `rule_set.version` is not valid semver or not strictly higher than the base version. |
| RD008 | Error | `rule_set.hash` cannot be computed or differs from the Blake3 hash of the head file. |
| RD009 | Error | `rule_set.timestamp` is unchanged from the base version. |
| RD010 | Error | A new rule reuses a rule number present in the base version. |
| RD011 | Error | A new rule or new note has an empty `added_by`. |
| RD012 | Error | An append-only note (`changelog`, `supersedes`, `extends`, `related`) was removed or retyped. |
| RD013 | Error | A note's content or type changed without its revision incrementing by exactly 1, or its revision changed without a content or type change. |
| RD014 | Error | A note that is not append-only and is present in the base version was removed. |
| RD015 | Error | A new note has revision ≠ 0. |
| RD016 | Error | The content of an append-only note changed. |
| RD017 | Error | `inherits` changed on an entity whose base version has a rule beyond Draft, or was removed on one whose base version has a Published or Implemented rule. |
| RD018 | Error | `overrides` was added, removed or retargeted on a rule that has left Draft. |
| RD019 | Error | A new rule or new note has an `added_by` that is not an email address or `@handle`. |
| RD020 | Info / Error | `visibility` changed. Info for a new or changed value, naming the `realm.version` bump a narrowing or widening needs; Error when the key is removed. |

#### `realm-lint` (RM)

RM002, RM003, RM004, RM007 and RM014–RM016 fire for the parent manifest and for each sub-realm manifest.
RM015, RM016 and the manifest half of RM014 need a dependency fetcher, and are skipped without one. A sub-realm that pins no core Realm of its own is compared with its parent's.
RM002 and RL002 validate a Realm against the schemas of the core version it pins. The caller selects the core Realm's own `_schema/` explicitly (`--core`); a Realm declaring the core ID does not. Any other Realm's `_schema/` never decides validation. A sub-realm that pins no core Realm of its own uses its parent's.
RM999 is not emitted by `realm-lint`; it appears only as a placeholder in its strict-mode unit test.

| Code | Severity | When it fires |
|------|----------|---------------|
| RM001 | Error | `realm.json` cannot be read or parsed. |
| RM002 | Info / Error | Info if no schema fetcher is set; Error if the schema fails to load or validation fails. |
| RM003 | Error | `_schema/` is missing, inaccessible, or not a directory. |
| RM004 | Error | `_schema/entity.v1.schema.json` is missing or inaccessible. |
| RM005 | Info / Error | Info if no rule linter is configured; Error if a path cannot be accessed or the walk fails. |
| RM006 | Error | The same EUID appears in more than one entity or sub-entity of the realm. |
| RM007 | Error | `realm.id` is missing or not reverse-domain notation, or `realm.version` is missing or not semver. |
| RM008 | Error | A `sub_realms` entry is not a clean directory name, or its directory does not exist. |
| RM009 | Error | A sub-realm has no `realm.json`, or it cannot be parsed. |
| RM010 | Error | A sub-realm `realm.id` is not `<parent id>.<directory name>`. |
| RM011 | Error | A sub-realm declares its own `sub_realms`. |
| RM012 | Error | An EUID in a sub-realm collides with the parent realm or a sibling sub-realm. |
| RM013 | Error | A sub-realm `realm.json` sets `rule_identifier_format`; a sub-realm inherits its parent's format (RLM-015). |
| RM014 | Error | A dependency's `version` differs from the release tag in its `github://` `source`, or the dependency's own manifest declares another `realm.id` or `realm.version`. |
| RM015 | Warning | A file in `_schema/` holds different JSON from the file of the same name in the pinned `dev.steerspec.core`, or cannot be compared with it. Files the core lacks are ignored, as are core files the Realm lacks. Skipped when the fetched core is not the one pinned, which RM014 reports. |
| RM016 | Warning | A pinned dependency or its manifest cannot be fetched or read, so the manifest half of RM014 and any RM015 comparison with it could not run. |
| RM017 | Error | A sub-realm `realm.json` sets `realm.language`; a sub-realm inherits its parent's language (RLM-018). |

#### `realm-resolve` (RR)

| Code | Severity | When it fires |
|------|----------|---------------|
| RR000 | Error | The context was cancelled or timed out during resolution. |
| RR001 | Error | A dependency `source` is empty, absolute, or escapes the repository root of the realm's origin. |
| RR002 | Error | Dependency directory or `realm.json` cannot be loaded, or an entity file is unreadable or invalid. |
| RR003 | Error | A dependency's `realm.id` differs from the declared `realm_id`. |
| RR004 | Error | A dependency's `realm.version` differs from the declared `version`. |
| RR005 | Error | An EUID collides between the parent realm and a dependency, or between two dependencies. |
| RR006 | Error | A dependency's `realm_id` is the realm or sub-realm that declares it. |
| RR007 | Error | A `sub_realms` entry is not a clean directory name, or its directory does not exist. |
| RR008 | Error | A sub-realm's `realm.json` cannot be loaded. |
| RR009 | Error | A sub-realm `realm.id` is not `<parent id>.<directory name>`. |
| RR010 | Error | A sub-realm declares its own `sub_realms`. |
| RR011 | Error | An EUID in a sub-realm collides with the parent, a sibling sub-realm, or an effective dependency. |
| RR012 | Error | A `github://` dependency cannot be fetched (bad reference, no fetcher or cache dir, download error). |
| RR013 | Error | An `inherits` target cannot be resolved: no such entity, a qualifier that is not a dependency of the hop's Realm, a dependency that failed to resolve, or a short EUID that only exists in a dependency. |
| RR014 | Error | An inheritance chain, followed across Realms, is cyclic or longer than three hops. |
| RR015 | Error | A live rule's `overrides` target is not in the entity's inherited rule set: the entity inherits nothing, the target is unknown, or an ancestor already overrides it. |
| RR016 | Warning | A live rule's `overrides` target exists in the chain but is not Published or Implemented, so it is not inherited and the override has no effect. |
| RR017 | Error | A `supersedes`/`extends`/`related` note of a live rule qualifies a reference with a Realm outside the tree that is neither a dependency of the note's Realm nor a declared sub-realm of one (NTE-007). |
| RR018 | Error | A `supersedes`/`extends`/`related` note of a live rule qualifies a reference with a known Realm that holds no such rule at the pinned version. |
| RR019 | Warning | An `extends`/`related` note or the body of a live rule makes a qualified reference to a rule that is Abandoned, Terminated or superseded. Text in backticks is ignored in bodies. |
| RR020 | Warning | The body of a live rule qualifies a reference with a Realm outside the tree that is neither a dependency of the rule's Realm nor a declared sub-realm of one. Text in backticks is ignored. |
| RR021 | Warning | The body of a live rule qualifies a reference with a known Realm that holds no such rule at the pinned version. Text in backticks is ignored. |
| RR022 | Error | An entity inherits an entity whose visibility does not permit inheritance from its Realm (ENT-014, ENT-016). |
| RR023 | Error | A rule that is not Abandoned, Retired or Terminated overrides a rule, and the visibility of an entity from the inherited entity up to the rule's owner does not permit it from the declaring Realm, whatever the state of the target (ENT-013, ENT-014, ENT-016). |
| RR024 | Error | An entity is wider than the entity it inherits (ENT-015). An absent `visibility` counts as `public`. |
| RR025 | Error | An `extends` note on a rule that is not Abandoned, Retired or Terminated references a rule whose entity's visibility does not permit inheritance from the note's Realm or is narrower than that of the entity holding the note (NTE-012). Short references count. |

#### `rule-resolve` (RSV)

RSV001 is unassigned. `rule-resolve` has no strict mode.

| Code | Severity | When it fires |
|------|----------|---------------|
| RSV000 | Error | The context was cancelled or timed out during resolution. |
| RSV002 | Error | A local source path cannot be resolved, is not a directory, or cannot be walked or read. |
| RSV003 | Error | An entity file cannot be parsed, or its `entity.id` is empty. |
| RSV004 | Error | `rule_set.hash` cannot be computed or differs from the Blake3 hash of the file. |
| RSV005 | Error | The same EUID is provided by two different sources. |
| RSV006 | Warning | A source directory yields no entity files and produced no errors. |
| RSV007 | Error | A `github://` source cannot be parsed, has no fetcher, or cannot be downloaded. |

#### `rule-eval` (RE)

| Code | Severity | When it fires |
|------|----------|---------------|
| RE001 | Warning / Error | The diff violates a rule; Error when the rule's state is in `fail_on`, Warning otherwise. |
| RE002 | Warning | The AI provider failed for a rule or entity, or returned no verdict or an unrecognized one. |
| RE003 | Info | AI evaluation skipped because the evaluator runs in static-only mode. |
| RE004 | Info | The diff is empty; nothing to evaluate. |

#### `rule-refs` (RF)

The stored `<EUID>-<NNN>` form always matches, which also covers a rendered identifier under the default format. A Realm's other rendered shapes match only that Realm's EUIDs. An unknown prefix is reported only when it is uppercase and starts with a letter, so `feat-123` and `100-200` are not citations. URLs, identifiers inside a longer hyphenated token, identifiers passed to `WithIgnore` and states passed to `WithAllowedStates` are skipped, as is a line holding `strspc-refs: ignore` and the line after it. A superseder in `D` or `A` replaces nothing.

RF007 compares whole bodies in every rule state, so `WithAllowedStates` doesn't exempt it. `WithIgnore` and the `strspc-refs: ignore` marker do, the marker on the line the copy starts on. Rules sharing a body are reported under the first one found. Rendered entity files are copies by construction, so leave them out of the scan. A paraphrase passes RF007; DOC-001 leaves that to the evaluator.

| Code | Severity | When it fires |
|------|----------|---------------|
| RF001 | Error | The cited rule does not exist in the sources, or the citation names a Realm that is not one of them. |
| RF002 | Error | The cited rule is in state `A`. |
| RF003 | Warning | The cited rule is in state `R` or `T`, whether or not a rule supersedes it. |
| RF004 | Error | The cited rule, in `D`, `P` or `I`, is superseded by a rule in `I`, `R` or `T`. |
| RF005 | Warning | The cited rule, in `D`, `P` or `I`, is superseded by a rule in `P`, so it stays in force until that rule reaches `I` (§3.3). |
| RF006 | Warning | The cited rule is in state `D` and no rule supersedes it. |
| RF007 | Error | The text holds the whole body of a rule of five words or more, ignoring case, punctuation and line breaks. |

#### `eval-cache` (EC)

| Code | Severity | When it fires |
|------|----------|---------------|
| EC001 | Info | The evaluation cache file was corrupt and has been discarded and recreated. |

### 6.4 Remote Sources (`github://`)

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

**Authentication.** Private repositories and higher rate limits need a token. The resolvers read `GITHUB_TOKEN`, then `GH_TOKEN`, from the environment and send it as `Authorization: Bearer <token>`, but only for sources owned by `SteerSpec`, by `GITHUB_REPOSITORY_OWNER`, or by an owner listed in the comma-separated `STRSPC_TOKEN_OWNERS`. Any other source is fetched anonymously, because a source can come from a `realm.json` the user only cloned. A 404 without a token is reported as "private repo or missing token", and a 404 where the token was held back names `STRSPC_TOKEN_OWNERS`.

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
14. Relation notes (`supersedes`, `extends`, `related`): every unqualified `<EUID>-<NNN>` reference in the note content points to an existing rule ID within the scanned realm. Qualified `<realm-id>:<EUID>-<NNN>` references target another realm and are checked by `realm-resolve` (RR017 to RR019).
15. Note numbers under each rule are sequential from `01` — no gaps, no duplicates.
16. Inheritance references: an unqualified `inherits` target is an entity, and an unqualified `overrides` target a rule, within the scanned realm. Qualified targets are resolved by `realm-resolve`.
17. Within an entity, at most one live rule overrides a given target.
18. Inheritance chains within the scanned realm do not cycle and do not exceed three hops.
19. Bodies of `D`, `P` and `I` rules: every unqualified `<EUID>-<NNN>` reference outside backticks points to an existing rule ID within the scanned realm. Qualified references are checked by `realm-resolve` (RR019 to RR021).
20. Bodies of `D`, `P` and `I` rules, and their `extends` and `related` notes, do not reference a rule in state `A`.
21. Bodies of `D` rules contain a key word from KWRD-002 to KWRD-006, or `DEFINES`, outside backticks and rule IDs.
22. Bodies of `D` rules are a single sentence (RUL-004). Other bodies are immutable, so checks 21 and 22 do not apply to them.
23. A declared `visibility` is `private`, `protected` or `public`, and no wider than the effective visibility of the containing entity (ENT-015).
24. A rule's `supersedes` names a rule of the same entity (RUL-012).

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
12. **Notes never removed:** Every note present in the base version must still be present. Append-only notes (`changelog`, `supersedes`, `extends`, `related`) must also keep their type and content unchanged.
13. **Note edited:** When a note's content or type changes, its revision must equal `previous_revision + 1`. A revision change without a content or type change is rejected.
14. **New note added:** Must have `revision: 0`.
15. **Inheritance is stable:** Once an entity has a rule beyond Draft, its `inherits` may not change, and may not be removed while a rule is Published or Implemented; adding it where there was none is allowed.
16. **Overrides are immutable:** Once a rule has left Draft, its `overrides` may not be added, removed or retargeted; supersede the rule instead.
17. **Visibility changes are reported:** A new or changed `visibility` is reported as Info, naming the `realm.version` bump it needs. Removing the key is rejected: set `public` explicitly.

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
6. The AI returns exactly one verdict per rule — compliant, violated, or not applicable — as a structured object keyed by rule id, so the output schema itself guarantees a verdict for every rule sent; a second request per violated rule then re-reads the diff with that single rule in focus and either confirms the verdict with its explanation and evidence locations (file, line, excerpt) or withdraws it (`compliant` or `not_applicable`, explanation kept, counted in `Usage.Withdrawn`). `not_applicable` is the default when the rule's subject is absent from the diff.
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

