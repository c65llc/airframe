# Components and retired treatments

A design system is more than its tokens. It is also the list of things
it agreed to build, and the list of things it agreed to stop doing.
Airframe keeps both as files beside the tokens and lints them:

- **The component inventory.** Each component has a file under
  `<content>/components/` naming the ruling it answers to, the tokens it
  owns, the files allowed to read them, and where each platform builds
  it. A component with no ruling is not in the system.
- **Retired tokens and treatments.** A token marked `retired: true` is
  still emitted, so code keeps compiling while it migrates, but nothing
  may read it. A treatment with no token to delete (a 3px inset bar, a
  hand-drawn shadow) is held in `<content>/retired.yaml` by its spellings
  on every platform, so it cannot quietly come back.

Three lint rules hold them:
[`component-unruled`](../rules/component-unruled.md),
[`component-incomplete`](../rules/component-incomplete.md) and
[`retired-used`](../rules/retired-used.md). They run in every lint output
format, the gate included, and each reports what it checked.

This guide uses the token set from the repository's
`internal/selftest/fixtures/clean-consumers` fixture: semantic `surface`,
`ink`, `accent`, `gap`, `hairline` and the retired `legacy-ink`;
primitives `gray-50`, `gray-900`, `blue-500`, `space-2`, `space-4` and
`line-1`; and the `button` component's `button-bg` and `button-ink`. The
consumer rules themselves (declaring `consumers`, wrappers, owners) are in
[Consumers: checking where tokens are read](consumers.md).

## 1. The project

Two platforms declare consumer files:

```yaml
# airframe.yaml
version: 1
content: design-system
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ":root[data-theme='dark']"}}
  density: {values: [compact, standard], base: standard, binding: runtime, web: {attr: data-density}}
platforms:
  web:
    emit: [css]
    out: {css: web/src/generated/tokens.css}
    consumers: ["web/src/**/*.{css,tsx}"]
  apple:
    emit: [swift, colorsets]
    out: {swift: apple/Generated/DesignTokens.swift, colorsets: apple/Generated/Assets.xcassets/Tokens/}
    consumers: ["apple/Sources/**/*.swift"]
```

## 2. Component files

A component is `<content>/components/<id>.yaml`:

```yaml
# design-system/components/button.yaml
id: button
noun: Button
ruling: "@ruling:button"
tokens: [button-bg, button-ink]
owners:
  - web/src/button/**
  - apple/Sources/Button*.swift
implementations:
  web: web/src/button/Button.tsx
  apple: apple/Sources/ButtonStyle.swift
```

| Field | Required | What it says |
| --- | --- | --- |
| `id` | yes | The component's id, kebab-case; it equals the file's basename. |
| `noun` | no | What people call it, for `show` and the inventory. |
| `ruling` | yes | `"@ruling:<id>"`: the Gantry ruling it answers to (§3). |
| `tokens` | no | The component tokens it owns. Each must exist, be tier `component` and say `component: <id>`; every token that names the component must be listed. |
| `owners` | unless `shared` | Repo-relative globs: the files allowed to read its tokens. Declared, never inferred. |
| `shared` | no | `true` lets any consumer read its tokens. Default `false`. |
| `hostedBy` | no | The container component that hosts this one (§5), or `null`. |
| `implementations` | for each platform with consumers | Platform name to the file that builds it there, or `"n/a: <reason>"` (§4). |

Any other key is `schema-invalid`, and `airframe fmt` rewrites the file
in canonical form. Both files the button names exist:

```tsx
// web/src/button/Button.tsx
export const Button = () => <button style={{ background: "var(--button-bg)", color: "var(--button-ink)" }} />;
```

```swift
// apple/Sources/ButtonStyle.swift
import SwiftUI

struct PrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(DesignTokens.buttonBg)
            .foregroundStyle(DesignTokens.buttonInk)
    }
}
```

## 3. Rulings and Gantry

`ruling` is always checked for its shape: `@ruling:` followed by an id of
`a-z`, `0-9` and `-`, starting with a letter or digit. A missing or
misspelt ruling is [`component-unruled`](../rules/component-unruled.md).

When the repository is also a Gantry
project, the reference is resolved against Gantry's records. Airframe
reads the files only; Gantry is not run and not a dependency.

- **Which Gantry project.** A `gantry.yaml` next to `airframe.yaml` is
  used when one exists. To use another, set the top-level key
  `gantry: tools/gantry/gantry.yaml` (a repo-relative path to the file)
  in `airframe.yaml`. A configured file that does not exist is
  `schema-invalid`; with no key and no file, resolution is syntax only.
- **Where the rulings are.** Gantry's `content` (default `docs/design`)
  is relative to the folder holding `gantry.yaml`, and must stay inside
  it. Each ruling is `<content>/rulings/<id>.md`.
- **What is read.** The front matter, between `---` lines at the top of
  the file: `id` (equal to the file name), `status` (`proposed`, `ruled`,
  `superseded` or `retired`) and, for a superseded ruling, `supersededBy`.
  Anchors, aliases, merge keys and duplicate keys are refused. A record
  that does not read (no closing `---`, invalid YAML, an id that does not
  match its file name) is a `component-unruled` finding at the record,
  never a crash.
- **Symlinks are refused at every level:** `gantry.yaml`, the content
  and `rulings` folders (`schema-invalid`), and each ruling file
  (`component-unruled` at the record). A link could lead the resolver out
  of the repository.

```yaml
# gantry.yaml
version: 1
content: docs/design
```

**`docs/design/rulings/button.md`**

```markdown
---
id: button
status: ruled
---
Buttons are drawn by the app: one filled shape, the accent behind the ink.
```

**`docs/design/rulings/old-menu.md`**

```markdown
---
id: old-menu
status: superseded
supersededBy: menu
---
The first menu ruling, before menus could nest.
```

**`docs/design/rulings/menu.md`**

```markdown
---
id: menu
status: ruled
---
Menus nest, and open on press.
```

**`docs/design/rulings/toast.md`**

```markdown
---
id: toast
status: proposed
---
Toasts are still being argued.
```

**`docs/design/rulings/selection-treatment.md`**

```markdown
---
id: selection-treatment
status: ruled
---
A selected row is filled with accent-soft, never marked by a bar.
```

Only `ruled` passes. A `proposed` ruling is not yet ruled; a `superseded`
one names its replacement; a `retired` one is no longer in force. Two
components that answer to the wrong rulings:

```yaml
# design-system/components/menu.yaml
id: menu
noun: Menu
ruling: "@ruling:old-menu"
owners: [web/src/menu/**]
implementations:
  web: web/src/menu/Menu.tsx
  apple: n/a
```

```yaml
# design-system/components/toast.yaml
id: toast
ruling: "@ruling:toast"
owners: [web/src/toast/**]
implementations:
  web: "n/a: the web app shows inline banners instead"
```

```text
design-system/components/menu.yaml:4: error component-unruled: component menu: ruling old-menu is superseded by menu; point ruling at @ruling:menu (ruling)
design-system/components/toast.yaml:3: error component-unruled: component toast: ruling toast is proposed, not yet ruled; a component answers to a ruled ruling (ruling)
```

A ruling the Gantry project has no record of is reported too, naming the
`gantry.yaml` it looked in. With no Gantry project, all three components
above would pass this rule: the check is syntax only.

## 4. Implementations and `n/a`

`implementations` says where each platform builds the component, and
[`component-incomplete`](../rules/component-incomplete.md) holds it to
being complete and true:

- every platform that declares `consumers` needs an entry;
- every key must be a configured platform;
- every value is a repo-relative file that exists (not a folder, and not
  a symlink or a path through one), or `n/a: <reason>` saying why that
  platform does not build it. A bare `n/a`, or `n/a:` with nothing after
  it, says nothing and is reported.

`menu.yaml` above names a file that does not exist and gives apple no
reason; `toast.yaml` has no apple entry at all:

```text
design-system/components/menu.yaml:7: error component-incomplete: component menu: implementations.web: web/src/menu/Menu.tsx does not exist; implementations are checked, so name the file that builds the component (implementations.web)
design-system/components/menu.yaml:8: error component-incomplete: component menu: implementations.apple: n/a needs a reason: "n/a: <why this platform does not build it>" (implementations.apple)
design-system/components/toast.yaml:5: error component-incomplete: component toast: platform apple declares consumers, but implementations has no apple entry; name the file that builds the component there, or "n/a: <reason>" (implementations)
```

The check is that the file exists, not that it builds the component.

## 5. `hostedBy`

`hostedBy` names the container a component lives in: a split button's
menu half is hosted by the button.

```yaml
# design-system/components/split-button.yaml
id: split-button
noun: Split button
ruling: "@ruling:button"
hostedBy: button
owners: [web/src/button/**]
implementations:
  web: web/src/button/Button.tsx
  apple: "n/a: the Mac app uses a pull-down button"
```

A `hostedBy` that names no component is `component-incomplete`. So is a
chain of hosts that never ends: if `button` also said `hostedBy:
split-button`, the cycle is reported once, at its first member in id
order, naming every member (`hostedBy cycles: button → split-button →
button`), rather than once per component.

## 6. `airframe show <component>`

`show` prints a component's inventory, its ruling resolved against
Gantry, and who reads its tokens, from the same consumer scan lint runs:

```text
$ airframe show button
button
noun: Button
ruling: @ruling:button (ruled)
shared: false
hostedBy: -
hosts: split-button
tokens:
  button-bg: component
  button-ink: component
owners:
  apple/Sources/Button*.swift
  web/src/button/**
implementations:
  apple: apple/Sources/ButtonStyle.swift
  web: web/src/button/Button.tsx
consumers:
  apple:
    apple/Sources/ButtonStyle.swift: 2 references
  web:
    web/src/button/Button.tsx: 2 references
```

`--json` prints the same fields with stable keys. A superseded ruling
carries its replacement in `supersededBy`, which is omitted otherwise:

```text
$ airframe show menu --json
{
  "id": "menu",
  "noun": "Menu",
  "ruling": {
    "ref": "@ruling:old-menu",
    "status": "superseded",
    "supersededBy": "menu"
  },
  "shared": false,
  "hostedBy": "",
  "hosts": [],
  "tokens": [],
  "owners": [
    "web/src/menu/**"
  ],
  "implementations": {
    "apple": "n/a",
    "web": "web/src/menu/Menu.tsx"
  },
  "consumers": {}
}
```

A ruling's status is `ruled`, `proposed`, `superseded` or `retired` from
Gantry; `unchecked` with no Gantry project; `missing` when Gantry has no
such ruling; `unreadable` when its record did not read; and `malformed`
when the reference is not `@ruling:<id>`.

A name is looked up as a token first, then as a component. When it is
both, `show` prints the token and says on stderr how to get the
component: `airframe show --component <name>` forces component mode.
`show` exits 1 when there is no such token or component.

## 7. Retired tokens

A token is retired in its own file, with `retired: true` in its Airframe
extensions. It is **still emitted**, on every platform, so code that
reads it keeps compiling while it migrates. What
[`retired-used`](../rules/retired-used.md) reports is reading it:

- a consumer reference to it, direct (`var(--legacy-ink)`,
  `DesignTokens.legacyInk`) or through a declared wrapper;
- a non-retired token that aliases it **directly**, at that token. Only
  the first hop counts: a token that aliases a token that aliases a
  retired one is fixed by fixing the middle token, which is reported.

```css
/* web/src/app.css */
.app {
  color: var(--legacy-ink);
  background: var(--surface);
}
```

```text
web/src/app.css:3:14: error retired-used: platform web: --legacy-ink is token legacy-ink, which is retired; it is still emitted so code compiles while it migrates, but nothing may read it: read what replaced it
```

## 8. Retired treatments: `retired.yaml`

A treatment with no token to delete is held by how it is spelt. The file
is optional, a list of entries:

```yaml
# design-system/retired.yaml
- id: flush-left-selection-bar
  ruling: "@ruling:selection-treatment"
  spellings:
    css: ["inset 3px 0 0"]
    swift: ["\\.frame\\(width: 3\\)"]
  marker: audited-selection-bar
```

- **`id`** (required) is kebab-case and unique in the file.
- **`ruling`** (optional) is checked like a component's: its shape
  always, and against Gantry when there is a Gantry project.
- **`spellings`** (required) maps an adapter name (`css`, `swift`,
  `kotlin`, `xaml` or `cs`, the consumer adapters of the
  [consumers guide](consumers.md#2-what-each-adapter-matches)) to a
  non-empty list of RE2 patterns. Each pattern is matched against the
  consumer files that adapter reads, so a `css` spelling is looked for in
  stylesheets and in `.tsx`, `.html` and the other web files.
- **`marker`** (optional) is the treatment's own escape word (§9).

`airframe schema retired` prints the file's JSON Schema, and `airframe
fmt` formats it. A spelling in a component's code is a finding, naming
the treatment and its ruling:

```tsx
// web/src/row.tsx
export const Row = () => <li style={{ boxShadow: "inset 3px 0 0 var(--accent)" }} />;
```

```text
web/src/row.tsx:2:51: error retired-used: platform web: "inset 3px 0 0" is a spelling of retired treatment flush-left-selection-bar (@ruling:selection-treatment), matched by css pattern "inset 3px 0 0"; it must not come back: use what replaced it, or mark an audited use with audited-selection-bar: <reason> (or airframe-exempt retired-used: <reason>)
```

## 9. Treatment markers

An audited use is escaped by the treatment's marker followed by a
reason, `audited-selection-bar: <reason>`, or by the generic
`airframe-exempt retired-used: <reason>`:

```css
/* web/src/mail.css */
.mail-row {
  /* was: box-shadow: inset 3px 0 0 var(--accent); */
  box-shadow: inset 3px 0 0 red; /* audited-selection-bar: email clients ignore custom properties, see #42 */
}
```

- **A marker works only inside a comment,** on the matched line or alone
  on the comment-only line above it, as `airframe-exempt` markers are
  placed. The same word in code or in a string marks nothing.
- **It needs a reason.** A marker with none escapes nothing, and the
  finding says so.
- **Every escape is listed** in the gate JSON's `exemptions`, with its
  file, line and reason, the `rule` (`retired-used`) and the `marker`
  word that escaped it, so an audited use is visible in review.
- **Stylesheet comments are blanked** before spellings are matched, so
  the commented-out `was:` line above does not fire.

## 10. The rules

| Rule | Default | Fires on |
| --- | --- | --- |
| [`component-unruled`](../rules/component-unruled.md) | error | a component with no ruling, or one not `@ruling:<id>`; with Gantry, a ruling that does not exist or is not `ruled`; the same for a `retired.yaml` entry's ruling; a ruling record that does not read |
| [`component-incomplete`](../rules/component-incomplete.md) | error | an `implementations` key that is no configured platform; a platform with consumers and no entry; a value that is neither an existing file nor `n/a: <reason>`; an unknown `hostedBy`; a `hostedBy` cycle, once |
| [`retired-used`](../rules/retired-used.md) | error | a consumer read of a retired token, direct or through a wrapper; a token that aliases a retired token directly; a `retired.yaml` spelling in a consumer file |

Each reports what it checked: in text output a tally line such as
`component-unruled  5 components and retired treatments`, and in `--format
gate` a check with a `scanned` count. A rule with nothing to check says
why (`n/a (no components declared)`), and the gate's self-test checks
that each still fires on a fixture built to trip it and stays silent on
a clean one.

## Limits

- **Spellings match one line at a time.** A treatment written across two
  lines (`box-shadow:` on one, `inset 3px 0 0` on the next) is not seen.
  Write patterns for the form your code is formatted in.
- **Code-file comments are not blanked.** Only stylesheets have their
  comments blanked, so `// was: inset 3px 0 0` in a `.tsx` file is a
  match. A marker comment that repeats the spelling in a code file is
  itself a match, so it does not exempt the line below it: put the marker
  on the matched line.
- **Rulings are read, not judged.** Airframe checks that the ruling exists
  and is ruled, not that it says anything about the component.
- **Implementations are checked to exist,** not to build the component.

## In CI

`airframe lint --format gate` (the `c65llc/airframe@v0` Action with
`args: lint --format gate`) runs these rules with every other rule: a
finding fails the gate, and the JSON lists each rule's scanned count and
every honoured exemption, treatment markers included.
