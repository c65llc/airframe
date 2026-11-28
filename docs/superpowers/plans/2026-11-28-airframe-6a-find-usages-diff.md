# Airframe 6a · Reference commands: `find`, `usages`, `diff` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three read-only reference commands:
- `airframe find <role words>` answers "which token do I use here?".
- `airframe usages <token>` lists every place a token is read.
- `airframe diff <ref-a> [<ref-b>]` shows token changes between two git refs, as text, Markdown for a PR job summary, or JSON.

Plan 6b adds the writing commands: `rename`, `retire` and `specimen`.

**Architecture:** The commands are built on the project loader, the token set, the consumer scan from plan 4 and the components from plan 5. Two new packages:
- `internal/find`: ranks tokens.
- `internal/tokendiff`: compares two loaded projects.

`diff` loads a project at a git ref through a read-only `fs.FS` backed by `git show`/`git ls-tree`, using the `git` binary. Nothing is checked out.

**Tech Stack:** Go 1.27 (`toolchain go1.27.4`), pinned deps only. `git` is an external binary, required only by `diff`.

**Spec:** `docs/specs/2026-11-27-airframe-design.md`:
- §6.5: the `find`, `usages` and `diff` lines.
- §6.6 CI: `diff --format md` written to the job summary.
- §7: "`find`'s ranking: token descriptions plus role-name matching are enough; no embeddings required".
- §6.7 item 6.

## Global Constraints

- License header on every Go file: `// SPDX-License-Identifier: AGPL-3.0-only`, then `// Copyright 2026 C65 LLC`.
- Output is deterministic, with a total order on every list.
- Exit codes: 0 means the result is printed; 1 means no result (no match, unknown token); 2 means a usage or I/O error, git missing, or a bad ref.
- These commands never write, and they never run git commands that change state.
- `--json` output has stable keys and sorted lists. A JSON Schema for each output is served by `airframe schema find|usages|diff`, so the later MCP server can rely on it.
- Public repo: no private-project names, paths or emails.

## Decisions this plan makes

### `find` (Task 1)

`func find.Rank(p *project.Project, query string, opts find.Options) []find.Hit`

**Options:**
- `Limit` defaults to 10.
- `Type` filters by DTCG type: `color` and so on.
- `File`, given as `--file <path>`, keeps only tokens that file may read under the consumer rules' logic: tier readability, plus owners for component tokens. Each kept hit carries a `Why` string ("semantic: consumers may read" / "component button: owners include web/button/**").

**Ranking:**
- Normalise the query to lowercase words. Split on non-letters and drop the stopwords `a an the for of on in to with`.
- Score each word against the token's text, adding up a weight per match:

  | Word matches | Weight |
  |---|---|
  | a whole name segment (`muted` in `text-muted`) | 3 |
  | the prefix of a name segment | 2 |
  | a word of `$description` | 2 |
  | the start of a description word | 1 |
  | the token's component noun | 1 |

- A small built-in synonym table maps role words to segment words: `muted↔subtle↔secondary`, `bg↔background↔surface`, `fg↔text↔ink↔foreground`, `border↔stroke↔outline↔divider`, `accent↔brand↔primary`, `danger↔error↔destructive`, `gap↔space↔spacing`, `radius↔corner↔rounding`. A synonym match scores one less than a direct match.
- Ties sort by tier (semantic, component, primitive), then by name.
- Tokens scoring 0 are dropped.
- Retired tokens are excluded unless `--retired` is given.

**Output:**
- Text: `name  tier  type  value(base)  — description`, one hit per line.
- `--json`: `[{name, tier, type, value, description, score, why?}]`.
- Exit 1 when there are no hits.

### `usages` (Task 2)

`airframe usages <token> [--json] [--platform p]`

**What it reports:**
- Consumer references: `consumer.Scan` Refs whose Token is this token, direct or through a wrapper. Each is shown as `platform  file:line:col  raw  (via wrapper:<root>)`.
- Token references: tokens whose value or mode values alias it directly. These are listed separately as "aliased by".
- The component that owns it, if any, and whether each read is from an owner.

**Exit status:**
- An unknown token exits 1 with the same "did you mean" behaviour as `show`.
- A known token with zero usages exits 0 and prints "no usages". That is a result, not an error.
- A scan I/O error exits 2.

### `diff` (Task 3)

`airframe diff <ref-a> [<ref-b>] [--format text|md|json]`

**Refs:**
- With one ref, the comparison is ref-a against the working tree.
- Refs resolve with `git rev-parse --verify <ref>^{commit}`. A failure exits 2 naming the ref.

**Reading a ref:**
- `gitfs.New(repoRoot, commit) fs.FS` reads through `git ls-tree -r -z --full-tree <commit>` and `git cat-file --batch`.
- It serves only paths under the directory holding `airframe.yaml`, mapped relative to the project root, which must be inside a git work tree.
- The project root is found with `git rev-parse --show-toplevel` plus the relative prefix.
- A project that fails to load at a ref is reported with that ref's load diagnostics, and exits 1.

**Comparison:** `tokendiff.Compare(a, b *project.Project) Diff` takes the token sets. `Diff` holds these lists, each sorted by name:
- `Added`;
- `Removed`;
- `Changed`, where each entry is `{Name, Changes []Change}`. A `Change` has a Kind:
  - `value`: per mode key, including the base, compared by resolved value. A colour also carries its sRGB hex and OKLCH for both sides.
  - `tier`, `type`, `retired`, `component` or `description`.
- `Renamed`: a heuristic only. A removed and an added token with identical values in every context and the same type are paired, and reported as "possibly renamed". Removed and Added still list them.

**Formats:**
- `text`: grouped by kind.
- `md`: a heading per group, and a table per changed token with columns `context | before | after`. Colours show `■ #rrggbb oklch(…)` in the cell. GitHub strips data-URI images and inline styles, so the "swatch" is the hex plus a `■` glyph. The SVG swatch sheet is `specimen`'s job, in plan 6b.
- `json`: stable keys.

The exit status is 0 whether or not anything changed; `--exit-code` makes a non-empty diff exit 1, as `git diff --exit-code` does.

**Git:**
- No git binary, not a work tree, or a project outside the work tree each exit 2, with a clear message.
- Git runs with `GIT_TERMINAL_PROMPT=0` and no pager.
- The `--` separator is placed so a ref is never read as an option, and a ref starting with `-` is rejected.

## Review Focus

1. **`diff` with a ref that names a branch with a slash, a tag, a short SHA, or `HEAD~1`** must work. A ref like `--output=/tmp/x` must be rejected, never passed to git. → Task 3, `TestDiffRefInjection`.
2. **`diff` when the project's content root moved between refs** (because `airframe.yaml`'s `content` changed) must load each side with its own config. → Task 3, `TestDiffContentMoved`.
3. **`find "muted text"`** on a typical token set must rank `text-muted` and similar semantic tokens above the primitive `gray-500`. → Task 1, `TestFindRanking`, a golden on the clean-consumers fixture.
4. **`find --file web/x.tsx`** must never offer a primitive, or a component token the file doesn't own. → Task 1, `TestFindFileFilter`.
5. **`usages` of a token read only through a wrapper or a Kotlin colour slot** must still list the read. → Task 2, `TestUsagesViaWrapper`.

---

### Task 1: `find`

**Files:** Create `internal/find/` (`rank.go`, `synonyms.go`) and `internal/cli/find.go`. Add `internal/schema/find.schema.json`. Add tests and goldens.

- [ ] Step 1: Write the tests:
  - `TestFindRanking`: a golden on testdata/consumers or the clean-consumers tokens, covering queries like "muted text", "card background", "accent ink", "gap", "danger".
  - `TestFindFileFilter`.
  - `TestFindTypeFilter`.
  - `TestFindRetiredExcluded`.
  - `TestFindNoHits`, which expects exit 1.
  - `TestFindJSONSchema`, which checks the JSON validates against the served schema.
- [ ] Step 2: Run them and see them FAIL.
- [ ] Step 3: Implement.
- [ ] Step 4: Run the full suite and see it pass.
- [ ] Step 5: Commit `feat: airframe find`.

### Task 2: `usages`

**Files:** Create `internal/cli/usages.go`. Add `internal/schema/usages.schema.json`. Add tests and a txtar.

- [ ] Step 1: Write the tests:
  - `TestUsagesText` and `TestUsagesJSON`, as goldens.
  - `TestUsagesViaWrapper`.
  - `TestUsagesAliasedBy`.
  - `TestUsagesNone`, which expects exit 0 and "no usages".
  - `TestUsagesUnknown`, which expects exit 1 and a suggestion.
  - `TestUsagesPlatformFilter`.
- [ ] Step 2: Run them and see them FAIL.
- [ ] Step 3: Implement.
- [ ] Step 4: Run the suite and see it pass.
- [ ] Step 5: Commit `feat: airframe usages`.

### Task 3: `diff`

**Files:** Create `internal/gitfs/`, `internal/tokendiff/` and `internal/cli/diff.go`. Add `internal/schema/diff.schema.json`. Add tests: build a temp git repo in the test with `git init`, two commits, and skip if git is absent.

- [ ] Step 1: Write the tests:
  - `TestGitFS`: read, stat, a dir listing, a missing path.
  - `TestCompare`: added, removed, a value change per mode, tier, retired, possible rename.
  - `TestDiffFormats`: text, md and json goldens.
  - `TestDiffWorkingTree`.
  - `TestDiffRefInjection`.
  - `TestDiffContentMoved`.
  - `TestDiffNoGit`, with PATH cleared.
  - `TestDiffExitCode`.
- [ ] Step 2: Run them and see them FAIL.
- [ ] Step 3: Implement.
- [ ] Step 4: Run the suite and see it pass.
- [ ] Step 5: Commit `feat: airframe diff between git refs`.

### Task 4: End to end and docs

**Files:**
- `cmd/airframe/testdata/script/{find,usages,diff}.txtar`. The diff script makes its own git repo inside the script, using the `exec git` commands that testscript provides; skip it if git is absent.
- `docs/guides/reference.md`: covers `find`, including the synonym list, `--file` and `--type`; `usages`; and `diff`, including the formats, a GitHub Actions job-summary snippet `airframe diff origin/main --format md >> "$GITHUB_STEP_SUMMARY"` and `--exit-code`. Its quoted outputs are asserted by a guide test.
- `README.md` and `docs/docs_test.go`: the three commands join the built list marked "(next release)". Planned keeps `rename`, `retire`, `specimen` and the surfaces.
- Spec: the §6.5 lines and §6.7 item 6, partly done.
- Action (`action.yml`): unchanged. The guide shows the summary snippet instead of adding an input. Adding an input is deferred to an issue.

- [ ] Step 1: Write the txtars and the guide test, and see them FAIL.
- [ ] Step 2: Write the docs.
- [ ] Step 3: Run `go test ./... && go vet ./... && gofmt -l .` and see it clean.
- [ ] Step 4: Commit `feat: reference commands end to end`.
