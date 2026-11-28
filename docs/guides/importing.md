# Importing existing CSS

Importing means turning a theme you already ship, hand-written CSS custom
properties, into Airframe token files, and proving the switch changes
nothing. `airframe import css` reads your stylesheets, maps each block to a
mode key, writes the token files, and then checks that the CSS
`airframe build` would generate from them resolves every custom property to
the same value as your stylesheets do, in every browser environment they
can tell apart. If either step finds an error, nothing is written.

This guide is for teams with a CSS theme (`:root { --surface: …; }` plus
dark-mode and density overrides) who want Airframe to own it from now on.

## 1. Describe your modes

Import runs inside a project: it needs an `airframe.yaml`, and it never
invents a mode. Each axis's `web:` spellings are how the importer recognises
a block, so they must match the selectors and media queries your CSS
already uses. For a theme with a dark mode (by system preference unless a
`data-theme` attribute says otherwise), a density attribute that coarse
pointers default to `relaxed`, and a per-app accent:

```yaml
version: 1
content: design-system
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ["@media (prefers-color-scheme: dark) :root:not([data-theme='light'])", ":root[data-theme='dark']"]}}
  density: {values: [compact, standard, relaxed], base: standard, binding: runtime, web: {attr: data-density, also: {relaxed: "@media (pointer: coarse)"}}}
  app: {values: [drafts, slides], base: drafts, binding: runtime, web: {attr: data-app}}
platforms:
  web: {emit: css}
```

Write the file by hand, or start from `airframe init` and then delete the
`*.tokens.json` files it wrote under `design-system/`: the import writes
its own, and the equivalence check does not see others (see Limitations).
Every axis needs a `web:` spelling, and a platform must emit `css`.

## 2. Import

The repository's `testdata/import` directory holds this config and a
`theme.css` to try it on. Check the import first, writing nothing:

```bash
airframe import css --dry-run theme.css
```

```text
imported 25 tokens from 38 declarations (1 non-custom ignored); equivalent in 192 environments across 25 properties
would write design-system/tokens/primitive.tokens.json
would write design-system/tokens/semantic.tokens.json
```

The summary says what was read (`color: var(--ink)` is not a custom
property, so it is counted and ignored) and what was proved: 192
combinations of `data-theme`, `data-density`, `data-app`,
`prefers-color-scheme` and `pointer`, each property compared in each. Then
import for real:

```bash
airframe import css theme.css
```

```text
imported 25 tokens from 38 declarations (1 non-custom ignored); equivalent in 192 environments across 25 properties
wrote design-system/tokens/primitive.tokens.json
wrote design-system/tokens/semantic.tokens.json
```

Each block became a mode key: `:root[data-density='compact']` is
`density:compact`, and both `:root[data-theme='dark']` and the
`prefers-color-scheme` block are `appearance:dark`. A comment right before
a `:root` declaration becomes its `$description`. Pass several files to
import them together; later files win the cascade, as in the browser.

The import refuses to overwrite either token file (exit 2) unless you pass
`--force`. Exit status is 0 when it wrote the files (warnings allowed), 1
when it found an error, and 2 for a usage or I/O error.

## 3. Fix what it reports

Each finding names the file and line. Every ID has a page at
`https://airframe.c65llc.com/rules/<id>/`, and `airframe explain <id>`
prints it.

| Finding | Means | Usual fix |
| --- | --- | --- |
| [`import-css-syntax`](../rules/import-css-syntax.md) | The CSS is malformed, or nested | Close the block, string or comment; un-nest rules |
| [`import-unsupported-at-rule`](../rules/import-unsupported-at-rule.md) | `@layer`, `@supports`, `@import`… | Move the properties to the top level or an `@media` block |
| [`import-unsupported-selector`](../rules/import-unsupported-selector.md) | Not `:root` plus attribute conditions | Theme the root: `:root[data-theme='dark']`, not `.dark` |
| [`import-unsupported-media`](../rules/import-unsupported-media.md) | A range feature, `not`, `or`, a media type | Keep breakpoints out of the token import |
| [`import-unmapped-scope`](../rules/import-unmapped-scope.md) | No mode key is spelled that way | Add the value, or fix the `web:` spelling, in `airframe.yaml` |
| [`import-unsupported-value`](../rules/import-unsupported-value.md) | `calc()`, a keyword, `!important`, a dangling `var()` | Write a literal, or alias a token |
| [`import-name-invalid`](../rules/import-name-invalid.md) | Not lowercase words joined by `-` | Rename the property and its readers |
| [`import-type-conflict`](../rules/import-type-conflict.md) | A colour in one mode, a length in another | Split it into two properties |
| [`import-no-base`](../rules/import-no-base.md) | Set only under a mode, never on bare `:root` | Give it a base value on `:root` |
| [`import-duplicate`](../rules/import-duplicate.md) (warn) | Declared twice for one mode | Delete the losing declaration |
| [`import-fallback-dropped`](../rules/import-fallback-dropped.md) (warn) | `var(--x, fallback)` | Drop the fallback; the alias alone is imported |
| [`import-not-equivalent`](../rules/import-not-equivalent.md) | The generated CSS differs somewhere | Make the config's spellings match what the CSS does |
| [`import-too-many-environments`](../rules/import-too-many-environments.md) | Over 200,000 environments to check | Import fewer files, or fewer unrelated attributes |
| [`import-nothing`](../rules/import-nothing.md) | Nothing to import, or nothing to check against | Pass the theme's files; add a platform that emits css |

## Limitations

- **Types come from values, and two from names.** A word list is a
  `fontFamily` only when the property name ends in `-font`, `-family` or
  `font-family`; `400` is a `fontWeight` only when the name contains
  `weight`. Otherwise `400` is a number and `bold` is an unsupported value.
- **`html` is rejected.** It matches the root element but with lower
  specificity than `:root`; spell it `:root`.
- **Only discrete media features**, joined by `and`: `prefers-color-scheme`,
  `prefers-contrast`, `pointer`, `hover`, `forced-colors`, `display-mode`
  and the like. No `min-width`, `not`, `or` or media types.
- **`!important` is rejected.** Airframe never emits it.
- **The check covers the imported files only.** It compares your
  stylesheets with the CSS generated from the two files the import writes,
  not with other token files already in the project. Import into a project
  without its own token files, or check the combined result yourself.
- **Tiers are a first guess.** A token whose values are all literals is
  `primitive`; one with any `var()` is `semantic`; none is `component`.

## After the import

1. **Assign tiers.** Read both files and set each token's tier to what it
   really is: a token one component owns is `component`, and a semantic
   token should read primitives rather than hold literals. Move tokens
   between files as you like; the file a token is in does not set its tier.
2. **Build.** `airframe build` writes the generated CSS; replace your
   hand-written stylesheet with it.
3. **Lint.** `airframe lint` now holds the tiers, aliases and modes; fix
   what it reports, and add `airframe lint --format gate` and
   `airframe build --check` to CI.
