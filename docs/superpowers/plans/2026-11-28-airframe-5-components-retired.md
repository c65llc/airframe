# Airframe 5 · Components and retired treatments — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** this plan completes the component inventory and retires things for good.
- Every component answers to a Gantry ruling (`component-unruled`), and its inventory is complete and true (`component-incomplete`).
- A retired token, or a retired treatment's spelling, can't come back into consumer code (`retired-used`).
- `airframe show <component>` prints a component's inventory, including who reads its tokens.

**Architecture:**
- **Retired treatments:** a new `retired.yaml` loader for the content root, built like the component loader.
- **Ruling resolver:** a small `internal/rulingref` package resolves `@ruling:<id>` against a Gantry project's `rulings/<id>.md` front matter. It reads files only, and Gantry is not a dependency.
- **Rules:** three lint rules, built on the plan 4 consumer scan.
- **`show`:** gains a component mode.

**Tech Stack:** Go 1.27 (`toolchain go1.27.4`), pinned deps only.

**Spec:** `docs/specs/2026-11-27-airframe-design.md`:
- §3: "A component with no ruling is not in the system", resolved against Gantry when a `gantry.yaml` is present.
- §6.3: the Component and Retired records.
- §6.4: the Consumer row.
- §6.7 item 5.

Gantry's record format is in its spec: rulings are `<content>/rulings/<id>.md`, with YAML front matter `id`, `status` (`proposed | ruled | superseded | retired`) and `supersededBy`.

## Global Constraints

- License header on every Go file: `// SPDX-License-Identifier: AGPL-3.0-only`, then `// Copyright 2026 C65 LLC`.
- Output is deterministic and lint fails closed, as in plan 4: every rule reports what it scanned, and an exemption needs a reason.
- Each new lint ID gets:
  - a registration with its default severity;
  - `docs/rules/<id>.md` with `since: v0.7.0`;
  - a flag fixture.

  The clean-consumers self-test fixture must stay clean, and gains a ruled, complete component.
- Exit codes are 0/1/2. Lint never writes.
- The repo is public: no private-project names, paths or emails.

## Decisions this plan makes

### Retired treatments (Task 1)

**`<content>/retired.yaml`** is optional. It is a list of entries, loaded into `project.Project.Retired []retired.Treatment`.
- `id`: required, kebab-case, unique.
- `ruling`: optional. A `@ruling:<id>` string; it is resolved like a component's when Gantry is present.
- `spellings`: required. A map from adapter name (`css`, `swift`, `kotlin`, `xaml`, `cs`) to a non-empty list of RE2 patterns, each of which must compile. These adapter names are the plan 4 adapters, and `cs` is the C# adapter.
- `marker`: optional. An identifier-ish escape word, `[a-z][a-z0-9-]*`.
- Anything else is schema-invalid.

**Tooling:**
- `airframe schema retired` serves its JSON Schema.
- `airframe fmt` and `not-formatted` cover the file.

**Retired tokens** (`retired: true`, which already parses) are still emitted, so code keeps compiling while it migrates. Reading one is what gets flagged.

### Ruling resolution (Task 2)

`package rulingref` provides `func Resolve(fsys fs.FS, cfg *config.Config) (*Index, []diag.Diagnostic)`, where `Index` has the method `Lookup(id string) (status, supersededBy string, ok bool)`.

**Which Gantry project:**
- The optional top-level key `gantry:` in `airframe.yaml` is a repo-relative path to a `gantry.yaml`.
- When the key is absent, a `gantry.yaml` next to `airframe.yaml` is used if one exists.
- If the key is set and the file is missing, that is schema-invalid.

**Reading rulings:**
- The Gantry content root comes from `gantry.yaml`'s `content`.
- Each `rulings/*.md` file's front matter is read: the text between the leading `---` lines, parsed as YAML.
- A ruling that doesn't parse becomes a diagnostic of the rule that needs it, not a crash.

**With no Gantry project,** `Index` is nil and resolution is "syntax only".

### Rules (Task 3)

All three rules are errors by default.

**`component-unruled`**, one finding per component. It fires when the component has:
- no `ruling`, or one that isn't `@ruling:<id>` (id `[a-z0-9][a-z0-9-]*`);
- with Gantry present, a ruling that doesn't exist;
- with Gantry present, a ruling whose status isn't `ruled`. The message names the status, and for `superseded` it names the `supersededBy`.

It also checks the `ruling` field of `retired.yaml` entries in the same way. Items count the components and retired entries checked. The rule is NotApplicable when the project has no components and no retired entries.

**`component-incomplete`**, a new ID added to the spec's Consumer row. It fires when:
- an `implementations` key isn't a configured platform;
- a platform that declares `consumers` has no `implementations` entry;
- a value is neither an existing repo-relative file path nor `n/a: <non-empty reason>`;
- `hostedBy` names an unknown component, or a `hostedBy` chain cycles (the cycle is reported once, naming every member).

**`retired-used`** fires on:
- a resolved consumer reference, direct or through a wrapper, to a token with `retired: true`;
- a non-retired token whose alias chain reaches a retired token. This is a source finding at the token.
- a `retired.yaml` spelling match in a consumer file whose adapter matches the spelling's key. Patterns are matched per line, and the finding is at the match's line and column, naming the treatment id and its ruling.

**Escaping a spelling match:**
- A `<marker>: <reason>` in the matched line, or on the comment-only line above it (plan 4's marker placement rules), suppresses the match. It is recorded as an exemption with `Marker: <marker>`, `Rule: retired-used`.
- The generic `airframe-exempt retired-used: <reason>` also works.
- A marker with no reason suppresses nothing.

### `airframe show <component>` (Task 4)

**Name lookup:**
- `show <name>` resolves a token first, then a component.
- If both exist, it shows the token and prints a hint, `(also a component: airframe show --component <name>)`.
- `--component` forces component mode.

**Text output.** In this order:
1. id, noun, ruling with its resolved status (or "not checked (no gantry.yaml)"), shared, hostedBy, and the components it hosts;
2. owned tokens with their tiers;
3. owners;
4. implementations per platform;
5. consumers: the files that read its tokens, per platform, with their reference counts, from `consumer.Scan`.

**JSON output** (`--json`) carries the same fields, with stable keys. Exit 1 if neither a token nor a component of that name exists.

## Review Focus

1. **Gantry resolution:**
   - A ruling file with CRLF line endings, a BOM, or no closing `---` must give a clean finding naming the file, not a crash.
   - A `gantry.yaml` whose `content` escapes the repo must be schema-invalid.
   - → Task 2, `TestResolveMalformed`.
2. **A superseded ruling** must be reported with its replacement named. A `proposed` ruling must be reported as not yet ruled. → Task 3, `TestComponentUnruledStatus`.
3. **A retired spelling that also matches inside a comment in a stylesheet** must not fire, because plan 4 blanks stylesheet comments. A match in a `.tsx` string must fire. → Task 3, `TestRetiredSpellingComments`.
4. **A `hostedBy` cycle** (`a → b → a`) must be reported once, not loop forever. → Task 3, `TestHostedByCycle`.
5. **`show` on a name that is both a token and a component** must be unambiguous. → Task 4, `TestShowAmbiguous`.

---

### Task 1: `retired.yaml` loader

**Files:**
- Create `internal/retired/retired.go`.
- Modify `internal/project/project.go`, `internal/format`, `internal/schema` (`retired.schema.json`) and the CLI `schema` kinds.
- Tests.

**Interfaces:** produces `retired.Treatment{ID, Ruling, Marker, File string; Line int; Spellings map[string][]*regexp.Regexp}` and `project.Project.Retired`.

- [ ] Step 1: write the tests.
  - `TestRetiredLoad`: valid; a duplicate id; a bad id; an unknown adapter key; an empty list; a bad regex; a bad marker; an unknown field.
  - `TestSpecRetiredExampleLoads`.
  - The format round-trip.
  - `airframe schema retired`.
- [ ] Step 2: run them and see them FAIL.
- [ ] Step 3: implement.
- [ ] Step 4: run the full suite and see it pass.
- [ ] Step 5: commit `feat: retired.yaml loader`.

### Task 2: Ruling resolver

**Files:**
- Create `internal/rulingref/`.
- Modify `internal/config/config.go` (the `gantry:` key) and the schema.
- Tests with a tiny Gantry fixture under `testdata/gantry-project/`.

**Interfaces:** produces `rulingref.Resolve` and `Index.Lookup`. It is memoised in the lint `Context`.

- [ ] Step 1: write the tests.
  - `TestResolveDefaultLocation`.
  - `TestResolveConfiguredPath`.
  - `TestResolveMissingConfigured`.
  - `TestResolveMalformed`: CRLF, a BOM, no closing fence, invalid YAML, an id mismatching the filename, and content outside the repo.
  - `TestResolveNoGantry`.
- [ ] Step 2: run them and see them FAIL.
- [ ] Step 3: implement.
- [ ] Step 4: run the suite and see it pass.
- [ ] Step 5: commit `feat: resolve @ruling references against a Gantry project`.

### Task 3: The three rules

**Files:**
- Create `internal/lint/rules_components.go`.
- Add `docs/rules/{component-unruled,component-incomplete,retired-used}.md`.
- Add flag fixtures.
- Update the clean-consumers fixture: its component gets a ruling, with no gantry.yaml, so the check is syntax only; it also gets complete implementations and one `n/a: …`.
- Update the spec §6.4 row, to add `component-incomplete`.

**Interfaces:** consumes `consumer.Scan`, `rulingref`, `project.Components`, `project.Retired` and the token set.

- [ ] Step 1: write the tests.
  - `TestComponentUnruled` and `TestComponentUnruledStatus`: missing, malformed, unknown with Gantry, proposed, superseded and retired.
  - `TestComponentIncomplete`: an unknown platform, a missing platform, a missing file, `n/a:` with no reason, an unknown `hostedBy`.
  - `TestHostedByCycle`.
  - `TestRetiredTokenRead`: direct and via a wrapper.
  - `TestRetiredAlias`.
  - `TestRetiredSpelling`, `TestRetiredSpellingComments` and `TestRetiredMarker`: a reason suppresses and is recorded with its marker; no reason does not suppress.
  - The self-test passes on both clean fixtures.
- [ ] Step 2: run them and see them FAIL.
- [ ] Step 3: implement.
- [ ] Step 4: run the suite and see it pass.
- [ ] Step 5: commit `feat(lint): component and retired rules`.

### Task 4: `airframe show` for components

**Files:** modify the `internal/cli` show command, `cmd/airframe/testdata/script/show_component.txtar`, and the tests.

- [ ] Step 1: write the tests.
  - `TestShowComponentText` and `TestShowComponentJSON` (golden).
  - `TestShowAmbiguous`.
  - `TestShowUnknown`.
  - The txtar.
- [ ] Step 2: run them and see them FAIL.
- [ ] Step 3: implement.
- [ ] Step 4: run the suite and see it pass.
- [ ] Step 5: commit `feat(cli): show a component's inventory`.

### Task 5: End to end and docs

**Files:**
- `cmd/airframe/testdata/script/lint_components.txtar`. It covers:
  - a project with a Gantry fixture;
  - a clean lint;
  - each violation in turn;
  - a marker exemption shown in the gate.
- `docs/guides/components.md`. It covers:
  - component files;
  - rulings and Gantry resolution;
  - implementations and `n/a`;
  - `hostedBy`;
  - `show`;
  - `retired.yaml` and retired tokens;
  - markers;
  - the three rules.

  Its quoted findings are asserted by a guide test, the way plan 4's guide test does it.
- `README.md` and `docs/docs_test.go`: components and retired treatments join the built list as "(next release)". Planned keeps the reference commands and surfaces.
- The spec:
  - the status line;
  - the §6.1 layout, dropping "(planned)" for `components/` and `retired.yaml`;
  - the §6.3 Component and Retired notes;
  - the §6.4 row;
  - §6.7 item 5, marked done;
  - §6.5 `show`, which accepts a component.

- [ ] Step 1: write the txtar and the guide test, and see them FAIL.
- [ ] Step 2: write the docs.
- [ ] Step 3: run `go test ./... && go vet ./... && gofmt -l .`; it must be clean.
- [ ] Step 4: commit `feat: components and retired treatments end to end`.
