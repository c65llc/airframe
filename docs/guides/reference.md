# The reference commands

Six commands answer the questions people ask of a design system while
they work in it, and make the changes they lead to, instead of editing
token files by hand. Three of them read:

- **`airframe find <words>`:** which token is for this? It ranks the
  tokens by the words of their names, descriptions and components, and
  with `--file` keeps only the ones that file may read.
- **`airframe usages <token>`:** who reads this token? Every consumer
  reference on every platform, direct or through a wrapper, and every
  token that aliases it.
- **`airframe diff <ref-a> [<ref-b>]`:** what did this change do to the
  tokens? Added, removed, possibly renamed and changed tokens between two
  git refs, or between a ref and the working tree, as text, Markdown for a
  pull request's job summary, or JSON.

None of these writes a file. Three write:

- **`airframe rename <old> <new>`:** a codemod. It renames a token in its
  token files, `airframe.yaml`, the components, the invariants and every
  platform's consumer files, then rebuilds.
- **`airframe retire <token>`:** marks a token retired and lists what
  lint will then report. With `--treatment`, it records a retired
  treatment in `retired.yaml` instead.
- **`airframe specimen`:** draws the colour, type and spacing tokens as
  SVG sheets.

Each of the writers works out every change before it writes anything,
and `rename` and `retire` take `--dry-run` to print the changes and stop.
All six read the project as `lint` does, so a project that does not load
is reported the same way (exit 1, with its diagnostics).

## 1. The project

The examples run against one small web project. Its config:

```yaml
# airframe.yaml
version: 1
content: design-system
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ":root[data-theme='dark']"}}
platforms:
  web:
    emit: [css]
    out: {css: web/src/generated/tokens.css}
    consumers: ["web/src/**/*.{css,tsx}"]
```

Its tokens are written one per line so they fit here; `airframe fmt`
would spread them out. The primitives:

**`design-system/tokens/primitive.tokens.json`**

```json
{
  "gray": {
    "$type": "color",
    "50": {"$value": {"colorSpace": "oklch", "components": [0.98, 0.004, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "500": {"$value": {"colorSpace": "oklch", "components": [0.55, 0.01, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "600": {"$value": {"colorSpace": "oklch", "components": [0.45, 0.01, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "900": {"$value": {"colorSpace": "oklch", "components": [0.2, 0.012, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}
  },
  "blue": {
    "$type": "color",
    "500": {"$value": {"colorSpace": "oklch", "components": [0.55, 0.15, 255]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}
  },
  "space": {
    "$type": "dimension",
    "2": {"$value": {"value": 8, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "4": {"$value": {"value": 16, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}
  }
}
```

The semantic tokens, with a description each, and one retired token:

**`design-system/tokens/semantic.tokens.json`**

```json
{
  "surface": {"$type": "color", "$value": "{gray.50}", "$description": "The page background.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "ink": {"$type": "color", "$value": "{gray.900}", "$description": "Body text.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "ink-muted": {"$type": "color", "$value": "{gray.500}", "$description": "Secondary text: captions and hints.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "accent": {"$type": "color", "$value": "{blue.500}", "$description": "Links and the primary action.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "gap": {"$type": "dimension", "$value": "{space.2}", "$description": "The space between items in a row.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "legacy-ink": {"$type": "color", "$value": "{gray.900}", "$description": "Old body text.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic", "retired": true}}}
}
```

The dark appearance:

**`design-system/themes/dark.tokens.json`**

```json
{
  "$extensions": {"com.c65llc.airframe": {"mode": "appearance:dark"}},
  "surface": {"$value": "{gray.900}"},
  "ink": {"$value": "{gray.50}"}
}
```

And one component, the button, which owns two tokens:

**`design-system/tokens/component.tokens.json`**

```json
{
  "button": {
    "$type": "color",
    "bg": {"$value": "{accent}", "$description": "The button's fill.", "$extensions": {"com.c65llc.airframe": {"tier": "component", "component": "button"}}},
    "ink": {"$value": "{surface}", "$extensions": {"com.c65llc.airframe": {"tier": "component", "component": "button"}}}
  }
}
```

```yaml
# design-system/components/button.yaml
id: button
noun: Button
ruling: "@ruling:button"
tokens: [button-bg, button-ink]
owners: [web/src/button/**]
implementations:
  web: web/src/button/Button.tsx
```

Two consumer files read them. The button is one of its own owners; the
app stylesheet is not, and borrows the button's fill anyway (lint reports
that as [`token-owner-violation`](../rules/token-owner-violation.md)):

```tsx
// web/src/button/Button.tsx
export const Button = () => <button style={{ background: "var(--button-bg)", color: "var(--button-ink)" }} />;
```

```css
/* web/src/app.css */
.app {
  background: var(--surface);
  color: var(--ink);
}
.app .cta {
  background: var(--button-bg);
}
```

Last, the invariants hold body text to a contrast floor in both appearances,
and every gap token to a minimum size:

```yaml
# design-system/invariants.yaml
- id: body-text
  kind: contrast
  fg: ink
  bg: surface
  min: 4.5
  forEach: [appearance]
- id: gap-floor
  kind: min-dimension
  match: "gap*"
  min: 4
```

## 2. `airframe find`

`find` takes the words you would use to describe what you are styling and
lists the tokens that match, best first:

```text
$ airframe find muted text
ink-muted  semantic  color  var(--gray-500)  — Secondary text: captions and hints.
ink  semantic  color  var(--gray-900)  — Body text.
button-ink  component  color  var(--surface)
```

Each line is the token's name, tier, type and base value in CSS spelling
(an alias is `var(--target)`), then its description when it has one. A
missing fact prints as `-`.

### How ranking works

The query is split into words: lowercase runs of letters. Digits and
punctuation separate words and are dropped, so `space 2` searches for
`space` alone. The stopwords `a`, `an`, `the`, `for`, `of`, `on`, `in`,
`to` and `with` are dropped too; a query with no words left is a usage
error.

Each word earns points against each token. A token's name is split into
words the same way (`ink-muted` is `ink` and `muted`), as are its
description and its component's `noun` (or the component's id when it
has no noun):

| A query word… | Points |
| --- | --- |
| is a whole word of the name | 3 |
| starts a word of the name | 2 |
| is a whole word of the description | 2 |
| starts a word of the description | 1 |
| is a word of the component's noun | 1 |

A word earns the best of the two name rows, plus the best of the two
description rows, plus the noun row. "Starts" means the query word is
the beginning of a longer word (`back` starts `background`), not the
other way round: there is no stemming, so `backgrounds` does not match
`background`.

A word also matches through its **synonyms**: role words that stand for
one another. A match through a synonym earns one point less in each row
than the word itself would, and a word earns the most it can, directly or
through any one synonym; the scores are not added together.

| Words that stand for one another |
| --- |
| `muted`, `subtle`, `secondary` |
| `bg`, `background`, `surface` |
| `fg`, `text`, `ink`, `foreground` |
| `border`, `stroke`, `outline`, `divider` |
| `accent`, `brand`, `primary` |
| `danger`, `error`, `destructive` |
| `gap`, `space`, `spacing` |
| `radius`, `corner`, `rounding` |

A token's score is the sum of its words' points. Above, `ink-muted` earns
3 for `muted` (a whole word of its name) and 2 for `text` (through `ink`,
one less than 3), so 5; `ink` and `button-ink` earn 2 each. A token that
scores 0 is not listed. Ties are broken by tier (semantic, then
component, then primitive, then any other) and then by name, so a
semantic token is offered before the primitive it reads. At most 10 hits
are listed; `--limit N` changes that.

The component noun helps when the words name a component:

```text
$ airframe find button background
button-bg  component  color  var(--accent)  — The button's fill.
button-ink  component  color  var(--surface)
surface  semantic  color  var(--gray-50)  — The page background.
```

### `--file`: what may this file read?

`--file F` keeps only the tokens the consumer file `F` may read, by the
same rules lint applies: a primitive is read by no consumer, a semantic
token by any consumer file, and a component token by its component's
owners (or by anyone, when the component is `shared`). Each hit says why
on a second line:

```text
$ airframe find background --file web/src/app.css
surface  semantic  color  var(--gray-50)  — The page background.
  semantic: consumers may read
```

`F` is a path **relative to the working directory**, as your editor or
shell gives it, or an absolute path; it need not exist yet. It must be
inside the project root, or `find` exits 2. Run from `web/src`, the same
file is `--file app.css`.

When the query matches tokens but the file may read none of them, `find`
says so, names the first match and why the file may not read it, and
exits 1:

```text
airframe find: 2 tokens match "button" but web/src/app.css may read none of them (button-bg: component button: owners (web/src/button/**) do not match web/src/app.css)
```

### The other flags

- **`--type T`** keeps only tokens of DTCG type `T` (`color`,
  `dimension`, …), ignoring case.
- **`--retired`** includes retired tokens, which are left out by default:
  `legacy-ink` is only found with `airframe find ink --retired`.
- **`--limit N`** lists at most `N` hits (default 10); `N` must be a
  positive number.
- **`--json`** prints the hits as a JSON array, with each one's score
  and, with `--file`, its `why`; no hits print `[]`:

```text
$ airframe find gap --limit 1 --json
[
  {
    "name": "gap",
    "tier": "semantic",
    "type": "dimension",
    "value": "var(--space-2)",
    "description": "The space between items in a row.",
    "score": 3
  }
]
```

### Exit status

| Exit | When |
| --- | --- |
| 0 | at least one hit was listed |
| 1 | nothing matches (with a "did you mean" when a token's name is close), every match is one `--file` may not read, or the project does not load |
| 2 | a usage error: no query, a query with no words, a `--limit` that is not a positive number, or a `--file` outside the project root |

## 3. `airframe usages`

`usages <token>` lists what reads a token and what aliases it, from the
same consumer scan lint runs:

```text
$ airframe usages button-bg
button-bg
tier: component
component: button
references:
  web  web/src/app.css:7:19  --button-bg  [not owner]
  web  web/src/button/Button.tsx:2:63  --button-bg  [owner]
```

- **References** are every read of the token in a platform's `consumers`
  files: the platform, the file, line and column, and the spelling as
  written. A read through a declared wrapper says so, as `(via
  wrapper:<root>)`.
- **A component token's reads are marked:** `[owner]` when the file is
  one of the component's owners, `[not owner]` when it is not (each of
  those is a `token-owner-violation`), or `[shared]` when the component
  is shared, so any file may read it.
- **Aliased by** lists every token whose value aliases this one
  directly, with the value's key (`default`, or the mode key such as
  `appearance:dark`) and where it is written:

```text
$ airframe usages gray-900
gray-900
tier: primitive
component: -
aliased by:
  ink  default  design-system/tokens/semantic.tokens.json:3
  legacy-ink  default  design-system/tokens/semantic.tokens.json:7
  surface  appearance:dark  design-system/themes/dark.tokens.json:3
```

A token nothing reads or aliases prints `no usages`; that is an answer,
not an error, and exits 0. `--platform P` keeps one platform's
references (aliases are listed either way); an unknown platform exits 2.
An unknown token exits 1, suggesting the closest name. `--json` prints
the same, with `owner` (true or false) or `shared` (true) on each
component token's read.

## 4. `airframe diff`

`diff` compares the project's tokens at git ref A with those at ref B,
or, given one ref, with the working tree. Here a branch, `tidy-tokens`,
darkens the muted ink, renames `gap` to `row-gap` and deletes the
retired `legacy-ink`:

**After: `design-system/tokens/semantic.tokens.json`**

```json
{
  "surface": {"$type": "color", "$value": "{gray.50}", "$description": "The page background.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "ink": {"$type": "color", "$value": "{gray.900}", "$description": "Body text.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "ink-muted": {"$type": "color", "$value": "{gray.600}", "$description": "Secondary text: captions and hints.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "accent": {"$type": "color", "$value": "{blue.500}", "$description": "Links and the primary action.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "row-gap": {"$type": "dimension", "$value": "{space.2}", "$description": "The space between items in a row.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}}
}
```

On that branch, against the main branch it will merge into:

```text
$ airframe diff origin/main
origin/main → working tree: 1 added, 2 removed, 1 possibly renamed, 1 changed

added (1):
  row-gap  dimension  semantic  8px

removed (2):
  gap         dimension  semantic  8px
  legacy-ink  color      semantic  #12171b oklch(20% 0.012 250)

possibly renamed (1):
  gap → row-gap

changed (1):
  ink-muted
    value base: #6d7277 oklch(55% 0.01 250) → #51565b oklch(45% 0.01 250)
```

- **Added and removed** tokens are listed with their type, tier and base
  value; a colour shows its sRGB hex beside its `oklch()`.
- **Possibly renamed** pairs a removed token with an added one of the
  same type, tier and component and the same value in every context. It
  is a guess, so both stay in the added and removed lists.
- **Changed** tokens are those both sides have, with each difference:
  the resolved value in each context (`base`, the value with no mode
  set, then each mode key such as `appearance:dark`), then the type,
  tier, component, `retired` flag and description. Values are compared
  resolved, so re-pointing an alias at a token with the same value is no
  change.

### Refs

- **One ref** compares that ref with the working tree, as it is on disk,
  committed or not: `airframe diff HEAD` shows your uncommitted token
  edits. **Two refs** compare two commits.
- A ref is anything `git rev-parse` takes that names a commit: a branch,
  a tag, a remote branch such as `origin/main`, a SHA, or `HEAD~1`.
- **Each side loads with its own `airframe.yaml`,** so a change that
  moves the content folder or adds a mode axis is compared correctly.
- **A side that does not load** (a token file that does not parse, or
  an `airframe.yaml` that is there but broken) exits 1 with its
  diagnostics, naming the ref and its commit.
- **Symlinks:** a ref side is read from git's objects, which store a
  symlink as a link, so a symlinked token or config file is skipped at a
  ref, while the working tree follows them. So a token file
  that is a symlink is missing from every ref side, and its tokens show
  as added when you compare a ref with the working tree.

### A ref from before `airframe.yaml`

A ref from before the project was adopted, with no `airframe.yaml` in
the project's directory at that commit, is an **empty side**: every
token of the other side is added (or removed), and a note on stderr says
so. If the project arrives in the pull request, `origin/main` has none:

```text
airframe diff: origin/main has no airframe.yaml at the repository root; comparing against an empty token set
```

The Markdown output repeats the note under its heading, so the pull
request that adopts Airframe still gets a useful summary, and the JSON
marks that side `"missing": true`.

### Formats

`--format text` (the default) is above. `--format md` is the same
report as Markdown, for a pull request's job summary: tables of tokens,
with `■` marking a colour and its hex and `oklch()` in separate code
spans. Past 50 changed tokens, the changed list is folded into a
`<details>` block.

```text
$ airframe diff origin/main --format md
## Token changes: `origin/main` → working tree

1 added, 2 removed, 1 possibly renamed, 1 changed.

### Added (1)

| token | type | tier | value |
| --- | --- | --- | --- |
| `row-gap` | dimension | semantic | `8px` |

### Removed (2)

| token | type | tier | value |
| --- | --- | --- | --- |
| `gap` | dimension | semantic | `8px` |
| `legacy-ink` | color | semantic | ■ `#12171b` `oklch(20% 0.012 250)` |

### Possibly renamed (1)

- `gap` → `row-gap`

### Changed (1)

#### `ink-muted`

| context | before | after |
| --- | --- | --- |
| base | ■ `#6d7277` `oklch(55% 0.01 250)` | ■ `#51565b` `oklch(45% 0.01 250)` |
```

`--format json` prints `from` and `to` (each a `ref` and the `commit`
it resolved to, or `{"workingTree": true}`), then `added`, `removed`,
`renamed` and `changed`, every list sorted by name; see
[the JSON schemas](#8-the-json-schemas).

### `--exit-code`

By default `diff` exits 0 whatever changed, as a report should.
`--exit-code` exits 1 when the tokens changed, as `git diff --exit-code`
does, so a script can ask "did this touch the tokens?":

```sh
airframe diff origin/main --exit-code > /dev/null || echo "tokens changed"
```

A possible rename counts as a change. Any failure (git missing, a bad
ref, a directory outside a git work tree) exits 2, and a side that does
not load exits 1, with or without the flag.

### Safety

- **Refs are resolved to commits** before anything is read (`git
  rev-parse --verify <ref>^{commit}`), and every ref is resolved before
  either side loads, so a bad second ref is reported first. A ref that
  names no commit exits 2, naming it.
- **A ref that looks like an option is refused.** An argument starting
  with `-` is a flag to `airframe`, and an unknown one is a usage error;
  after `--` (`airframe diff -- -x`), a ref starting with `-`, an empty
  ref and one containing a NUL byte are refused before git sees them
  (exit 2). Git is never run through a shell.
- **`diff` never writes.** Nothing is checked out, staged or committed:
  the tree is listed with `git ls-tree` and files are read with `git
  cat-file`, with optional locks off, so `git status` is the same before
  and after. Variables that would point git at another repository or
  inject config are removed from its environment; git still reads its
  usual system, user and repository config.

### In CI: a job summary on every pull request

The `c65llc/airframe@v0` Action runs `lint` and `build --check`. To show
reviewers what a pull request does to the tokens, add a step that writes
`diff --format md` to the job summary:

```yaml
- uses: actions/checkout@v4
  with:
    fetch-depth: 0
- if: github.event_name == 'pull_request'
  run: airframe diff "origin/$GITHUB_BASE_REF" --format md >> "$GITHUB_STEP_SUMMARY"
```

The step runs only on pull requests, where `GITHUB_BASE_REF` names the
branch the pull request merges into. It is read from the environment and
quoted for the shell, never spliced into the script by the workflow.

`diff` needs the base branch's commit. `actions/checkout` fetches only
the pull request's merge commit by default, so either set
`fetch-depth: 0`, as above, or fetch the base ref yourself before the
step (`git fetch --depth=1 origin "$GITHUB_BASE_REF"`). The step assumes
`airframe` is on the runner's `PATH`; install it as the
[README](../../README.md#install) says.

## 5. `airframe rename`

`rename <old> <new>` renames a token everywhere it is spelled, then
rebuilds. Renames ship with a codemod, never with compatibility aliases:
there is no alias left behind under the old name, so the old name is
gone from the sources, the outputs and the consumer code in one change.

### Preconditions

`rename` refuses, exits 1 and writes nothing unless:

- the project loads without errors and builds;
- `old` is a token (`there is no token nope`);
- `new` is a valid token name, lowercase words joined by `-`;
- `new` is not a token already, nor a group: `button` holds
  `button-bg` and `button-ink`, so `rename ink button` is refused;
- the new path does not sit under a token (`ink.subtle` would put a
  token inside `ink`);
- the renamed project loads and builds, and no consumer reference still
  reads `old` once the edits are made.

**The new path.** A token's name is its DTCG path joined with `-`, so
`button.ink` is `button-ink`. When the old token's path is its name split
on `-`, the new path is the new name split the same way: `gap` at path
`gap` becomes `row-gap` at `row.gap`. When it is not, because a segment
holds a `-` itself, `rename` cannot guess, and `--path` gives the new
path, dotted:

```text
airframe rename: ink-muted is at path ink-muted, not ink.muted; give the new path with --path
```

```text
$ airframe rename ink-muted ink-subtle --path ink-subtle --dry-run
would rename ink-muted to ink-subtle (path ink-muted → ink-subtle)
design-system/tokens/semantic.tokens.json:4  move ink-muted → ink-subtle
```

### What it edits

- **Token files.** The token's node moves from the old path to the new
  one in its base file and in every mode file that overrides it. Within
  its group it keeps its place; moved to another group, it is appended
  there. Groups are created as needed, and a group left empty is
  deleted. Every alias `{old.path}` becomes `{new.path}`,
  in values, mode values and inside composite values such as a shadow's
  colour. If the new group would give the token a different inherited
  `$type`, the token gets its own.
- **`airframe.yaml`.** `options.accentColor`, the `material3Scheme`
  role values, the `highContrast` keys, wrapper `map` values, and a
  `names` entry for a fixed-naming format (css, json, colorsets) that
  spells the default name.
- **Components.** Each `tokens` entry of `components/<id>.yaml`.
- **Invariants.** The exact token names in `tokens`, `fg`, `bg`, `only`
  and `match`.
- **Consumer references, on every platform.** Each read the consumer
  scan finds is respelled where it stands, as an exact byte splice at its
  line and column; nothing else in a consumer file changes, and consumer
  files are never reformatted. The new spelling is the same emitted name
  of the new token: a suffixed or slot name follows its token, so in XAML
  `DesignGapCompact` becomes `DesignSpacingCompact`, and a Kotlin colour
  slot `surfaceCompact` becomes `canvasCompact`. A read through a generated
  wrapper has its member renamed; a read through a `map` wrapper needs
  no code edit, as the map's value is edited instead. A retired
  spelling is not a token reference, so it is left alone.
- **Generated outputs.** They are rebuilt, as `build` would. Renaming a
  colour leaves the old name's colorset in each asset catalog; `rename`
  removes the colorsets the rename makes stale, and lists each one
  (`removed <dir>`). It removes only the `Contents.json` and then the
  folder, which must then be empty, so a file you added there stays.

The token, config, component and invariants files it touches are
written in canonical form, as `airframe fmt` would.

### `--dry-run`

`--dry-run` prints the plan and writes nothing: one line per text edit
(`file:line:col  old → new`), then each token moved (`file:line  move
from → to`), then each manual item, then `would remove <dir>` for each
stale colorset. Renaming the page background:

```text
$ airframe rename surface canvas --dry-run
would rename surface to canvas (path surface → canvas)
design-system/invariants.yaml:5:7  surface → canvas
design-system/tokens/component.tokens.json:5:24  {surface} → {canvas}
web/src/app.css:3:19  --surface → --canvas
design-system/themes/dark.tokens.json:3  move surface → canvas
design-system/tokens/semantic.tokens.json:2  move surface → canvas
```

The same command without `--dry-run` prints the plan, then each file it
wrote, the regenerated CSS last:

```text
$ airframe rename surface canvas
renaming surface to canvas (path surface → canvas)
design-system/invariants.yaml:5:7  surface → canvas
design-system/tokens/component.tokens.json:5:24  {surface} → {canvas}
web/src/app.css:3:19  --surface → --canvas
design-system/themes/dark.tokens.json:3  move surface → canvas
design-system/tokens/semantic.tokens.json:2  move surface → canvas
wrote design-system/invariants.yaml
wrote design-system/themes/dark.tokens.json
wrote design-system/tokens/component.tokens.json
wrote design-system/tokens/semantic.tokens.json
wrote web/src/app.css
wrote web/src/generated/tokens.css
```

A move that appends the token to a different group says so, as the order
of the generated outputs changes with it.

### Manual items and `--allow-manual`

Two kinds of reference cannot be rewritten safely, and are left to you
as **manual items**:

- **Wrapper patterns.** A pattern template in `airframe.yaml` is not
  edited. A read that resolves through a pattern to `old` is a manual
  item at that read, and so is a pattern that can now produce `new`,
  since a member that read no token would start reading it.
- **Invariant globs.** A glob in `match` or `only` that matched `old`
  and does not match `new`, or the reverse, would change what the
  invariant covers.

`gap-floor` covers every token matching `gap*`, and `row-gap` does not
match it:

```text
$ airframe rename gap row-gap --dry-run
would rename gap to row-gap (path gap → row.gap)
design-system/tokens/semantic.tokens.json:6  move gap → row.gap (appended to its new group: the emitted order changes)
design-system/invariants.yaml:10  manual: invariant gap-floor: match "gap*" matched gap and does not match row-gap, so what it covers changes; adjust it by hand
```

Without `--allow-manual`, a plan with manual items prints the plan,
writes nothing and exits 1:

```text
airframe rename: 1 reference(s) are left to finish by hand (manual, above); nothing was written. Pass --allow-manual to write the rest.
```

`--allow-manual` writes everything else, lists the manual items, and
exits 0 (or as the rebuild does): the manual items are yours to finish.
Here, until the glob is changed, lint reports that `gap-floor` selects no
tokens.

```text
$ airframe rename gap row-gap --allow-manual
renaming gap to row-gap (path gap → row.gap)
design-system/tokens/semantic.tokens.json:6  move gap → row.gap (appended to its new group: the emitted order changes)
design-system/invariants.yaml:10  manual: invariant gap-floor: match "gap*" matched gap and does not match row-gap, so what it covers changes; adjust it by hand
wrote design-system/tokens/semantic.tokens.json
wrote web/src/generated/tokens.css
1 reference(s) left to finish by hand (manual, above)
```

### Writing

- **A file changed since it was read is refused.** Before writing,
  `rename` reads every file it plans to change again and compares it
  with the bytes the plan was made from. If any differs (an editor saved
  it meanwhile), it exits 1 with `airframe rename: <file> changed since
  it was read; nothing was written`. Run it again. Each consumer splice is
  checked the same way while planning: the bytes at the reference's
  position must still be what the scan read.
- **A symbolic link is refused** (exit 1), as for `retire` below.
- **Writes are staged.** Every file is first written to a temporary name
  beside it, and only when all are written are they renamed into place,
  in path order. A failure while staging removes the temporary files and
  changes nothing. **The final rename phase is not atomic:** a rename
  within one directory almost never fails, but if one does, the files
  already renamed stay renamed. `rename` lists them as `wrote` lines,
  says how many of the total were written, and exits 2. Your version control has the rest.
- The generated outputs are rebuilt after the sources are written, from
  the project read back from disk.

| Exit | When |
| --- | --- |
| 0 | the rename was written, or `--dry-run` printed the plan |
| 1 | a precondition failed, the renamed project would not load or build, a reference would be left reading `old`, manual items without `--allow-manual`, a planned file changed since it was read, or a symbolic link |
| 2 | a usage error, or an I/O error, including a failure partway through the renames |

## 6. `airframe retire`

`retire` takes a token, or with `--treatment` a treatment that has no
token, out of use. It never edits consumer code: retiring is what makes
lint find the reads, through
[`retired-used`](../rules/retired-used.md).

### A token

`retire <token>` sets `retired: true` in the token's base declaration,
under `$extensions["com.c65llc.airframe"]`, creating either object if it
is missing, and writes the file in canonical form. The token is still
emitted, so nothing breaks at build time, but no consumer may read it and
no token may alias it. `retire` prints the consequences: each read lint
will now report, and each token that aliases it.

```text
$ airframe retire surface --dry-run
would retire surface in design-system/tokens/semantic.tokens.json
retired-used will report:
  web  web/src/app.css:3:19  --surface
aliased by:
  button-ink  default  design-system/tokens/component.tokens.json:5
```

A token nothing reads or aliases prints `no reads or aliases: lint will
report nothing`. A token that is already retired is refused:

```text
airframe retire: legacy-ink is already retired
```

### A treatment

`retire --treatment <id> --spelling <adapter>=<RE2>…` appends a
treatment to `<content>/retired.yaml`, creating the file if needed, and
prints the consumer code its spellings already match:

- **`--treatment ID`** is the treatment's id, in kebab-case; an id
  `retired.yaml` already has is refused.
- **`--spelling ADAPTER=RE2`** is required and repeats: an RE2 pattern
  that must not appear in that adapter's consumer files (`css`, `swift`,
  `kotlin`, `xaml` or `cs`). An unknown adapter or a pattern that does not
  compile is refused.
- **`--ruling @ruling:x`** names the ruling that retired it, and
  **`--marker M`** the marker that exempts an audited use (see
  [Components and retired treatments](components.md)).

The new file must load as `retired.yaml` does, or nothing is written.

```text
$ airframe retire --treatment inline-style --spelling 'css=style=\{\{' --ruling @ruling:no-inline-styles --dry-run
would retire treatment inline-style in design-system/retired.yaml
retired-used will report:
  web  web/src/button/Button.tsx:2:37  style={{
```

### Symbolic links

`retire` and `rename` write a file in place, so a file they would write
that is a symbolic link is refused, with `--dry-run` too, and nothing is
written. If `semantic.tokens.json` linked to a shared file:

```text
airframe retire: design-system/tokens/semantic.tokens.json is a symbolic link; edit the file it points to, or replace the link with the file, first. Nothing was written.
```

`retire` exits 0 when it retired (or with `--dry-run` would retire), 1
when it refused, and 2 on a usage error: no token and no `--treatment`,
both, `--treatment` with no `--spelling`, or `--spelling`, `--ruling` or
`--marker` without `--treatment`.

## 7. `airframe specimen`

`specimen` draws the tokens as SVG sheets, for a design review or a
pull request:

```text
$ airframe specimen
wrote design-system/generated/specimen/colors.svg
wrote design-system/generated/specimen/spacing.svg
wrote design-system/generated/specimen/type.svg
```

- **`colors`** has one row per colour token, grouped by tier (semantic,
  component, primitive) in declaration order: its name, then a swatch
  per appearance (one column with no appearance axis), with the sRGB hex
  and `oklch()` under each. A colour outside sRGB is gamut-mapped, as
  CSS Color 4 does, for the swatch and its hex. A retired token is marked
  `(retired)`.
- **`type`** has one row per `fontFamily` and `fontWeight` token, and per
  dimension whose name holds `font-size` or `line-height`, each with a
  sample line set in it. Fonts are named, not embedded, so the sheet
  falls back to the viewer's system fonts.
- **`spacing`** has every other dimension as a bar of its px value (a rem
  is 16px), with its name and value. Bars are drawn at most 480px long;
  the label keeps the real value.

A sheet with nothing to draw (this project has no type tokens) is still
written, with its title. Text is drawn in the light appearance's colours,
so the sheets read on white.

**One set per fan-out value.** When a platform fans out over a build
axis (per app, say), there is one set of sheets per value of that axis,
named `colors-<value>.svg`, `type-<value>.svg` and
`spacing-<value>.svg`; a value two axes share is named
`<axis>-<value>`. With no fan-out, as here, there is one set.

**Where they go.** The default directory is
`<content>/generated/specimen/`; `--out D` writes to `D` instead, a
directory inside the project. A directory outside it, or one with a drive
letter, is a usage error (exit 2).
The files are overwritten each run, and the output is byte-stable: the
same tokens draw the same bytes. `specimen` is not part of `build`, so
`build --check` neither writes nor checks the sheets; regenerate them
when you want them.

**Layout.** Each column is as wide as the widest text it holds,
measured without a font at a fixed average width per character, plus a
gap: the swatch columns fit their longest hex or `oklch()` label, and the
type sample column its largest sample (set at most 72px). The name column
is capped at 480px: a longer name is cut with `…`, and its tooltip (the
SVG `<title>`) carries the whole name with the description.

## 8. The JSON schemas

Each command's JSON output has a JSON Schema (draft 2020-12), printed
by `airframe schema`:

| Command | Schema | Shape |
| --- | --- | --- |
| `find --json` | `airframe schema find` | an array of hits, best first: `name`, `tier`, `type`, `value`, `description` and `score`, and `why` with `--file` |
| `usages --json` | `airframe schema usages` | `name`, `tier`, `component`, `references` (each `platform`, `file`, `line`, `col`, `raw`, `via`, and for a component token exactly one of `owner` or `shared`) and `aliasedBy` (each `name`, `key`, `file`, `line`) |
| `diff --format json` | `airframe schema diff` | `version` (1), `from` and `to` (a ref with its `commit` and, for an empty side, `missing`; or `workingTree`), `added` and `removed` (each `name`, `type`, `tier`, `component`, `value`), `renamed` (`from`, `to`) and `changed` (each `name` and its `changes`: a `value` change has a `context` and `before` and `after` values, each a `value` and for a colour its `hex`, or an `error`; any other kind has `before` and `after`) |
