# Airframe

Airframe turns a design system into a tool: design tokens as typed source files (DTCG JSON) with tiers, ownership and mode axes, a linter that enforces them, and generated outputs for each platform.

Airframe is a Go CLI. It is a sibling of Starboard (screens and flows) and Gantry (decisions, defects, evidence).

## Status

v0.4.0 is the current release: the core, the CSS importer and the Apple and Android emitters. Nothing here is stable yet.

**What v0.4.0 does**

- **Emits CSS and JSON.** `airframe build` writes CSS custom properties with one scope per mode (media queries, `:root` selectors and data attributes, as `airframe.yaml` spells them) and a neutral JSON contract of every token's resolved value per mode key. Output is byte-stable.
- **Emits Apple outputs** (since v0.3.0). The `apple` platform writes an asset catalog of Display P3 colorsets (light and dark, one catalog per app target) and a SwiftUI `DesignTokens` file naming every token as a typed member. See [Apple: Swift and asset catalogs](docs/guides/apple.md).
- **Emits Kotlin/Compose** (new in v0.4.0). The `android` platform writes a shared Jetpack Compose `DesignTokens` class naming every token as a typed member, and per app a `DesignColorValues` object of light and dark colours, with optional Material 3 colour schemes. See [Android: Kotlin and Jetpack Compose](docs/guides/android.md).
- **Lints the token source.** Tiers (a primitive aliases nothing; a semantic token reads primitives; a component token reads semantics), literals in semantic tokens, density coverage, accent groups, hue families, alias cycles and type mismatches, ambiguous or conflicting mode values, stale generated files and unformatted sources. `airframe rules` lists every rule; each has a page at `https://airframe.c65llc.com/rules/<rule>/`.
- **Checks invariants.** `invariants.yaml` declares contrast floors, reserved hues, hue stability across appearances and minimum dimensions; lint computes each in every context it applies to.
- **Formats.** `airframe fmt` rewrites config, token and invariants files in canonical form.
- **Speaks a gate protocol.** `airframe lint --format gate` prints a JSON verdict for CI gates, and `--json` prints the findings.
- **Self-tests.** Every run of the gate checks that each rule still fires on a fixture built to trip it and stays silent on a clean one; a gate whose self-test fails is not believed.
- **Imports existing CSS** (since v0.2.0). `airframe import css` turns hand-written custom properties into token files, mapping each block to a mode key through the `web:` spellings in `airframe.yaml`, and proves that the CSS `airframe build` generates from them resolves every property to the same value in every environment. See [Importing existing CSS](docs/guides/importing.md).

**Planned (not in v0.4.0)**

- Native emitter: C#/XAML. `airframe.yaml` already accepts `xaml`; nothing is built for it yet, and Airframe says so (`emit-unsupported`, a warning).
- Consumer rules: reading tokens where they are used (every platform's code, not just the token files) to enforce tiers and ownership at the consumer.
- Components and a reference surface for asking which token to use and whether a file may read it.

These land in later plans (3 to 8); until then, no part of this README describes them as working.

## Install

Download the archive for your platform from the repository's Releases page and put `airframe` on your `PATH`, or build it with Go:

```sh
go install github.com/c65llc/airframe/cmd/airframe@latest
```

## Quick start

```sh
airframe init      # write a starter project (airframe.yaml + design-system/) and build it
airframe lint      # check it; exit 1 on findings
airframe build     # regenerate the CSS and JSON outputs
airframe fmt       # rewrite the sources in canonical form
airframe import css theme.css   # adopt an existing stylesheet's custom properties as tokens
```

`airframe help` lists every command. Exit status is 0 when clean, 1 on findings or drift, 2 on a usage or I/O error.

### Mode axes and the base value

Every axis in `airframe.yaml` has one **base** value: the one a token's `$value` is, rendered when no attribute is set. It is the value whose only web selector is bare `:root`, else the axis's `base:`:

```yaml
modes:
  appearance: { values: [light, dark], web: { light: ":root", dark: ":root[data-theme='dark']" } }
  density:    { values: [compact, standard, relaxed], base: standard, web: { attr: data-density } }
```

A mode key never names a base value (`mode-base-value`), and lint checks the base context, the render with no attribute set, like every other.

## GitHub Action

```yaml
- uses: c65llc/airframe@v0
  with:
    version: v0.4.0
    args: lint --format gate
```

The action downloads that release's binary for the runner, verifies it against the release checksums and runs it.

## License

AGPL-3.0-only. Copyright 2026 C65 LLC. See [LICENSE](LICENSE).
