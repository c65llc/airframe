# Airframe 4 · Consumer rules with the wrapper map — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `airframe lint` scans each platform's `consumers` files and checks every token reference, whether direct or through a declared wrapper. It enforces tiers and component ownership, catches references to tokens that don't exist, catches literal CSS fallbacks, and reports wrapper reads that aren't mapped. The checks run in all of lint's output formats, the gate included, with tallies and a self-test.

**Architecture:**
- A new package, `internal/consumer`, finds the consumer files, runs one regex adapter per file type, resolves wrapper reads and returns `Reference`s.
- The names an adapter recognises come from the emitters themselves, through a new `emit.ReferenceNames`. A consumer reference and the generated code therefore can't drift.
- Five lint rules sit on the scan.
- Component ownership comes from a minimal `components/<id>.yaml` loader. Plan 5 extends it with rulings, the inventory and `component-unruled`.

**Tech Stack:** Go 1.27 (`toolchain go1.27.4`), pinned deps only. Glob matching uses doublestar v4.10.7, already a dependency; RE2 handles regexes.

**Spec:** `docs/specs/2026-11-27-airframe-design.md`:
- §5, "Consumer scanning" and "Hand-written wrappers";
- §6.2, platforms' `consumers`;
- §6.3, the Component record;
- §6.4, the Consumer rules row and the bullets under it;
- §6.7 item 4.

## Global Constraints

- **License header** on every Go file: `// SPDX-License-Identifier: AGPL-3.0-only`, then `// Copyright 2026 C65 LLC`.
- **Deterministic output:** findings are sorted, and no map is ranged unsorted.
- **Fail closed:**
  - a declared input that is never scanned is reported, never skipped;
  - "reads are always declared, never inferred";
  - "owners are declared, never inferred".
- **Exemptions need reasons:** every rule reports what it scanned in a `diag.Tally`, and an exemption with no reason exempts nothing.
- **New lint IDs** each need: an entry in `lint` with a default severity, `docs/rules/<id>.md` with `since: v0.6.0`, and a self-test flag fixture under `internal/selftest/fixtures/flag/<id>/`. The clean fixture must stay clean.
- **Exit codes and safety:** exit codes are 0, 1 or 2. Reads go through the project FS, and lint never writes.
- **Public repo:** no private-project names, paths or emails.

## Decisions this plan makes

### Config: wrappers (Task 1)

`platforms.<p>.wrappers` is a list, and each entry declares one root:

```yaml
wrappers:
  - root: tokens                       # `tokens.<member>` is a token read
    generated: true                    # members are the platform's generated names (as DesignTokens.<member>)
  - root: AppTheme.spacing
    map: { small: space-2, hairlineStrong: hairline }
    patterns: [ { match: '^space(\d+)$', token: 'space-{1}' } ]
```

- `root` is one or more dotted identifiers (`[A-Za-z_][A-Za-z0-9_]*`, dot-separated). Two entries on one platform with the same root is schema-invalid.
- Each entry has exactly one of two shapes. Anything else is schema-invalid:
  - `generated: true`;
  - at least one of `map` and `patterns`.
- `map` values are non-empty strings.
- In `patterns`:
  - `match` must compile under RE2, and `token` must be a non-empty template;
  - `{1}`…`{9}` in `token` are replaced by capture groups, and a group number beyond the regex's groups is schema-invalid.
- Config can't see token names, so a mapped token that doesn't exist is reported at lint time, as `wrapper-unmapped`.

### Components (Task 1)

`<content>/components/<id>.yaml` is loaded into `project.Project.Components` (`map[string]*component.Component`, keyed by id).

| Field | Rules |
|---|---|
| `id` | Required, and must equal the file's basename. |
| `tokens` | Optional list of owned token names. |
| `owners` | List of repo-relative globs. Required and non-empty unless `shared: true`. |
| `shared` | Boolean, default false. |
| `noun`, `ruling`, `hostedBy` | Accepted, type-checked as strings (`hostedBy` may be null). |
| `implementations` | Accepted, type-checked as a string → string map. |
| anything else | Schema-invalid. |

Plan 5 gives `noun`, `ruling`, `hostedBy` and `implementations` their rules.

Consistency checks, each schema-invalid with the component file as the location:
- A listed token must exist, have tier `component` and have `component: <id>`.
- A `component`-tier token whose `component` names X must appear in X's `tokens` list, when X's file exists.
- A component-tier token whose component has **no file** is not a load error. Reading it from a consumer is a `token-owner-violation` (see below).

### Reference names (Task 2)

`func ReferenceNames(p *project.Project, pl config.Platform, format string) (map[string]string, error)` lives in `internal/emit`. It maps every name the format's emitter would write to the token it stands for. A value of `""` marks a generated symbol that isn't a token: an enum, a nested type, `Colors`, `Shadow`, init labels and so on.

| Format | Names |
|---|---|
| `css` | `--<name>`, per the css naming (`--{name}`). |
| `swift` | `DesignTokens` members, static and instance, plus nested type and enum names, with type part `<Type>`. |
| `kotlin` | `<Type>` members, plus `<Type>ColorValues.<appearance>.<slot>` slots, each mapped to its token. |
| `xaml` | Every key: base keys, runtime-suffixed keys, and shadow part keys (`<Key>Layer<N>OffsetX`, …), each mapped to its token. |

- It must reuse each emitter's own naming code: `NameFor`, the slot builders and the suffix builders. Never re-derive names.
- `TestReferenceNamesMatchEmitted` checks that every member, key and custom property in the fixture goldens appears in the map. Testdata: apple, android, windows, css.

### Scanner (Task 3)

`package consumer`:

```go
func Scan(p *project.Project) (Result, error)
```

`Result` has:
- `Refs []Reference`, where a `Reference` is `{Platform, File, Line, Col, Raw, Token, Via}`:
  - `Via` is `"direct"` or `"wrapper:<root>"`;
  - `Token` is `""` when the reference is unresolved.
- `Files map[string]int`: references examined per platform.
- `Problems []Problem`, where a `Problem` is `{Kind, Platform, File, Line, Col, Raw, Detail}`. `Kind` is one of `undeclared`, `wrapper-unmapped`, `fallback-literal` or `empty-scan`.
- `Exempt []Exemption`.

**Which files are scanned**
- Files are the repo-relative paths matching a platform's `consumers` globs, read from `p.FS`, where the project root is the directory holding `airframe.yaml`.
- Generated outputs and the content root are excluded.
- A platform whose globs match no file gets one `empty-scan` problem.

**Adapters, chosen by extension**

| Adapter | Extensions | Matches |
|---|---|---|
| css | `.css .scss .sass .less .html .htm .vue .svelte .js .jsx .mjs .cjs .ts .tsx` | `var(` + `--name`, then an optional `, fallback` scanned with balanced parentheses up to the matching `)`. |
| swift | `.swift` | `` \b<Type>\.(`?[A-Za-z_]\w*`?) `` |
| kotlin | `.kt .kts` | `\b<Type>\.(…)`, and `\b<Type>ColorValues\.\w+\.(\w+)` |
| xaml | `.xaml` | `\{(?:StaticResource\|ThemeResource)\s+([A-Za-z_]\w*)\s*\}` |
| xaml-in-C# | `.cs` | `Resources\["([A-Za-z_]\w*)"\]` |

- **Adapter checks:** an adapter runs only if its format is in the platform's `emit`. A consumer file whose extension has no adapter, or whose adapter's format the platform doesn't emit, is `schema-invalid`. That check reports one finding per platform, listing the files, at `platforms.<p>.consumers`.
- **css:** a name that's in the index resolves. A name that isn't is `undeclared` unless it's defined (`--name:`) in any consumer file scanned for that platform, which makes it app-local.
- **swift, kotlin, xaml:** a matched name is "ours" only if it fits the naming template's literal prefix and suffix. For `Design{Pascal}`, that means it starts with `Design`. An unmatched name that is ours is `undeclared`; any other name is ignored, so `{StaticResource SystemControlForegroundBaseHighBrush}` is left alone. A name that maps to `""` is ignored.
- **css fallbacks:** a fallback that doesn't itself start with `var(` is a `fallback-literal` problem. This holds whether or not it equals the token's value, and the detail says when it does.

**Wrappers**
- For each declared root on the platform, the scanner matches `(?:^|[^\w.])<root as escaped literal>\.([A-Za-z_]\w*)` in every consumer file of that platform, whatever its type.
- A member resolves in this order:
  1. `map`, by exact match;
  2. `patterns`, in order: the first match fills the template;
  3. `generated: true`, through the platform's ReferenceNames, trying the platform's native format first (swift, kotlin, xaml), then css.
- An unresolved member, or a resolved name that isn't a token, is a `wrapper-unmapped` problem.
- A resolved wrapper read is a `Reference`, `Via: "wrapper:<root>"`, and the tier and owner rules count it exactly like a direct read.

**Exemptions**
- An `airframe-exempt <rule>: <reason>` substring on the reference's line or the line before it suppresses that rule for that reference, and records an `Exemption`.
- A marker with an empty reason suppresses nothing. The finding's message then ends `(airframe-exempt on line N has no reason)`.

### Rules (Task 4)

All five rules read one memoised `consumer.Scan` per lint run, held in the lint `Context`.

| ID | Default | Fires when |
|---|---|---|
| `reference-undeclared` | error | `undeclared` or `empty-scan` problems |
| `primitive-read-by-consumer` | error | a resolved reference to a token whose tier's `readableBy` lacks `consumers` and whose tier isn't `component` |
| `token-owner-violation` | error | a resolved reference to a component-tier token from a file matching none of its component's `owners` globs, unless the component is `shared`. If the component has no `components/<id>.yaml`, the message says `component <id> declares no owners (no components/<id>.yaml)`. |
| `fallback-literal` | warn | `fallback-literal` problems |
| `wrapper-unmapped` | error | `wrapper-unmapped` problems |

- **Finding locations:** the consumer file, line and col. The message names the token and, for wrappers, the root.
- **Tallies:** `Files` is the consumer files scanned, `Items` is the references examined, and the noun is `references`.
- **Not applicable:** a rule with no input sets `NotApplicable`:
  - `no platform declares consumers` for all five rules;
  - `no platform declares wrappers` for `wrapper-unmapped`;
  - `no css consumer files` for `fallback-literal`.
- **Lint IDs:** they go into `lint.AllIDs`, with `docs/rules/*.md`, flag fixtures (each with a small consumer file) and `explain` support.

## Review Focus

1. **App-local CSS custom properties** (`--sidebar-w: 240px; width: var(--sidebar-w)`) must not be `reference-undeclared`. A real typo (`var(--accnet)`) must be. → Task 3, `TestCSSLocalProperty`.
2. **Foreign XAML or Swift symbols** (`{ThemeResource SystemControlForegroundBaseHighBrush}`, `DesignTokens.Density.compact`, `DesignTokens(density:)`) must not be `undeclared`. → Task 3, `TestForeignAndGeneratedSymbols`.
3. **Nested parentheses in a fallback** (`var(--x, rgb(0 0 0 / 0.5))`, and `var(--x, var(--y, 4px))`) must be parsed correctly. The first is a literal fallback. The second is not, but its inner `--y, 4px` is. → Task 3, `TestCSSFallbackNesting`.
4. **Wrapper precedence.** A member mapped both by `map` and by a pattern must take the `map` entry. A wrapper read of a component token from a non-owner file must be a `token-owner-violation`. → Task 3/4, `TestWrapperPrecedence`, `TestOwnerViaWrapper`.
5. **An empty scan.** A glob typo (`consumers: ["web/**/*.tssx"]`) must fail lint with `reference-undeclared` naming the platform and the glob, not pass clean. → Task 4, `TestEmptyScanFails`.

---

### Task 1: Config wrappers and the component loader

**Files:**
- Modify `internal/config/config.go` (`Platform.Wrappers []Wrapper`).
- Create `internal/component/component.go`, with parse and validation.
- Modify `internal/project/project.go` to load `components/*.yaml` and run the consistency checks.
- Add the schema in `internal/schema/` (`component.schema.json`, also served by `airframe schema component`).
- Tests: config, component and project.

**Interfaces:**
- `type Wrapper struct{ Root string; Generated bool; Map map[string]string; Patterns []WrapperPattern }`
- `type WrapperPattern struct{ Match *regexp.Regexp; Token string }`
- `type component.Component struct{ ID, File string; Line int; Tokens, Owners []string; Shared bool; Noun, Ruling, HostedBy string; Implementations map[string]string }`
- `project.Project.Components map[string]*component.Component`

- [ ] Step 1: write the tests.
  - `TestWrappersConfig`, a table of every schema-invalid case above, plus one valid set.
  - `TestComponentLoad`: valid; id ≠ basename; owners missing without `shared`; an unknown field; `implementations` of the wrong type.
  - `TestComponentConsistency`: a listed token that doesn't exist; a listed token that isn't component-tier; a component token not listed in its component.
  - The spec §6.2 and §6.3 examples load.
- [ ] Step 2: run them and see them FAIL.
- [ ] Step 3: implement.
- [ ] Step 4: run `go test ./...` and see it pass.
- [ ] Step 5: commit `feat: wrapper map config and component loader`.

### Task 2: ReferenceNames

**Files:**
- Create `internal/emit/refnames.go` and `refnames_test.go`.
- Refactor the emitters only to expose their naming. The goldens must stay byte-identical.

**Interfaces:**
- Produces `ReferenceNames` (above), plus `func NamingAffixes(cfg *config.Config, format string) (prefix, suffix string)`, the template's literal parts, for the "ours" test.

- [ ] Step 1: write the tests.
  - `TestReferenceNamesMatchEmitted`: every emitted member, key or property in the goldens is in the map. Parse them with simple regexes in the test.
  - `TestReferenceNamesTokens`: spot-check that `DesignGapCompact` maps to `gap`, a Kotlin slot to its token, and a shadow part to its token.
  - `TestNamingAffixes`.
- [ ] Step 2: run them and see them FAIL.
- [ ] Step 3: implement.
- [ ] Step 4: run the suite. The goldens must be unchanged.
- [ ] Step 5: commit `feat(emit): reference names for consumer scanning`.

### Task 3: The scanner

**Files:**
- Create `internal/consumer/{scan.go,adapters.go,wrappers.go}` and tests.
- Testdata: `testdata/consumers/`, a project with web, apple, android and windows platforms. It needs consumer files that exercise each adapter, a wrapper of each shape, exemption markers, a component with owners, and app-local CSS properties.

**Interfaces:**
- Consumes `ReferenceNames`, `NamingAffixes`, `Platform.Wrappers` and `Components`.
- Produces `consumer.Scan` and the types above.

- [ ] Step 1: write the tests.
  - `TestScanGolden`: the sorted `Refs` and `Problems` of the testdata, compared as JSON to `testdata/consumers/scan.golden.json`.
  - `TestCSSLocalProperty`, `TestForeignAndGeneratedSymbols`, `TestCSSFallbackNesting` and `TestWrapperPrecedence`.
  - `TestExemptionMarkers`: a marker with a reason, and one without.
  - `TestUnadaptedExtension`.
  - `TestScanDeterministic`: same result whatever order the files are walked in.
- [ ] Step 2: run them and see them FAIL.
- [ ] Step 3: implement.
- [ ] Step 4: run `go test ./...` and see it pass.
- [ ] Step 5: commit `feat(consumer): scan consumer files for token references`.

### Task 4: The five rules

**Files:**
- Modify `internal/lint`: one file per rule group, `rules_consumer.go`, and register the rules.
- `docs/rules/{reference-undeclared,primitive-read-by-consumer,token-owner-violation,fallback-literal,wrapper-unmapped}.md`.
- `internal/selftest/fixtures/flag/<id>/` for each rule. `fixtures/clean` gains consumers that are clean.

**Interfaces:** consumes `consumer.Scan`.

- [ ] Step 1: write the tests, one per rule, each covering a firing case and a silent case.
  - Also: `TestOwnerViaWrapper`, `TestEmptyScanFails`, `TestConsumerTallies` and `TestConsumerExemption` (the exemption appears in the gate JSON).
  - The self-test passes.
- [ ] Step 2: run them and see them FAIL.
- [ ] Step 3: implement.
- [ ] Step 4: run `go test ./...` and see it pass.
- [ ] Step 5: commit `feat(lint): consumer rules`.

### Task 5: End to end and docs

**Files:**
- `cmd/airframe/testdata/script/lint_consumers.txtar`. It runs:
  - `init`;
  - add consumers, a wrapper and a component;
  - a clean lint;
  - inject each violation, and check `lint` exits 1 naming it;
  - `--format gate` has checks for all five rules with scanned counts.
- `docs/guides/consumers.md`. It covers:
  - declaring `consumers`;
  - what each adapter matches;
  - app-local CSS properties;
  - wrappers, all three shapes, with Swift, Kotlin and C# examples;
  - components and owners;
  - exemption markers;
  - each rule, with links.
- `README.md`: consumer rules join the built list as "(next release)", and the Planned list keeps components/retired and the reference surface. Update `docs/docs_test.go` to match.
- The spec: the §6.4 Consumer row status (`reference-undeclared`, `token-owner-violation`, `primitive-read-by-consumer`, `fallback-literal` and `wrapper-unmapped` built; `retired-used` and `component-unruled` stay planned, for plan 5), the §6.3 Component note, and §6.7 item 4 marked done.

- [ ] Step 1: write the txtar and the guide test, which lints the guide's yaml and consumer snippets the way the 3c guide tests do. See them FAIL.
- [ ] Step 2: implement.
- [ ] Step 3: `go test ./... && go vet ./... && gofmt -l .` must be clean.
- [ ] Step 4: commit `feat: consumer rules end to end`.
