# Airframe 3a · Apple emitters (Swift + asset-catalog colorsets) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `airframe build` emits, for an `apple` platform, a SwiftUI `DesignTokens` source file and asset-catalog colorsets (Display P3, light/dark), with one output set per fanned-out app. `build --check` / `emitted-stale` covers them like CSS.

**Architecture:** A shared **native plan** (`internal/emit/native.go`) splits the config's axes by role for one platform:
- the fan-out axis gives one output set per value;
- the `appearance` axis is native light/dark;
- other runtime axes are parameters of the generated type;
- other build-bound axes are fixed at their base value, with a warning.

It also resolves every token per context, and plans 3b (Android) and 3c (Windows) reuse it. Two emitters sit on top: `colorsets` and `swift`.

**Model:** Starboard (https://github.com/c65llc/starboard) for conventions, and this repo's existing emitters (`internal/emit/css.go`, `json.go`) for structure, headers, determinism and golden tests.

**Tech Stack:** Go 1.27 (`toolchain go1.27.4`), pinned deps only. Swift output targets SwiftUI on iOS 17+/macOS 14+.

**Spec:** `docs/specs/2026-11-27-airframe-design.md` §6.2 (the `apple` platform, `naming`, per-token `names`) and §6.7 item 3.

## Global Constraints

- License header on every Go file: `// SPDX-License-Identifier: AGPL-3.0-only`, then `// Copyright 2026 C65 LLC`.
- Deterministic, byte-stable output: LF, final newline, no timestamps; never range a map without sorting its keys.
- Fail closed: anything a platform cannot express is a diagnostic that names it. Never drop it silently, never guess.
- Generated files carry a header saying they are generated and naming the regenerate command, in the target language's comment syntax. JSON files can't carry a comment; colorset `Contents.json` instead sets `"info": {"author": "airframe", "version": 1}`.
- Exit codes are 0/1/2. Writes go only through the CLI's os.Root helper, which already handles every `Build` output.
- Public repo: no private-project names, paths or emails.

## Decisions this plan makes

- **Config** (Task 1):
  - `platforms.<p>.fanOut` names one loaded, build-bound axis.
  - `options` is a map, and each platform validates its own keys. For apple:
    - `gamut`: `css-color-4` (default) or `clip-p3`;
    - `accentColor`: a token name.
  - `naming` maps a format to a template using `{name}`, `{camel}` or `{Pascal}`.
  - Validation errors are `schema-invalid`: a `fanOut` that isn't a loaded build axis, an unknown apple option, a bad `gamut` value, or an unknown naming placeholder.
- **Per-token names** (Task 2): `$extensions["com.c65llc.airframe"].names` maps a format to a spelling, which wins over the naming template for that format only.
- **Axis roles** for a platform (Task 3):

  | Role | Which axis | What the emitter does |
  |---|---|---|
  | fan-out | the `fanOut` axis | one output set per value, with `{<axis>}` in an out path replaced by the value |
  | appearance | an axis named `appearance` | colours handle it in the colorset; non-colour tokens get it as a runtime parameter |
  | runtime | every other runtime axis | a parameter of the generated type |
  | fixed | other build-bound axes | resolved at their base value; when some emitted token varies on such an axis, a warning `emit-axis-fixed` |

  `emit-axis-fixed` is a new lint ID (warn) with a docs page and a self-test fixture. Its message is `platform apple: <axis> is build-bound but not fanned out; using its base value <base>`, reported once per platform and axis.
- **Which axes a token varies on** comes from the mode keys reachable through its alias chain (the JSON emitter's `modeKeys` logic, shared). A token's value in a context comes from `tokens.Set.Resolve`.
- **Colorsets** (Task 4):
  - **Location:** out `colorsets` is a catalog folder, for example `ios/{app}/Assets.xcassets/Tokens/`. The folder's `Contents.json` sets `"properties": {"provides-namespace": true}`, so colours are referenced as `"Tokens/<Name>"`.
  - **What gets a colorset:** every emitted colour token, including each shadow layer's colour. The colorset name is the token name in Pascal case, plus `-<Value>` per runtime-axis value (Pascal) when the colour varies on a runtime axis other than appearance (one colorset per combination). Shadow layers are named `<Token>Layer<N>`.
  - **Contents:** each colorset has a universal entry for the base appearance and, when the colour varies on appearance, a second entry with `appearances: [{appearance: "luminosity", value: "dark"}]`. Colours are `color-space: "display-p3"`, components are strings with 4 decimals, and alpha is included.
  - **Gamut:** CSS Color 4 mapping by default (`color.ToP3(CSSColor4)`), or `Clip` when `gamut: clip-p3`.
  - **Limits:** an appearance axis with values other than light/dark (base + `dark`) cannot be expressed and is `emit-failed`.
  - **Accent colour:** when `options.accentColor` names a colour token, `AccentColor.colorset` is also written at the catalog root, the parent of the out folder. An unknown or non-colour token is `emit-failed`.
- **Swift** (Task 5):
  - **Paths:** the out `swift` path is one shared file, or one file per fan-out value when the path contains `{<fanOut>}`. With a shared file, a non-colour token that varies on the fan-out axis is `emit-failed` ("…varies per app; put `{app}` in the swift out path").
  - **Type shape:** the file declares `public struct <Type>: Sendable`, where `<Type>` is the type part of the naming template (default `DesignTokens`). It contains:
    - one nested `public enum <Axis>: String, CaseIterable, Sendable` per runtime axis that some emitted token needs, in axis order;
    - stored properties for those axes;
    - an `init` whose defaults are each axis's base value.
  - **Colours:**
    - a colour varying only on appearance (or not at all) is `public static let <name> = Color("Tokens/<Name>")`;
    - a colour varying on another runtime axis is an instance property that switches on those axes to the right colorset name;
    - colours require `colorsets` in the same platform's `emit`, otherwise `emit-failed`.
  - **Non-colours:** a static `let` when the token doesn't vary; otherwise an instance computed property with a `switch` over the axes it varies on (appearance included, as a parameter).
  - **Types:**

    | Token type | Swift |
    |---|---|
    | dimension | `CGFloat` in points: px as is, rem × 16 |
    | number | `Double` |
    | duration | `TimeInterval` in seconds |
    | cubicBezier | `DesignTokens.Bezier(x1:y1:x2:y2:)`, a struct declared in the file |
    | fontFamily | `[String]` |
    | fontWeight | `Font.Weight` (100 → `.ultraLight`, 200 `.thin`, 300 `.light`, 400 `.regular`, 500 `.medium`, 600 `.semibold`, 700 `.bold`, 800 `.heavy`, 900 `.black`; other values snap to the nearest weight) |
    | shadow | `[DesignTokens.Shadow]`, a struct with `color: Color`, `x`, `y`, `blur`, `spread: CGFloat`, `inset: Bool` |

  - **Identifiers:** member names come from the naming template, so `{camel}` gives `accentInk`. Swift keywords are wrapped in backticks, and a leading digit gets a `_` prefix. Two tokens that map to the same identifier are `emit-failed`, naming both.
  - **Imports and header:** `import SwiftUI`, and the header comment `// GENERATED by airframe from <content>/ — do not edit. Regenerate with \`airframe build\`.`
- **Verification of Swift:** golden files, plus `TestSwiftTypechecks`. The test is skipped unless `runtime.GOOS == "darwin"` and `swiftc` is on PATH. It runs `swiftc -typecheck` on the generated file for each golden.
- **Registration:** `swift` and `colorsets` move from the "known future" set into `emit.Emitters`. `emit-unsupported` stays for `kotlin` and `xaml`.

## Review Focus

1. **A colour that varies on an axis colorsets can't express** (a third appearance, such as `high-contrast`) must be `emit-failed` naming the token and value, not a colorset that silently drops it. → Task 4, `TestColorsetUnsupportedAppearance`.
2. **Two tokens that collide after camel-casing** (`space-4` and `space4`) must be reported, not emitted twice (Swift won't compile) or overwritten. → Task 5, `TestSwiftIdentifierCollision`.
3. **An alias chain that crosses the fan-out axis** (`button-bg → {accent}`, where accent varies per app) must still give each app its own colour, and a shared Swift file must stay valid. → Task 4/5, `TestFanOutThroughAlias`.
4. **Out-of-gamut colours** (a vivid OKLCH beyond P3) must be mapped by the stated method and stay within [0,1]. `clip-p3` must clip. → Task 4, `TestColorsetGamut`.
5. **Running `build` twice, or with files read in a different order,** must give byte-identical output across every colorset and Swift file. → Task 6, `TestAppleDeterministic`.

---

### Task 1: Platform fanOut, options and naming in config

**Files:** Modify `internal/config/config.go` (+ the schema if needed); Test `internal/config/config_test.go`.

**Interfaces:**
- Produces fields on `Platform`:
  - `FanOut string`;
  - `Options map[string]any` (scalars as string, bool or float64; nested maps kept);
- and on `Config`:
  - `Naming map[string]string`;
  - `func (p Platform) Option(key string) (string, bool)`;
  - `func (c *Config) NamingFor(format string) string`, which returns `"--{name}"` for css and `"DesignTokens.{camel}"` for swift and kotlin when unset.

- [ ] **Step 1: Write failing tests:**
  - `TestPlatformFanOutParsed`: the spec §6.2 YAML gives apple `FanOut == "app"`.
  - `TestFanOutMustBeBuildAxis`: `fanOut: density` (runtime) gives schema-invalid naming the axis; `fanOut: nope` gives schema-invalid.
  - `TestAppleOptions`: `gamut: clip-p3` is accepted, and `gamut: wide` gives schema-invalid. An unknown apple option key gives schema-invalid.
  - `TestNamingTemplates`: `swift: "Tokens.{camel}"` is accepted; `"{weird}"` gives schema-invalid. `NamingFor("swift")` defaults to `DesignTokens.{camel}`.
- [ ] **Step 2:** Run `go test ./internal/config/`. Expected: FAIL.
- [ ] **Step 3:** Implement. Only apple options are validated in this plan; other platforms' options are passed through untouched (plans 3b and 3c add theirs).
- [ ] **Step 4:** Run the tests. Expected: PASS, and `TestSpec62ExampleLoads` still passes.
- [ ] **Step 5:** Commit: `feat(config): platform fanOut, options and naming`.

### Task 2: Per-token platform names

**Files:** Modify `internal/tokens/model.go`, `internal/tokens/parse.go`, `internal/schema/tokens.schema.json`; Test `internal/tokens/parse_test.go`.

**Interfaces:** Produces `Token.Names map[string]string` (format → spelling), parsed from `$extensions["com.c65llc.airframe"].names`. Values must be non-empty strings, else schema-invalid. Like the other descriptive fields, `names` on an override declaration is schema-invalid.

- [ ] **Step 1: Write failing tests:** `TestTokenNamesParsed`; `TestTokenNamesBadValue`; `TestTokenNamesOnOverrideRejected`.
- [ ] **Step 2:** Run `go test ./internal/tokens/`. Expected: FAIL.
- [ ] **Step 3:** Implement and update the schema.
- [ ] **Step 4:** Run the tests. Expected: PASS.
- [ ] **Step 5:** Commit: `feat(tokens): per-platform token names`.

### Task 3: Native plan (axis roles, per-context resolution, naming)

**Files:** Create `internal/emit/native.go`, `internal/emit/naming.go`; Test `internal/emit/native_test.go`, `internal/emit/naming_test.go`; Modify `internal/emit/json.go` (share `modeKeys` instead of duplicating it); Modify `internal/lint` (register `emit-axis-fixed`: add it to `AllIDs`, add a docs page `docs/rules/emit-axis-fixed.md`, and a self-test fixture under `internal/selftest/fixtures/flag/emit-axis-fixed/`).

**Interfaces:** Produces:
- `type NativePlan struct{ Platform config.Platform; FanOut *config.Axis; Appearance *config.Axis; Runtime []config.Axis; Fixed []config.Axis }`;
- `func PlanNative(cfg *config.Config, pl config.Platform) NativePlan`;
- `func (np NativePlan) Outputs() []Output`, with `type Output struct{ FanValue string }` (one entry with FanValue "" when there is no fan-out);
- `func (np NativePlan) Varies(s *tokens.Set, t *tokens.Token) []string`: the sorted axis names this token varies on, through aliases;
- `func (np NativePlan) Value(s *tokens.Set, name string, ctx config.Context) (*tokens.Value, error)`: resolves with the fan-out value applied and fixed axes at base;
- `func (np NativePlan) FixedWarnings(p *project.Project) []diag.Diagnostic`.

`naming.go` produces:
- `func Camel(name string) string`, `func Pascal(name string) string`;
- `func Apply(template, name string) (typ, member string)`, which splits `DesignTokens.{camel}` into the type and member parts;
- `func SwiftIdent(s string) string` (keywords in backticks, a leading digit gets a `_` prefix);
- `func NameFor(cfg *config.Config, t *tokens.Token, format string) string` (a per-token `names` entry wins).

- [ ] **Step 1: Write failing tests:**
  - `TestPlanNativeRoles`: with the spec §6.2 config, apple has FanOut app, Appearance appearance, Runtime [density] (swatch is deferred and skipped) and Fixed [channel].
  - `TestVariesThroughAlias`: `button-bg → {accent}`, where accent varies on app and appearance, gives `[app appearance]`.
  - `TestValueAppliesFanOutAndFixed`.
  - `TestFixedWarningsOnlyWhenUsed`: channel is fixed; a warning appears only when an emitted token varies on channel.
  - `TestCamelPascal`: `accent-ink` gives `accentInk` / `AccentInk`, and `space-4` gives `space4` / `Space4`.
  - `TestSwiftIdent`: `default` gives `` `default` ``, and `2x` gives `_2x`.
  - `TestNameForOverride`.
- [ ] **Step 2:** Run `go test ./internal/emit/ -run 'Native|Varies|Value|Fixed|Camel|Swift|NameFor'`. Expected: FAIL.
- [ ] **Step 3:** Implement as described in Interfaces. The JSON goldens must not change.
- [ ] **Step 4:** Run `go test ./...` (the self-test and docs tests include the new ID). Expected: PASS.
- [ ] **Step 5:** Commit: `feat(emit): native plan — axis roles, resolution and naming`.

### Task 4: Colorsets emitter

**Files:** Create `internal/emit/colorsets.go`; Test `internal/emit/colorsets_test.go`; Create `testdata/apple/` (a project with appearance, density, app (fan-out, 2 apps) and channel; about 15 tokens including colours varying on appearance, on app, through an alias, and on density; a shadow; an out-of-gamut OKLCH; `accentColor: accent`), with goldens under `testdata/apple/ios/...`.

**Interfaces:** Produces `func Colorsets(p *project.Project, pl config.Platform) (map[string][]byte, error)`, registered as `Emitters["colorsets"]`, and `func ColorsetName(np NativePlan, t *tokens.Token, ctx config.Context) string` (the colorset a colour token uses in a context, `Tokens/` namespace included), which Task 5 consumes.

- [ ] **Step 1: Write failing tests:**
  - `TestColorsetsGolden`: the output equals the committed goldens. Regenerate with `go test ./internal/emit -update` and hand-check one light/dark colorset against Xcode's documented format (`colors[].color.color-space`, `components{red,green,blue,alpha}`, `appearances[].{appearance: luminosity, value: dark}`, `info{author, version}`).
  - `TestColorsetNamespaceFolder`: the folder's `Contents.json` has `provides-namespace: true`.
  - `TestColorsetPerRuntimeValue`: a colour varying on density gives `Surface-Compact`, `Surface-Standard`, `Surface-Relaxed`.
  - `TestFanOutThroughAlias` (Review Focus 3, colour half): each app's catalog has its own `ButtonBg` value.
  - `TestColorsetGamut` (Review Focus 4): components stay within [0,1] under css-color-4, and under clip-p3 they equal clipped values.
  - `TestColorsetUnsupportedAppearance` (Review Focus 1).
  - `TestAccentColorAtCatalogRoot`, and `TestAccentColorUnknownToken` (`emit-failed`).
  - `TestShadowLayerColorsets`.
- [ ] **Step 2:** Run `go test ./internal/emit/ -run Colorset`. Expected: FAIL.
- [ ] **Step 3:** Implement as described in Interfaces.
- [ ] **Step 4:** Run the tests. Expected: PASS.
- [ ] **Step 5:** Commit: `feat(emit): asset-catalog colorsets for apple`.

### Task 5: Swift emitter

**Files:** Create `internal/emit/swift.go`; Test `internal/emit/swift_test.go`; goldens under `testdata/apple/`.

**Interfaces:** Consumes NativePlan, the naming helpers and Task 4's `ColorsetName`. Produces `func Swift(p *project.Project, pl config.Platform) (map[string][]byte, error)`, registered as `Emitters["swift"]`.

- [ ] **Step 1: Write failing tests:**
  - `TestSwiftGolden`.
  - `TestSwiftStaticColour` (appearance-only gives a `static let`), and `TestSwiftRuntimeColour` (density gives an instance `switch`).
  - `TestSwiftNonColourTypes`, covering each type-table row.
  - `TestSwiftInitDefaultsAreBase`.
  - `TestSwiftIdentifierCollision` (Review Focus 2).
  - `TestSwiftNeedsColorsets`.
  - `TestSwiftSharedFileRejectsPerAppNonColour`, and `TestSwiftPerAppPath` (`{app}` in the path gives 2 files).
  - `TestFanOutThroughAlias` (Swift half: the shared file is valid).
  - `TestSwiftTypechecks`: darwin with swiftc only, run `swiftc -typecheck` over each golden Swift file, plus a stub that uses every member.
- [ ] **Step 2:** Run `go test ./internal/emit/ -run Swift`. Expected: FAIL.
- [ ] **Step 3:** Implement as described in Interfaces.
- [ ] **Step 4:** Run the tests. Expected: PASS, with the typecheck run on this Mac.
- [ ] **Step 5:** Commit: `feat(emit): SwiftUI DesignTokens for apple`.

### Task 6: Wire-up, end to end and docs

**Files:** Modify `internal/emit/emit.go` (remove swift and colorsets from the known-future set); `cmd/airframe/testdata/script/build_apple.txtar`; `README.md` (Apple moves from planned to done); `docs/specs/2026-11-27-airframe-design.md` (§6.2 note "v0.2.0 builds css/json…" now names swift and colorsets as built; §6.7 item 3 is partly done); a short section in `docs/guides/` or the README on using the generated Swift (`DesignTokens(density: .compact).spaceGap`, `DesignTokens.accentInk`).

**Interfaces:** Consumes everything above.

- [ ] **Step 1: Write failing tests:**
  - `build_apple.txtar`: `airframe build` on `testdata/apple` writes the Swift file(s) and colorsets and lists each path; `build --check` then exits 0; editing a colour token makes `build --check` exit 1 naming the changed colorset; `lint` exits 0 apart from the expected `emit-axis-fixed` warning.
  - `TestAppleDeterministic` (Review Focus 5): build twice, and with the token files reversed, gives byte-identical output.
  - The `emit-unsupported` tests now expect only kotlin and xaml.
- [ ] **Step 2:** Run `go test ./...`. Expected: FAIL.
- [ ] **Step 3:** Wire up, and update the README, spec and docs.
- [ ] **Step 4:** Run `go vet ./... && go test ./...` and `gofmt -l .` (must print nothing). Expected: PASS.
- [ ] **Step 5:** Commit: `feat: apple platform builds Swift and colorsets`.
