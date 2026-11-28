# Airframe — design

- **Status:** v0.1.2 implements the core (§6.1–6.3, the source and invariant rules in §6.4, and `init`, `fmt`, `lint`, `build`, `show`, `explain`, `rules`, `schema` from §6.5) with CSS and JSON emitters; v0.2.0 adds `import css` (§6.7 item 2); v0.3.0 adds the Apple emitters, v0.4.0 the Android emitter and v0.5.0 the Windows emitter (§6.7 item 3, done); v0.6.0 adds the consumer rules with the wrapper map (§6.4, §6.7 item 4, done); the rest is planned.
- **Family:** Starboard (screens and flows) · **Airframe** (design systems) · Gantry (decisions, defects, evidence)
- **License:** AGPL-3.0-only, © 2026 C65 LLC

---

## 1 · Intent

A cross-platform design system is three things:

1. a set of named values (tokens);
2. rules for who may read which values (tiers and ownership);
3. a translation of those values into each platform's native form.

Teams usually build all three as one repository's scripts: tokens hand-authored in CSS, a generator that asserts a few invariants, an emitter per platform, and a handful of grep gates, each added after something shipped wrong. The scripts work, but they enforce only what someone remembered to grep for, and they do not travel to the next project.

**Airframe makes that a tool:** a design system as typed source files, with tiers and ownership enforced at the *consumer* (every platform's code, not just the token files), emitters for each platform, and a reference surface where people and agents can ask "which token do I use here, and may this file read it?"

The failures it exists to prevent are generic:

- a component measurement read by other components becomes a de facto foundation, and can no longer change without breaking them;
- an accent ramp drifts between light and dark because nothing asserts that both themes show one accent;
- a value restated as a literal fallback silently diverges from its token;
- a treatment that was retired comes back, because there is no token to delete and nothing remembers its spelling.

## 2 · Goals, non-goals, anti-goals

**Goals**

1. One source of truth for tokens, with tiers, appearances, densities and accent groups modelled explicitly.
2. Generated native outputs per platform: CSS, Swift, Kotlin/Compose, C#/XAML resources, and a neutral JSON contract. CI fails on drift.
3. Consumer-side enforcement across languages: tier discipline, component-token ownership, no undeclared references, no restated values, no retired treatments.
4. Declared design invariants checked on every build: contrast pairs, reserved hues, an accent hue that never flips between appearances, minimum target sizes.
5. A component inventory: each component's owned tokens, its consumers, its per-platform implementation, and the Gantry ruling it answers to.
6. A reference surface: browse, search and diff tokens with swatches, in a PWA, a desktop app, VS Code and over MCP.

**Non-goals**

- **Rulings.** What a component *should* be, and why, belongs to Gantry. Airframe links to `@ruling:<id>` and never restates it.
- **A component library.** Airframe inventories components and points at their code; it ships no UI code.
- **Figma sync in v1.** DTCG source files make an importer or exporter straightforward later.
- **Screens, flows, screenshots.** Those are Starboard's and Gantry's.

**Anti-goals**

- **A clean gate that scanned nothing.** Every rule self-tests, fails closed on an empty scan and reports what it examined, by name.
- **An alias layer that grows a generation per release.** Renames ship with a codemod, never with compatibility aliases.
- **Pixel parity across platforms.** Colour resolves by *role*; each platform maps roles onto its own conventions.

## 3 · Boundaries

| Airframe owns | Airframe references |
|---|---|
| tokens and tiers, appearances, densities, accent groups | `@ruling:<id>` (Gantry) for each component |
| the component inventory (owned tokens, owners, platform implementations) | `@screen:<id>` (Starboard), optionally, for where a component appears |
| emitters and emitted artifacts | |
| token gates (tier, ownership, dangling, fallback, retired, invariants) | |

"A component with no ruling is not in the system" is checked by Airframe's `component-unruled` rule, which resolves `ruling:` against Gantry when a `gantry.yaml` is present.

The three tools share conventions: lint JSON, a documentation page per rule, `fmt`, a GitHub Action, `skills install`, and `@kind:id` references.

## 4 · Constraints

- **Go core:** a native CLI, plus a wasm build for the UIs.
- **Deterministic output:** emitted files are byte-stable and carry a "generated, do not edit" header naming the command that regenerates them.
- **Colour in OKLCH,** converted to sRGB and Display P3 for native emitters. The gamut-mapping method is stated, never implicit (CSS Color 4 mapping by default).
- **No runtime dependency** in consuming apps: emitted files are plain source.
- **License:** AGPL-3.0-only, © 2026 C65 LLC, matching Starboard and Gantry.

## 5 · Approaches considered

**Source of truth for tokens**

| Option | For | Against |
|---|---|---|
| **A. DTCG JSON as source, CSS emitted (chosen)** | A W3C community standard (first stable version 2025); typed; Figma and Tokens Studio interop later at little cost; CSS becomes one output among several | Teams with hand-written CSS tokens convert once (`airframe import css`) |
| B. CSS custom properties with structured comments | No migration for web teams | The comment grammar *is* the schema; every team writes CSS-with-annotations; favours web-first over native-first teams |
| C. YAML with Airframe's own schema | As typed as A | No ecosystem; rejected |

Airframe-specific facts live under `$extensions["com.c65llc.airframe"]`: tier, owning component, density values, retirement, emit targets.

**Consumer scanning.** Regex adapters per platform, keyed off the emitted names, which are deterministic: `var(--x)` in CSS, `DesignTokens.x` in Swift and Kotlin, `{StaticResource X}` / `{ThemeResource X}` in XAML. Per-language parsers are not worth the dependency.

**Hand-written wrappers.** Native code often reads tokens through hand-written layers over the generated files (extension properties, composition locals). So `airframe.yaml` takes a declared **wrapper map** per platform: a symbol pattern mapped to the token it reads, plus explicit entries for irregular names. Consumer rules count a read through a wrapper exactly like a direct one. A wrapper symbol the map does not cover is reported as `wrapper-unmapped`, never silently skipped. Reads are always declared, never inferred.

## 6 · The design

### 6.1 Layout

```
airframe.yaml
design-system/                         # content root, configurable
  tokens/primitive.tokens.json         # tier 1: raw ramps and hues
  tokens/semantic.tokens.json          # tier 2: roles (surfaces, ink, edges, accent group)
  tokens/component.tokens.json         # tier 3: one component's box
  themes/{light,dark}.tokens.json      # appearance overrides (or $extensions modes)
  brands/<accent-group>.tokens.json    # per-app or per-brand accents
  components/<component-id>.yaml       # the inventory (planned)
  invariants.yaml                      # declared design invariants
  retired.yaml                         # retired tokens and treatments (planned)
  generated/                           # emitted, never hand-edited
```

Outputs go wherever each platform's `out:` says (§6.2).

### 6.2 `airframe.yaml`

```yaml
version: 1
content: design-system
tiers:
  primitive: { readableBy: [semantic] }
  semantic:  { readableBy: [component, consumers, emitters] }
  component: { readableBy: [owner] }          # the owning component only, unless `shared: true`
modes:                    # every axis a token value may vary on
  appearance: { values: [light, dark], binding: runtime,  web: { light: ":root", dark: ["@media (prefers-color-scheme: dark) :root:not([data-theme='light'])", ":root[data-theme='dark']"] } }
  density:    { values: [compact, standard, relaxed], base: standard, binding: runtime, web: { attr: data-density, also: { relaxed: "@media (pointer: coarse)" } } }
  app:        { values: [drafts, edit, review, slides, tasks, wiki], base: drafts, binding: build, web: { attr: data-app }, group: accent }   # per-app accents
  swatch:     { values: from brands/swatches.tokens.json, binding: runtime, web: { attr: data-accent-swatch }, group: accent }  # user-chosen, retints live
  channel:    { values: [stable, next], base: stable, binding: build, web: { attr: data-channel } }   # a contextual override
# Every loaded axis has one BASE value: the one `$value` is, rendered when no attribute is set. It is the
# value whose only web selector is bare `:root` (appearance: light), else the axis's `base:`. A mode key
# never names a base value, and lint checks the base context (the unset render) like any other.
accentGroups: { required: [accent, accent-ink, accent-contrast, accent-on-ink, accent-soft] }   # any mode with `group: accent` must set all five
platforms:
  web:     { emit: css, out: dist/web/tokens.css, consumers: ["web/**/*.{css,tsx}"], externalProperties: ["--radix-*", "--font-*"] }   # a var() of a library's property is foreign, not undeclared
  apple:                   # emitters may fan out per build-bound mode value
    emit: [swift, colorsets]             # a shared .swift + one asset-catalog colorset per colour token (P3, light/dark)
    out: { swift: ios/Shared/DesignTokens.swift, colorsets: "ios/{app}/Assets.xcassets/Tokens/" }
    fanOut: app                          # one colorset set per app target, including AccentColor
    consumers: ["ios/**/*.swift"]
    wrappers:                            # hand-written layers over the generated code; a read through one counts like a direct read
      - root: Theme                      # `Theme.<member>`: members are the platform's generated names
        generated: true
      - root: AppTheme.spacing           # irregular names: map first, then patterns in order
        map: { small: gap, hairlineStrong: hairline }
        patterns: [ { match: '^(accent|ink|surface)Tone$', token: '{1}' } ]
  android:
    emit: [kotlin]
    out: { kotlin: "android/{app}/src/main/kotlin/DesignColorValues.kt" }   # out keys are emit formats
    fanOut: app
    options:                             # package is required; sharedOut is the shared DesignTokens file's path
      package: com.example.design
      sharedOut: android/shared/src/main/kotlin/DesignTokens.kt
      material3Scheme: { primary: accent, onPrimary: accent-contrast, primaryContainer: accent-soft }   # maps Material 3 ColorScheme roles to tokens; emits a light and dark scheme per appearance
    consumers: ["android/**/*.kt"]
  windows:
    emit: [xaml]
    out: "windows/{app}/DesignTokens.xaml"
    fanOut: app
    options: { gamut: clip-srgb }        # clip to sRGB; the default is CSS Color 4 gamut mapping
    consumers: ["windows/**/*.{xaml,cs}"]
  json:
    emit: [json]
    out: dist/tokens.json
naming: { css: "--{name}", swift: "DesignTokens.{camel}", kotlin: "DesignTokens.{camel}", xaml: "Design{Pascal}" }
# A token may override its spelling on one platform (`$extensions…names.swift: "hairline"`). That is
# ONE token with a platform spelling, not a second readable name, so the no-alias rule holds. Until their
# emitters read naming, css, json and colorsets accept only their default spelling (anything else is schema-invalid).
lint:
  rules: { fallback-literal: error, contrast-below: error }
```

Every format `airframe.yaml` accepts is built: v0.5.0 builds the `css`, `json`, `swift`, `colorsets`, `kotlin` and `xaml` (WinUI 3 resource dictionaries) formats. With nothing left unbuilt, `emit-unsupported` is retired (v0.5.0); its rule page stays.

### 6.3 Records

**Token** (DTCG with extensions):

```json
{ "tabstrip": { "h": {
  "$type": "dimension",
  "$value": { "value": 36, "unit": "px" },
  "$extensions": { "com.c65llc.airframe": {
    "tier": "component", "component": "tabstrip",
    "density": { "compact": 32, "standard": 36, "relaxed": 40 },
    "emit": ["css", "swift", "kotlin", "xaml"] } } } } }
```

**Component** (`components/tabstrip.yaml`). The loader is built: `id`, `tokens`, `owners` and `shared` are checked against the token files, and the consumer rules read `owners`. `noun`, `ruling`, `hostedBy` and `implementations` are accepted and type-checked; their rules (the inventory, `component-unruled`) land in §6.7 step 5:

```yaml
id: tabstrip
noun: Tab strip
ruling: "@ruling:tab-strip"          # Gantry; required (component-unruled)
tokens: [tabstrip-h, tabstrip-max-w, tabstrip-gap]   # owned tier-3 tokens
owners:                              # files allowed to read the owned tokens
  - web/src/tabstrip/**
  - ios/**/TabStrip*.swift
hostedBy: null                       # or a container component id
implementations:                     # where each platform builds it; "n/a: reason" allowed
  web: web/src/tabstrip/TabStrip.tsx
  apple: ios/Shared/TabStrip.swift
  android: "n/a: phone uses @component:mobile-tab-bar"
  windows: windows/Shared/TabStrip.xaml
shared: false
```

**Invariant** (`invariants.yaml`, implemented). Kinds are generic:

```yaml
- id: accent-hue-stable
  kind: same-hue-across-appearances      # the accent hue never flips between appearances
  tokens: ["accent", "accent-ink"]
  forEach: accentGroup
- id: danger-hue-reserved
  kind: hue-reserved
  hue: 25
  tolerance: 8
  only: ["danger", "danger-*"]
- id: selection-text-contrast
  kind: contrast
  fg: accent-ink
  bg: accent-soft
  min: 4.5
  forEach: [accentGroup, appearance]
- id: touch-floor
  kind: min-dimension
  match: "*-target*"
  min: 44
  densities: all
```

**Retired** (`retired.yaml`, planned). Treatments with no token to delete are held by their spellings on every platform:

```yaml
- id: flush-left-selection-bar
  ruling: "@ruling:selection-treatment"
  spellings:
    css: ["inset 3px 0 0", "border-left: 3px solid var\\(--accent"]
    swift: ["\\.frame\\(width: 3\\).*accent"]
  marker: audited-not-retired-treatment     # escape marker; requires a reason
```

### 6.4 Lint rules

| Group | Rules | Status |
|---|---|---|
| Source | `schema-invalid`, `token-unclassified`, `tier-violation`, `literal-in-semantic`, `density-missing`, `accent-group-partial`, `prefix-multiple-hues`, `alias-cycle`, `alias-unresolved`, `alias-type-mismatch`, `mode-ambiguous`, `mode-override-conflict`, `mode-base-value`, `emitted-stale`, `not-formatted` | v0.1.2 |
| Invariant | `contrast-below`, `hue-reserved`, `accent-flip`, `dimension-below` | v0.1.2 |
| Consumer | `reference-undeclared`, `token-owner-violation`, `primitive-read-by-consumer`, `fallback-literal`, `wrapper-unmapped` | v0.6.0 (see `docs/guides/consumers.md`) |
| Consumer | `retired-used`, `component-unruled`, `component-incomplete` | planned (§6.7 step 5) |

- **Tiers:** a primitive aliases nothing; a semantic token reads primitives (never literals); a component token reads semantics and is read only by its owner.
- **Invariant findings** name the invariant ID, the token pair, the mode context and the measured value. Each is computed in every context it applies to, including the base context.
- **Consumer rules** scan each platform's `consumers` globs. Owners are declared, never inferred: inferred ownership produces more false positives than findings. `fallback-literal` catches `var(--x, 24px)` restating a token's value.
- Every rule reports the files scanned and references examined, and fails closed on an empty scan. An exemption uses a per-rule marker and requires a reason (`exemption-without-reason`).
- `airframe rules` lists every rule; each has a page at `https://airframe.c65llc.com/rules/<rule>/`.

**Gate protocol.** `airframe lint --format gate` emits a JSON verdict with one check per rule, scanned counts, exemptions and a self-test: each rule must still fire on a fixture built to trip it and stay silent on a clean one, or the gate is not believed. In a repository that also uses Gantry, Airframe runs as one gate in Gantry's sweep, and a defect can owe a specific Airframe rule.

### 6.5 CLI

```
airframe init [--profile P]                       # v0.1.2
airframe fmt [--check]                            # v0.1.2
airframe lint [--json|--format text|json|github|gate] [--strict]   # v0.1.2
airframe build [--check] [--platform <p>]...      # v0.1.2; --check = drift gate
airframe show <token> [--json]                    # v0.1.2; value per mode key, tier, alias chain
airframe explain <rule|token>                     # v0.1.2
airframe rules [--json] | schema <kind> | version # v0.1.2
airframe import css <files…> [--dry-run] [--force] # v0.2.0; adopt existing custom properties, equivalence-checked
airframe find <role words>                        # planned: "which token for muted text on a sheet"
airframe usages <token>                           # planned: every consumer reference across platforms
airframe diff <ref-a> <ref-b> [--format md|json]  # planned: token changes between git refs, with swatches
airframe rename <old> <new>                       # planned: codemod across all consumer platforms
airframe retire <token|treatment>                 # planned
airframe specimen [--out dir]                     # planned: swatch, type and spacing sheets as SVG
airframe skills … | mcp | migrate                 # planned
```

Exit status: 0 clean, 1 findings or drift, 2 usage or I/O error.

### 6.6 Surfaces (planned, except the Action)

All surfaces share the Go core through its wasm build.

- **PWA:** a tier browser with appearances side by side, density columns and an accent-group switcher; a contrast matrix of declared pairs with measured ratios; the component inventory with platform coverage and ruling links; usages per platform; a diff between two refs with swatches.
- **Tauri desktop app:** the same bundle with native filesystem access, plus a colour picker that writes OKLCH and warns about gamut per platform.
- **VS Code:** hover on any token reference (`var(--x)`, `DesignTokens.x`, a XAML key) showing value per appearance and density with a swatch, tier, owner and ruling; colour decorators; **tier-aware completion** that offers only tokens this file may read, so ownership is taught at the keystroke rather than in CI; diagnostics; a quick fix replacing a literal with the token whose value it matches.
- **CI:** the `c65llc/airframe@v0` Action runs `lint --format gate` and `build --check` (v0.1.2). Planned: `diff --format md` written to the job summary on PRs that touch tokens, so reviewers see swatches, not hex.
- **AI skills:** `airframe` (before styling anything: find the role token, check this file may read it), `airframe-add-token` (tier, density values, emit targets), `airframe-component` (inventory entry and ruling reference), `airframe-adopt`.
- **MCP (`airframe mcp`):** reads `airframe_find`, `airframe_show`, `airframe_usages`, `airframe_can_read` (file and token: allowed, and why), `airframe_lint`, `airframe_diff`, `airframe_specimen` (SVG/PNG so a model can see the palette); writes `airframe_token_set` and `airframe_component_set`, each going through edit, `fmt`, `build --check`.

### 6.7 Roadmap

1. **Core** (v0.1.2): DTCG parsing, modes, source and invariant lint, `fmt`, CSS and JSON `build`, the gate protocol with self-test, the GitHub Action.
2. **Importer** (v0.2.0): `airframe import css`, with a semantic-equivalence check: for every mode combination, each custom property resolves to the same value in the old and generated CSS.
3. **Native emitters** (done: Apple, Android and Windows, each with its guide): Swift and asset-catalog colorsets (done: Apple, with per-app fan-out and gamut handling; see `docs/guides/apple.md`), Kotlin/Compose with an optional Material 3 scheme (done: Android, with per-app colour values; see `docs/guides/android.md`), and WinUI 3 XAML resource dictionaries with theme dictionaries and high-contrast system colours (done: Windows, with per-app dictionaries; see `docs/guides/windows.md`).
4. **Consumer rules,** with the wrapper map (done, v0.6.0: `reference-undeclared`, `token-owner-violation`, `primitive-read-by-consumer`, `fallback-literal` and `wrapper-unmapped`, with the component loader; see `docs/guides/consumers.md`).
5. **Components and retired treatments:** the inventory, `component-unruled`, `retired-used`.
6. **Reference commands:** `find`, `usages`, `diff`, `rename`, `retire`, `specimen`.
7. **Skills and MCP.**
8. **PWA**, then **VS Code**, then **Tauri.**

All of it is in scope for 1.0.

### 6.8 Prior art

- **Declare** (davidtemkin/declarelang): findings carry a code and an applicable rewrite, and `airframe explain <rule|token>` follows the same idea. Declare's principle that every dependency must be *visible* to the compiler is the one behind declared owners and the wrapper map: never infer a read.
- No Declare theme emitter in v1. It is an easy emitter to add once someone needs it.

## 7 · Left to the implementer

- The gamut-mapping algorithm: state it; CSS Color 4's is the default.
- `find`'s ranking: token descriptions plus role-name matching are enough; no embeddings required.
- The codemod engine for `rename`: textual, driven by the naming table, per platform.
