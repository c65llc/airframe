# Airframe 6b · Reference commands: `rename`, `retire`, `specimen` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the three commands that write files.
- `airframe rename <old> <new>` is a codemod across token files, config, components, invariants and every consumer platform. The spec says "Renames ship with a codemod, never with compatibility aliases".
- `airframe retire` marks a token retired, or records a retired treatment.
- `airframe specimen` writes swatch, type and spacing sheets as SVG.

This finishes roadmap step 6.

**Architecture:**
- `internal/rename` builds an **edit plan** without writing anything: a list of `Edit{File, Line, Col, Old, New}` for text edits, plus structural edits to JSON and YAML documents.
  - It checks the plan by applying it in memory, then loading, building and linting the result.
  - The CLI then writes every file through os.Root, regenerates the outputs, and runs `fmt` on the source files it touched.
- Consumer edits are driven by the emitters' names. A new `emit.ReferenceVariants` maps each emitted name to `(token, variant)`, so an old name such as `DesignGapCompact` maps to the right new one.
- `internal/specimen` renders deterministic SVG.

**Tech Stack:** Go 1.27 (`toolchain go1.27.4`), pinned deps only.

**Spec:** `docs/specs/2026-11-27-airframe-design.md`:
- §2 anti-goal: renames ship with a codemod.
- §6.5: the rename, retire and specimen lines.
- §7: "The codemod engine for `rename`: textual, driven by the naming table, per platform".
- §6.7 item 6.

## Global Constraints

- License header on every Go file: `// SPDX-License-Identifier: AGPL-3.0-only`, then `// Copyright 2026 C65 LLC`.
- Output is deterministic.
- Writes go only through os.Root.
- All or nothing: a command either writes every edit or none. Each command computes everything first, then writes.
- `--dry-run` prints the plan and writes nothing.
- Exit codes:
  - 0: done, or the dry-run plan was printed;
  - 1: refused, because a precondition failed or the result would not load or build;
  - 2: usage or I/O error.
- Public repo: no private-project names, paths or emails.

## Decisions this plan makes

### `emit.ReferenceVariants` (Task 1)

`func ReferenceVariants(p *project.Project, pl config.Platform, format string) (map[string]Variant, error)`

`Variant` is `{Token string; Key string}`. `Key` is a stable descriptor of which emitted name this is:

| Key | Meaning |
|---|---|
| `""` | the token's base name |
| `density=compact` | runtime suffix, with axes in axis order and `+` joining them |
| `layer1` | shadow layer colour |
| `layer1.offsetX` | shadow layer part |
| `slot:density=compact` | Kotlin colour slot |
| `values:light` | not a token: the ColorValues val |

- It is built from the same code paths as `ReferenceNames`, and `ReferenceNames` becomes a projection of it. The existing tests must pass unchanged.
- `func NameFor(variants map[string]Variant) map[Variant]string` inverts it.

### `rename` (Tasks 1–2)

**Command:** `airframe rename <old> <new> [--path a.b.c] [--dry-run] [--allow-manual]`

**Preconditions.** Each failure exits 1 with a message:
- `old` exists.
- `new` doesn't exist, as a token or as a group path.
- `new` is a valid token name.
- The project loads without errors.

**The new DTCG path:**
- With `--path`, the dotted path is used as given.
- Otherwise, if the old token's path is `split(old, "-")`, the new path is `split(new, "-")`.
- Otherwise `--path` is required. That covers paths whose segments contain `-`.

**Edits, all computed in memory:**
1. **Token files.** Move the token's node from the old path to the new path in its base file and in every mode file that overrides it. Use `ojson`, so order is kept. Create intermediate groups, and delete groups left empty. Rewrite every alias `{old.path}` to `{new.path}` in every token file, in values, in mode values, and inside composite values such as shadow colours.
2. **Config (`airframe.yaml`).** Rewrite these token references:
   - `options.accentColor`;
   - `material3Scheme` role values;
   - `highContrast` keys;
   - wrapper `map` values.

   Pattern templates aren't edited. If a pattern used to resolve a member to `old`, that member becomes a **manual item**.
3. **Components.** Update each `tokens` list entry.
4. **Invariants.** Update exact token names in `tokens`, `fg`, `bg` and `only`. A glob such as `match: "*-target*"` or `only: ["danger-*"]` that matched `old` but doesn't match `new`, or the reverse, becomes a **manual item**.
5. **Consumer files.** For each `consumer.Scan` reference to `old`:
   - A `direct` reference, or a `generated: true` wrapper reference: its `Raw` text at line and col is replaced by the new spelling of the same `Variant`, from the renamed project's `ReferenceVariants`.
   - A `map` wrapper reference: no code edit, because the config map edit covers it.
   - A `pattern` wrapper reference: a **manual item**.
   - A retired spelling isn't a token reference, so it isn't touched.
   - Edits are exact byte splices at the recorded columns. Before it splices, the scanner checks that the bytes at that position still equal `Raw`.
6. **Generated outputs.** Regenerate them with `build`, after the source edits are applied.
7. **Formatting.** Run `fmt` on every touched source file: token, config, component and invariants files. Consumer files are never reformatted.

**Validation.** The renamed project is applied in an overlay FS, then loaded and built. If it fails to load or build, the command exits 1, prints the diagnostics, and writes nothing.

**Manual items.** If there are any manual items and `--allow-manual` isn't given, the command prints the plan and the manual items, then exits 1 without writing. With `--allow-manual`, it writes everything else and lists the manual items. The exit status is still 0, because the manual items are the user's to finish.

**`--dry-run` output:**
- one line per edit, `file:line:col  old → new`;
- then the structural moves;
- then the manual items.

### `retire` (Task 3)

**Retiring a token:** `airframe retire <token> [--dry-run]`
- Sets `retired: true` in the token's base declaration `$extensions["com.c65llc.airframe"]`, creating the object if it doesn't exist.
- If the token is already retired, exit 1.
- Then print the consequences: the reads that `retired-used` will now report (the `usages` list) and the direct aliases.
- It does not edit consumers. Retiring is what makes lint find them.

**Retiring a treatment:** `airframe retire --treatment <id> --spelling <adapter>=<RE2> … [--ruling @ruling:x] [--marker m] [--dry-run]`
- Appends an entry to `<content>/retired.yaml`, creating the file if needed, then runs fmt on it.
- It is validated by the retired loader.
- A duplicate id exits 1.
- `--spelling` can be repeated.
- After writing, it prints the matches the new spellings find.

### `specimen` (Task 4)

**Command:** `airframe specimen [--out dir]`. The default output directory is `<content>/generated/specimen/`. It is not part of `build` or `build --check`, and the files are overwritten each run.

**Sheets.** One set per fan-out value of any build axis used by a platform with `fanOut`, or one set if there's none. Names are `colors[-<value>].svg`, `type[-<value>].svg` and `spacing[-<value>].svg`.

| Sheet | Contents |
|---|---|
| `colors` | One row per colour token, grouped by tier (semantic, component, primitive). Within a group, rows follow the Set order. Each row has: the name, then one swatch column per appearance value (in axis order, or a single column), then the sRGB hex and OKLCH under each swatch. Swatches use the sRGB value mapped by CSS Color 4. Retired tokens are marked "retired". |
| `type` | One row per fontFamily or fontWeight token, and per dimension token whose name contains `font-size` or `line-height`. A sample line, "The quick brown fox jumps over the lazy dog", is set in that family, weight and size. Fonts fall back to the system; nothing is embedded. |
| `spacing` | Dimension tokens other than type, each as a horizontal bar of its px value (rem × 16), with its name and value. Bars are capped at 480px drawn, and the label shows the real value. |

**SVG output:**
- Escaped text and fixed geometry. No timestamps.
- A `<title>` naming the project content root.
- It uses the light appearance colours for text, so a sheet reads on white.
- The output is byte-stable, which a determinism test checks.

## Review Focus

1. **Renaming a token whose emitted names have suffixes or slots.** For example, renaming `gap` to `spacing` when it varies on density. `DesignGapCompact` must become `DesignSpacingCompact` in XAML; in Kotlin, `DesignColorValues.light.surfaceCompact` must follow `surface`. → Task 2, `TestRenameVariants`.
2. **A consumer file edited since the scan.** If the bytes at a ref's position no longer equal `Raw`, refuse with exit 1. Never splice at the wrong place. → Task 2, `TestRenameStaleSplice`.
3. **Renaming onto an existing group path.** For example, renaming `ink` to `text` when `text-muted` exists, which makes `text` a group. Refuse this. → Task 1, `TestRenameCollision`.
4. **An invariant glob that changes what it matches.** This must be a manual item, never silently left. → Task 1, `TestRenameInvariantGlob`.
5. **`specimen` with a third appearance, no appearance axis, a fan-out, and an out-of-gamut colour.** It must give stable sheets with clamped hex. → Task 4, `TestSpecimenShapes`.

---

### Task 1: ReferenceVariants and the rename planner

**Files:**
- Create `internal/emit/variants.go`, and refactor `refnames.go` onto it.
- Create `internal/rename/plan.go` for the plan types and planner, and `overlay.go` for the in-memory FS.
- Tests.

**Interfaces:**
- `rename.Plan(p *project.Project, old, new, path string) (*Plan, error)`.
- `Plan` holds `Edits []Edit`, `Moves []Move`, `Manual []Manual` and `Files map[string][]byte`. `Files` is the final bytes of every source file that changes. Regenerated outputs aren't in it.

- [ ] Step 1: Write the tests.
  - `TestReferenceVariants`, which includes round-tripping `NameFor`.
  - `TestReferenceNamesUnchanged`: every existing refnames test still passes.
  - `TestPlanTokenMove`, covering the base file and mode files.
  - `TestPlanAliases`.
  - `TestPlanConfigRefs`.
  - `TestPlanComponents`.
  - `TestRenameInvariantGlob`.
  - `TestRenameCollision`.
  - `TestPlanNeedsPath`.
- [ ] Step 2: Run them and see them FAIL.
- [ ] Step 3: Implement.
- [ ] Step 4: Run the suite and see it pass. The goldens must be unchanged.
- [ ] Step 5: Commit `feat(rename): plan renames across sources and config`.

### Task 2: Consumer edits and the `rename` command

**Files:**
- Modify `internal/rename/` to add the consumer edits and validation.
- Create `internal/cli/rename.go`.
- Tests, plus `cmd/airframe/testdata/script/rename.txtar`.

- [ ] Step 1: Write the tests.
  - `TestRenameVariants`: on testdata/consumers, run `gap→spacing` and `surface→canvas`. Check every platform's references.
  - `TestRenameWrappers`:
    - a map wrapper edits only the config;
    - a generated wrapper edits the member name;
    - a pattern wrapper becomes a manual item.
  - `TestRenameStaleSplice`.
  - `TestRenameDryRun`: the output is golden and nothing is written (hash the tree).
  - `TestRenameAllOrNothing`: inject a write failure with a read-only file, and check that nothing changed.
  - `TestRenameValidation`: a rename that would break the build is refused.
  - `TestRenameThenLintClean`: after the rename, `lint` and `build --check` are clean, and there is no reference left to the old name.
  - The txtar.
- [ ] Step 2: Run them and see them FAIL.
- [ ] Step 3: Implement.
- [ ] Step 4: Run the suite and see it pass.
- [ ] Step 5: Commit `feat: airframe rename codemod`.

### Task 3: `retire`

**Files:** Create `internal/cli/retire.go`, plus tests and `retire.txtar`.

- [ ] Step 1: Write the tests.
  - `TestRetireToken`: the extensions get created, and the consequences are printed.
  - `TestRetireAlready`.
  - `TestRetireTreatment`: create `retired.yaml`, append to it, and handle a duplicate id, a bad regex and an unknown adapter.
  - `TestRetireDryRun`.
  - `TestRetireThenLint`: lint now reports `retired-used` at the listed reads.
- [ ] Step 2: Run them and see them FAIL.
- [ ] Step 3: Implement.
- [ ] Step 4: Run the suite and see it pass.
- [ ] Step 5: Commit `feat: airframe retire`.

### Task 4: `specimen`

**Files:** Create `internal/specimen/` and `internal/cli/specimen.go`, plus tests with SVG goldens.

- [ ] Step 1: Write the tests.
  - `TestSpecimenGolden`: the apple fixture with its fan-out.
  - `TestSpecimenShapes`.
  - `TestSpecimenDeterministic`.
  - `TestSpecimenEscaping`: names and descriptions containing `<&>`.
  - `TestSpecimenWellFormed`: parse every file with `encoding/xml`.
- [ ] Step 2: Run them and see them FAIL.
- [ ] Step 3: Implement.
- [ ] Step 4: Run the suite and see it pass.
- [ ] Step 5: Commit `feat: airframe specimen`.

### Task 5: End to end and docs

**Files:**
- `docs/guides/reference.md`: add rename, retire and specimen. A guide test asserts the quoted outputs, including a dry-run plan.
- `README.md` and `docs_test`: the three commands go in the built list as "(next release)". Planned keeps skills, MCP and the surfaces.
- Spec:
  - the status line;
  - the §6.5 lines;
  - §6.7 item 6 marked done;
  - the §2 anti-goal, linked to `rename`.

- [ ] Step 1: Write the txtars and the guide test, and see them FAIL.
- [ ] Step 2: Write the docs.
- [ ] Step 3: `go test ./... && go vet ./... && gofmt -l .` is clean.
- [ ] Step 4: Commit `feat: rename, retire and specimen end to end`.
