# Airframe 2 · CSS importer with an equivalence check — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `airframe import css <files…>` turns hand-written CSS custom properties into DTCG token files for an existing Airframe project, and proves the result: for every combination of root attributes and media features, every custom property resolves to the same value in the original CSS and in the CSS Airframe generates from the imported tokens.

**Architecture:** Four small packages and one command.
- `internal/css` parses a supported subset of CSS, matches selectors and media against an *environment*, and resolves custom properties through the cascade.
- `internal/cssvalue` turns a CSS value into a token value and compares two values semantically.
- `internal/importer` maps each CSS block back to a mode key by inverting emit's own scope expansion, writes token files, and runs the equivalence check.
- The project's `airframe.yaml` defines every axis and its web spelling. The importer never invents an axis.

**Model:** Starboard (https://github.com/c65llc/starboard) is the reference for conventions: command wiring and flags (`internal/cli`), testscripts (`cmd/starboard/testdata/script/*.txtar`), the adoption guide (`docs/guides/adopting.md`) and the CLI reference (`docs/reference/cli.md`). Where this plan leaves a convention open, do what Starboard does.

**Tech Stack:** Go 1.27 (`toolchain go1.27.4`), the module's pinned dependencies only (no CSS library). testify, testscript.

**Spec:** `docs/specs/2026-11-27-airframe-design.md` §6.7 item 2 (and §6.2 for the config the importer reads, §6.5 for the command).

## Global Constraints

- License header on every Go file: `// SPDX-License-Identifier: AGPL-3.0-only` then `// Copyright 2026 C65 LLC`.
- Deterministic output: LF, a final newline, no timestamps; never range a map without sorting its keys.
- Fail closed. Anything the importer cannot read is reported by name; it is never skipped silently. A check that compared nothing says so.
- Exit codes: 0 = clean, 1 = findings, 2 = usage or I/O error.
- Writes go through the CLI's existing `os.Root` helper (`writeFile`), never directly to joined paths.
- Diagnostics use `diag.Diagnostic` with `Docs: diag.DocsURL(id)`. Every new ID gets a `docs/rules/<id>.md` page.

## Decisions this plan makes

- **Prerequisite.** Import runs inside an existing project. `airframe.yaml` must exist and load; its axes, bases and web spellings are the mapping. A missing config is the existing `config-missing` (exit 1).
- **Supported CSS** (everything else is an error that names the construct and its line):
  - Top-level rules, and rules nested one level inside `@media`. Nested `@media` inside `@media` concatenates conditions.
  - Comments anywhere. Strings and parentheses are respected when splitting on `;`, `,` and `}`.
  - Selectors: compounds on `:root` only (`html` is accepted as a synonym), made of `[attr]`, `[attr='v']` / `[attr="v"]`, `:not([attr])` and `:not([attr='v'])`. A selector list is split on top-level commas, and each selector is handled on its own.
  - Media: conjunctions (`and`) of `(feature: value)` atoms, for example `(prefers-color-scheme: dark)`, `(pointer: coarse)`. `not`, `or`, `only` and media types are unsupported.
  - Other at-rules (`@layer`, `@supports`, `@import`, `@font-face`…) are unsupported.
  - Declarations that are not custom properties (`color: red`) are counted, reported once in the summary, and ignored. They are not tokens.
- **Environment.** An environment is a value for every attribute seen in either stylesheet (unset, or one of the values seen) plus a value for every media feature seen (unset, or one of the values seen). The equivalence check enumerates the full product. More than 200,000 environments is the error `import-too-many-environments`.
- **Specificity** follows CSS: `:root` counts (0,1,0), as does each attribute selector. `:not(x)` counts as x. Ties go to later source order. All rules target the root element.
- **Mapping a block to a mode key.** Enumerate every mode key over the config's loaded axes: each axis is either absent or one non-base value. Expand each key with the exported emit scope function (Task 6) and index the normalised scopes. Normalising means:
  - parse the media atoms and sort them;
  - parse the selector's attribute conditions and sort them;
  - treat `html` as `:root`.
  
  A source block whose normalised scope is not in the index is `import-unmapped-scope`. The message names the scope and says which `airframe.yaml` web spelling would produce it.
- **Values** (`internal/cssvalue`). Accepted forms:
  - **colour:** `oklch()` (with `/ alpha`), `#rgb`, `#rgba`, `#rrggbb`, `#rrggbbaa`, `rgb()`/`rgba()` (numbers or percentages, comma or space syntax), `hsl()`/`hsla()`, and `transparent`. Every colour is converted to OKLCH.
  - **dimension:** `px` and `rem` only.
  - **number.**
  - **duration:** `ms` and `s`.
  - **cubicBezier:** `cubic-bezier(a, b, c, d)`.
  - **fontFamily:** a comma list of identifiers or quoted strings, only when the property name ends in `-font`, `-family` or `font-family`. Otherwise an ident list is unsupported.
  - **fontWeight:** `100`–`900` and `normal` (400) / `bold` (700), only when the name contains `weight`.
  - **shadow:** one or more comma-separated layers.
  - **alias:** `var(--x)`. A fallback is dropped with the warning `import-fallback-dropped`.
  
  Anything else (`calc()`, `clamp()`, other units, keywords) is `import-unsupported-value`, and that declaration is not imported.
- **Value equality** (for the equivalence check):
  - Colours compare in OKLab: Euclidean distance ≤ 1e-4 and |Δalpha| ≤ 1e-4. Hue is ignored when both chromas are below 1e-4.
  - Numbers, dimensions and durations need the same unit and |Δ| ≤ 5e-5. `px` never equals `rem`.
  - Lists compare element by element.
  - Two values that both failed to resolve (a cycle, or a missing `var()` target) are equal; one resolved and one not is a mismatch.
- **Token names** are the custom-property name without `--`. A name that fails Airframe's token-name rule is `import-name-invalid`.
- **Types.** One token has one type across all its values. A conflict is `import-type-conflict`.
- **Tiers.** A token whose values are all literals is `tier: primitive`. A token with any `var()` value is `tier: semantic`. The importer never assigns `component`. Tiers are the author's decision afterwards; lint will say where they are wrong.
- **Base value.** The `:root` block (mode key `""`) supplies `$value`. A token set only under some mode key is `import-no-base`.
- **Duplicates.** The same token and mode key declared twice: the later one wins, with the warning `import-duplicate` naming both lines.
- **Descriptions.** A comment that immediately precedes a `:root` declaration, with only whitespace between them, becomes `$description`. It is trimmed, a leading `*` on each line is stripped, and whitespace runs collapse to one space.
- **Output.** `<content>/tokens/primitive.tokens.json` and `<content>/tokens/semantic.tokens.json`, flat keys in first-appearance order, each run through `format.File` so they are canonical. Mode values go in `$extensions["com.c65llc.airframe"].modes`. If either file exists, exit 2 unless `--force`. With any error-severity diagnostic, nothing is written (exit 1). `--dry-run` never writes.
- **New IDs** (all error severity unless marked): `import-unsupported-selector`, `import-unsupported-at-rule`, `import-unsupported-media`, `import-unmapped-scope`, `import-unsupported-value`, `import-name-invalid`, `import-type-conflict`, `import-no-base`, `import-duplicate` (warn), `import-fallback-dropped` (warn), `import-not-equivalent`, `import-too-many-environments`, `import-nothing`. `importer.IDs()` returns them, sorted. They are command findings, not lint rules: they are not in `lint.AllIDs()` and are not self-tested. The ID-scan test and the docs test learn about `importer.IDs()` (Task 9).

## Review Focus

1. **Class- or element-based theming** (`.dark :root`, `body`, `[data-theme=dark] .card`) must be reported as `import-unsupported-selector` with its line, not silently ignored. → Task 7, `TestImportReportsUnsupportedSelector`.
2. **A selector list mixing scopes** (`:root, :root[data-theme='dark'] { --x: 1 }`) must import the declaration under both keys. → Task 7, `TestImportSelectorListSplits`.
3. **Comments and strings with structural characters** (`/* a } b; */`, `--font: "A; B", sans-serif`) must not break parsing. → Task 2, `TestParseCommentsAndStrings`.
4. **A `var()` cycle or missing target in the source** must resolve to "invalid" on both sides and count as equal. A generated file that fixes it is a reported mismatch, not a crash. → Task 4, `TestResolveCycleIsInvalid`; Task 8, `TestCheckInvalidOnBothSidesIsEqual`.
5. **Colour spellings** (`#ABC`, `#aabbccdd`, `rgb(255 0 0 / 50%)`, `hsl(120deg 100% 25%)`) must convert correctly and compare equal to the `oklch()` Airframe emits. → Task 5, `TestParseColourSpellings`; Task 8, `TestCheckHexEqualsEmittedOKLCH`.

---

### Task 1: sRGB to OKLCH

**Files:** Modify `internal/color/color.go`; Test `internal/color/color_test.go`.

**Interfaces:** Produces `func FromSRGB(r, g, b, alpha float64) OKLCH` (gamma-encoded sRGB channels in 0–1). Hue is normalised to [0,360), and hue is 0 when chroma < 1e-9.

- [ ] **Step 1: Write failing tests:**
  - `TestFromSRGBRed`: `FromSRGB(1,0,0,1)` ≈ `OKLCH{0.62796, 0.25768, 29.2339, 1}` (±1e-4 each).
  - `TestFromSRGBWhiteAchromatic`: `FromSRGB(1,1,1,1)` has L ≈ 1, C < 1e-6, H == 0.
  - `TestFromSRGBRoundTrip`: for 20 fixed colours, `FromSRGB(c.ToSRGB(Clip))` ≈ c (OKLab distance ≤ 1e-6).
- [ ] **Step 2:** Run `go test ./internal/color/ -run FromSRGB`. Expected: FAIL (undefined).
- [ ] **Step 3:** Implement using the existing `rgbToOKLab` and the sRGB matrices, then convert to polar.
- [ ] **Step 4:** Run the tests. Expected: PASS.
- [ ] **Step 5:** Commit: `feat(color): convert sRGB to OKLCH`.

### Task 2: CSS parser (supported subset)

**Files:** Create `internal/css/parse.go`; Test `internal/css/parse_test.go`.

**Interfaces:** Produces:
- `type Sheet struct{ File string; Rules []Rule; Ignored int }`, where Ignored counts non-custom declarations;
- `type Rule struct{ Media []string; Selectors []string; Decls []Decl; Line int }`, where Media is the raw conditions, outermost first, and Selectors is the raw list split on top-level commas and trimmed;
- `type Decl struct{ Name, Value, Comment string; Line, Order int }`, where Name includes `--`, Value is trimmed, Comment is the immediately preceding comment (raw) or "", and Order is the global declaration index in the sheet;
- `func Parse(file string, src []byte) (*Sheet, []diag.Diagnostic)`, which reports `import-unsupported-at-rule` at the at-rule's line and continues past it.

- [ ] **Step 1: Write failing tests:**
  - `TestParseRootBlock`: `:root { --a: 1px; color: red }` gives 1 rule, 1 decl (`--a`, `1px`), and `Ignored == 1`.
  - `TestParseMediaNesting`: `@media (prefers-color-scheme: dark) { :root:not([data-theme='light']) { --a: 2px } }` gives `Media == ["(prefers-color-scheme: dark)"]`.
  - `TestParseNestedMediaConcatenates`: two nested `@media` blocks give both conditions in order.
  - `TestParseSelectorList`: `:root, :root[data-theme="dark"] {…}` gives 2 selectors.
  - `TestParseCommentsAndStrings` (Review Focus 3): `/* a } b; */ :root { /* the ink */ --font: "A; B", sans-serif; --x: var(--y, "}") }` gives the decls `--font` = `"A; B", sans-serif` with Comment containing `the ink`, and `--x` = `var(--y, "}")`.
  - `TestParseUnsupportedAtRule`: `@supports (x) { :root { --a: 1 } }` gives `import-unsupported-at-rule` at line 1, and no rules.
  - `TestParseLinesAndOrder`: Line and Order are correct across a 3-line sheet.
- [ ] **Step 2:** Run `go test ./internal/css/`. Expected: FAIL.
- [ ] **Step 3:** Implement a hand-written scanner: comments, strings (`'`/`"` with escapes), and paren depth. Track lines.
- [ ] **Step 4:** Run the tests. Expected: PASS.
- [ ] **Step 5:** Commit: `feat(css): parse the custom-property subset of CSS`.

### Task 3: Selectors, media and environments

**Files:** Create `internal/css/match.go`, `internal/css/env.go`; Test `internal/css/match_test.go`, `internal/css/env_test.go`.

**Interfaces:** Produces:
- `type Cond struct{ Attr, Value string; Present, Negated bool }`. `Present` with an empty Value means `[attr]`; `Negated` wraps it in `:not(…)`.
- `type Selector struct{ Conds []Cond; Spec int }`. Spec counts the conditions plus 1 for `:root`.
- `func ParseSelector(s string) (Selector, error)`: accepts `:root`/`html` followed by conditions, in any order; anything else is an error whose message names the selector.
- `type Atom struct{ Feature, Value string }`.
- `func ParseMedia(conds []string) ([]Atom, error)`: splits on ` and `; anything other than a parenthesised `feature: value` is an error.
- `func (s Selector) Key() string` and `func MediaKey(atoms []Atom) string`: sorted, canonical text used for indexing.
- `type Env struct{ Attrs map[string]string; Media map[string]string }`. A missing key means unset.
- `func (s Selector) Matches(e Env) bool` and `func MediaMatches(atoms []Atom, e Env) bool`.
- `func Envs(sheets ...*Sheet) ([]Env, error)`: the deterministic product; more than 200000 environments is an error.

- [ ] **Step 1: Write failing tests:**
  - `TestParseSelectorForms`: `:root`, `html`, `:root[data-theme='dark']`, `:root:not([data-theme="light"])[data-app='x']`, and `:root[data-density]` parse with the right Conds and Spec (1, 1, 2, 3, 2).
  - `TestParseSelectorRejects`: `.dark`, `body`, `:root .card`, `:root:hover` and `[data-x]` (no `:root`) are errors.
  - `TestMatches`: a negated condition matches when the attribute is unset or has a different value. `[attr]` matches any set value.
  - `TestParseMediaForms`: `(prefers-color-scheme: dark) and (pointer: coarse)` gives 2 atoms; `not screen`, `(min-width: 40em) or (x: y)` and `print` are errors.
  - `TestEnvsProduct`: sheets using `data-theme` ∈ {dark, light} and `prefers-color-scheme` ∈ {dark} give 3 × 2 = 6 environments, in deterministic order.
  - `TestEnvsTooMany`: a synthetic sheet past 200000 environments gives the error.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement as described in Interfaces.
- [ ] **Step 4:** Run the tests. Expected: PASS.
- [ ] **Step 5:** Commit: `feat(css): selector and media matching over environments`.

### Task 4: Cascade resolution

**Files:** Create `internal/css/cascade.go`; Test `internal/css/cascade_test.go`.

**Interfaces:** Consumes Tasks 2–3. Produces:
- `type Computed struct{ Value string; Invalid bool }`, where Value has every `var()` substituted;
- `func Resolve(sheets []*Sheet, e Env) (map[string]Computed, error)`.

Resolve takes every declaration whose rule's media and any selector in its list match, picks the winner by (specificity, Order across sheets in the given order), then substitutes `var(--x[, fallback])` recursively. A missing target uses the fallback, else Invalid. A cycle makes every member Invalid. An unsupported selector or media in a rule is an error, so Resolve never silently skips a rule.

- [ ] **Step 1: Write failing tests:**
  - `TestResolveSpecificityBeatsOrder`: `:root[data-theme='dark']{--a:2}` then `:root{--a:1}`, with data-theme=dark, gives 2.
  - `TestResolveOrderBreaksTies`: two `:root` blocks give the later value.
  - `TestResolveMediaGate`: a block under `(pointer: coarse)` applies only when Media pointer=coarse.
  - `TestResolveVarChain`: `--a: var(--b)`, `--b: 3px` gives `--a` = `3px`.
  - `TestResolveFallback`: `--a: var(--missing, 4px)` gives `4px`.
  - `TestResolveCycleIsInvalid` (Review Focus 4): `--a: var(--b); --b: var(--a)` makes both Invalid. `--c: var(--zz)` with no fallback is Invalid.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement as described in Interfaces.
- [ ] **Step 4:** Run the tests. Expected: PASS.
- [ ] **Step 5:** Commit: `feat(css): resolve custom properties through the cascade`.

### Task 5: CSS values to token values

**Files:** Create `internal/cssvalue/value.go`, `internal/cssvalue/equal.go`; Test `internal/cssvalue/value_test.go`, `internal/cssvalue/equal_test.go`.

**Interfaces:** Consumes `color.FromSRGB`, `tokens.Value`/`Dim`/`Duration`/`Shadow`. Produces:
- `func Parse(name, raw string) (typ string, v *tokens.Value, warn string, err error)`. typ is a DTCG `$type`. An alias sets `v.Alias` to the target name without `--`, with typ "", because the type comes from the target. warn is non-empty when a `var()` fallback was dropped.
- `func Equal(a, b Computed) bool`, comparing two resolved CSS values per the plan's equality rule (Computed is `css.Computed`).

- [ ] **Step 1: Write failing tests:**
  - `TestParseColourSpellings` (Review Focus 5): `#f00`, `#FF0000`, `#ff000080`, `rgb(255 0 0 / 50%)`, `rgba(255,0,0,.5)`, `hsl(0deg 100% 50%)`, `oklch(62.8% 0.2577 29.23)` and `transparent` all parse to colours. The red ones are ≈ `OKLCH{0.628, 0.2577, 29.23}`, with alpha where given.
  - `TestParseUnits`: `12px` and `1.25rem` are dimensions; `150ms` and `.2s` are durations; `cubic-bezier(.2, 0, 0, 1)` is cubicBezier; `0.5` is a number.
  - `TestParseFontByName`: `--body-font: "IBM Plex Sans", system-ui` is a fontFamily of 2. The same value under `--x` is unsupported. `--heading-weight: bold` is fontWeight 700.
  - `TestParseShadow`: `0 1px 2px 0 rgb(0 0 0 / 20%), inset 0 0 0 1px #000` gives 2 layers, the second inset.
  - `TestParseAlias`: `var(--accent)` gives Alias `accent`; `var(--a, 4px)` gives Alias `a` plus a warn.
  - `TestParseUnsupported`: `calc(1px + 2px)`, `1em`, `50%`, `auto` and `red` are errors naming the value.
  - `TestEqual`: `#ff0000` equals `oklch(62.7955% 0.2577 29.2339)`. `12px` does not equal `0.75rem`. Two invalids are equal; invalid against a value is unequal.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement as described in Interfaces.
- [ ] **Step 4:** Run the tests. Expected: PASS.
- [ ] **Step 5:** Commit: `feat(cssvalue): parse CSS values into token values and compare them`.

### Task 6: Export emit's scope expansion

**Files:** Modify `internal/emit/css.go`; Test `internal/emit/css_test.go`.

**Interfaces:** Produces `type Scope struct{ Media []string; Selector string }` and `func Scopes(cfg *config.Config, key config.ModeKey) ([]Scope, error)`, the existing `scopesFor`, exported. The internal `scope` type is replaced by `Scope`, with the CSS output unchanged (the goldens must not move).

- [ ] **Step 1: Write a failing test:** `TestScopesExported` asserts that `Scopes(spec §6.2 config, "appearance:dark")` returns the two dark scopes in emit order.
- [ ] **Step 2:** Run it. Expected: FAIL (undefined).
- [ ] **Step 3:** Rename and export; update callers.
- [ ] **Step 4:** Run `go test ./internal/emit/`. Expected: PASS with the goldens unchanged.
- [ ] **Step 5:** Commit: `refactor(emit): export scope expansion for the importer`.

### Task 7: Import (CSS to token files)

**Files:** Create `internal/importer/importer.go`, `internal/importer/index.go`, `internal/importer/ids.go`; Test `internal/importer/importer_test.go`; Create `testdata/import/` (a project: `airframe.yaml` with appearance (light bare-`:root`, dark media + attribute), density (attr, base standard, relaxed `also` coarse) and app (attr, base drafts); plus `theme.css` with about 25 custom properties across those scopes, hex, rgb and oklch colours, rem and px, a font stack, a shadow, aliases and comments).

**Interfaces:** Consumes Tasks 2, 3, 5 and 6, `config.Config`, `format.File`. Produces:
- `type Result struct{ Files map[string][]byte; Tokens, Declarations, Ignored int; Diagnostics []diag.Diagnostic }`, where Files maps repo-relative paths to canonical bytes;
- `func Import(cfg *config.Config, sheets []*css.Sheet) Result`;
- `func IDs() []string`, the 13 IDs from the plan, sorted.

The index (`index.go`) builds `map[scopeKey]config.ModeKey` over every key of the loaded axes (each axis absent or one non-base value; deferred axes skipped) via `emit.Scopes`. Here scopeKey = `css.MediaKey(atoms) + "|" + selector.Key()`. Deterministic.

- [ ] **Step 1: Write failing tests:**
  - `TestImportFixture`: `testdata/import` imports with no error diagnostics. The result has the expected token count. `accent-ink` is semantic, aliases `accent`, and has an `appearance:dark` mode value. `space-4` is a primitive `Dim{1,"rem"}`. The comment above `--ink` becomes its `$description`. Both output files are canonical (format.File is a no-op on them).
  - `TestImportSelectorListSplits` (Review Focus 2).
  - `TestImportReportsUnsupportedSelector` (Review Focus 1): `.dark { --a: 1 }` gives `import-unsupported-selector` at its line, and `Files` is empty.
  - `TestImportUnmappedScope`: `:root[data-theme='sepia']` with no such axis value gives `import-unmapped-scope`, and the message names the selector.
  - `TestImportNoBase`, `TestImportTypeConflict`, `TestImportNameInvalid` (`--Accent_Ink`), `TestImportDuplicateWarns` (a warning; the file is still produced), `TestImportFallbackDropped` (a warning).
  - `TestImportNothing`: a sheet with no custom properties gives `import-nothing` (error).
  - `TestImportDeterministic`: two runs produce byte-identical Files.
- [ ] **Step 2:** Run `go test ./internal/importer/`. Expected: FAIL.
- [ ] **Step 3:** Implement as described in Interfaces and Decisions.
- [ ] **Step 4:** Run the tests. Expected: PASS.
- [ ] **Step 5:** Commit: `feat(importer): map CSS blocks to mode keys and write token files`.

### Task 8: Equivalence check

**Files:** Create `internal/importer/check.go`; Test `internal/importer/check_test.go`.

**Interfaces:** Consumes `Import`'s Files, `project.MemFS` + `project.Load`, `emit.CSS`, `css.Parse`, `css.Envs`, `css.Resolve`, `cssvalue.Equal`. Produces:
- `type CheckResult struct{ Environments, Properties, Comparisons int; Diagnostics []diag.Diagnostic }`;
- `func Check(cfg *config.Config, configYAML []byte, files map[string][]byte, old []*css.Sheet) CheckResult`.

Check builds an in-memory project (the config plus the imported files), generates its CSS with the config's `css` platform, parses it, and enumerates `css.Envs(old…, generated)`. In every environment it compares every custom property that either side declares (the union; a property on one side only is a mismatch). It reports `import-not-equivalent` per property: the first mismatching environment as `attr=…, media=…`, with old and new values, then "+N more environments". Reports are capped at 20 properties plus a count. `Comparisons == 0` is `import-nothing`.

- [ ] **Step 1: Write failing tests:**
  - `TestCheckFixtureEquivalent`: the `testdata/import` fixture compares equal. Environments > 0, and Properties equals the fixture's property count.
  - `TestCheckHexEqualsEmittedOKLCH` (Review Focus 5): old `--a: #3366cc` is equal after the round trip.
  - `TestCheckDetectsCascadeDifference`: a hand-edited generated file (dark override dropped) gives `import-not-equivalent` naming the property and a `data-theme=dark` environment.
  - `TestCheckInvalidOnBothSidesIsEqual` (Review Focus 4).
  - `TestCheckAlsoFallback`: `@media (pointer: coarse) { :root:not([data-density]) { … } }` in the source round-trips through the `also` spelling and is equal.
- [ ] **Step 2:** Run the tests. Expected: FAIL.
- [ ] **Step 3:** Implement as described in Interfaces.
- [ ] **Step 4:** Run the tests. Expected: PASS.
- [ ] **Step 5:** Commit: `feat(importer): prove generated CSS equivalent in every environment`.

### Task 9: Command, IDs and docs

**Files:** Modify `internal/cli/cli.go` (or create `internal/cli/import.go` and register it there, to keep cli.go from growing), `internal/lint/ids_test.go`, `docs/docs_test.go`; Create `docs/rules/<id>.md` for every `importer.IDs()` entry; Test `cmd/airframe/testdata/script/import_*.txtar`; Modify `README.md` (move "import" from planned to done, and add a Quick start line) and `docs/specs/2026-11-27-airframe-design.md` (§6.5 drops "planned" from `import css`; §6.7 marks item 2 done).

**Interfaces:**
- Consumes `importer.Import`, `importer.Check`, `css.Parse`, the CLI's `writeFile` and root discovery.
- Also creates `docs/guides/importing.md`, modeled on Starboard's `docs/guides/adopting.md`: who it is for, prerequisites (an `airframe.yaml` whose web spellings match your CSS), a walkthrough on the `testdata/import` fixture, what each `import-*` finding means and how to fix it, and what to do after (assign tiers, run lint).
- Produces the command `airframe import css <files…> [--dry-run] [--force]`:
  1. load the config;
  2. parse every file (read through the os.Root);
  3. Import, then Check, but only when Import produced no error-severity diagnostic, since Resolve refuses unsupported rules;
  4. print diagnostics (text);
  5. print a summary, e.g. `imported 25 tokens from 31 declarations (4 non-custom ignored); equivalent in 576 environments across 25 properties`;
  6. write the files only when there is no error-severity diagnostic and not `--dry-run`, and refuse existing outputs unless `--force` (exit 2).
- The ID-scan test treats `importer.IDs()` as known IDs. The docs test expects pages for `lint.AllIDs() ∪ importer.IDs()`. Import pages have `severity` front matter matching the plan and `since: v0.2.0`.

- [ ] **Step 1: Write failing testscripts:**
  - `import_ok.txtar`: the `testdata/import` fixture without token files. `import css theme.css` exits 0 and prints the summary; `lint` then exits 0, after `build`; `build --check` after `build` exits 0.
  - `import_dry_run.txtar`: exits 0 and writes no token files.
  - `import_refuses_existing.txtar`: exits 2, and the existing file is unchanged; with `--force` it exits 0.
  - `import_errors.txtar`: an unsupported selector exits 1, writes nothing, and stdout names `import-unsupported-selector` with `theme.css:<line>`.
  - `import_no_config.txtar`: exits 1 with `config-missing`.
- [ ] **Step 2:** Run `go test ./cmd/airframe/`. Expected: FAIL.
- [ ] **Step 3:** Implement the command, the docs pages, the test updates, and the README and spec edits.
- [ ] **Step 4:** Run `go vet ./... && go test ./...` and `gofmt -l .` (must print nothing). Expected: PASS.
- [ ] **Step 5:** Commit: `feat(cli): airframe import css with an equivalence check`.
