# QA realm fixtures

These fixtures prove that every diagnostic code fires through a public API. `clean/` passes every module. Each directory under `broken/` is a themed scenario that runs through every module it has input for, and `src/qa` compares the exact diagnostics with the scenario's `expected.json`. Codes no file can provoke, such as a cancelled context or a failed provider, are the Go cases in `src/qa/cases_test.go`.

**These fixtures are broken on purpose.** The rule against hand-editing rule JSON does not apply here: `strspc rule ...` would refuse most of what they hold, so edit them by hand, then run `-update` to restore their hashes. Don't copy them as examples of valid rules, except `clean/`.

## Layout

| Path | Holds |
|------|-------|
| `clean/` | A Realm with a sub-realm, a dependency, `inherits`, `overrides`, supersession, all six states and every note type. |
| `broken/<scenario>/realm/` | The Realm under test. `rule-lint`, `realm-lint`, `realm-resolve`, `rule-resolve` and `rule-eval` read it. |
| `broken/<scenario>/base/`, `head/` | Flat directories of entity files for `rule-diff`. Keep every file changed, and keep `realm.json` out. |
| `broken/<scenario>/cited/` | Text files citing rules, for `rule-refs`. |
| `broken/<scenario>/deps/` | Dependency Realms, beside `realm/` and never inside it. |
| `broken/<scenario>/scenario.json` | What the files cannot say (below). Written by hand; `-update` never rewrites it. |
| `broken/<scenario>/expected.json` | The exact `{module, code, severity, path}` list, sorted. Paths are relative to the scenario, with `:line` when the diagnostic has a line. |

`scenario.json` holds:

| Key | Meaning |
|-----|---------|
| `claims` | Required. The codes the scenario exists to fire. They must fire even under `-update`, so regenerating cannot quietly lose one. |
| `bad_hash` | Files, relative to the scenario, that are wrong on purpose or do not parse. `-update` leaves them alone; any other file with a bad hash fails `TestFixtureHashes`. |
| `rule_sources` | The `rule-resolve` sources relative to the scenario. Defaults to `["realm"]`. |
| `eval` | The `rule-eval` runs, each with `diff`, `violated`, `fail_on` and `static_only`. Defaults to one run in which no rule is violated. |

## Add a scenario

1. Copy the closest scenario, or `clean/`, to `broken/<theme>/` and break one theme. Scenarios are themed, not one per code: group the codes that can fire together, and split where one failure short-circuits another, such as a manifest that does not parse.
2. Write `scenario.json`, with the codes you meant to fire in `claims`.
3. Give every Realm and sub-realm a `_schema/entity.v1.schema.json`, or RM003 and RM004 fire. A copy of `rules/_schema/` will do: validation always uses `rules/_schema/`, not the fixture's.
4. Run `go test ./src/qa -update`, then read `expected.json` line by line against docs/SPEC.md §6.3. Regenerating records what the engine does, not what it should do.

If the engine contradicts the spec, leave the case out, add a row with `bug: true` to `exceptions` in `src/qa/codes_test.go`, and report the bug. Git keeps no empty directory, so a fixture that needs one holds a `.gitkeep`.

A new code in `result.Codes()` fails `TestEveryCodeIsCovered` until a scenario or a Go case fires it.

## Regenerate

```bash
go test ./src/qa -update
git diff src/qa/testdata
```

`-update` stamps `rule_set.hash` with `entity.ComputeHash`, sub-entities included, skips the `bad_hash` files, and rewrites every `expected.json`. Review the diff by hand.

## Manual QA

Point `strspc` at a fixture from the repository root:

```bash
strspc lint --cross-ref src/qa/testdata/clean/realm
strspc realm validate src/qa/testdata/clean/realm
strspc lint --cross-ref src/qa/testdata/broken/note-structure/realm
strspc realm validate src/qa/testdata/broken/manifest-fields/realm
```

The CLI wires the modules its own way, so its output is not `expected.json`: it fetches schemas by the Realm's pin, and `strspc diff` compares git refs, not `base/` and `head/`. `strspc refs` reads `.strspc/config.yaml`, so run it from a project configured with a fixture Realm as a source.
