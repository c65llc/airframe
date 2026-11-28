# Consumers: checking where tokens are read

The token files say what each token is and who may read it. The consumer
rules check the other side: the app code that reads them. `airframe lint`
scans each platform's **consumer files** (stylesheets, components, Swift,
Kotlin, XAML and C#) and checks every token reference, whether it is
written directly (`var(--ink)`, `DesignTokens.ink`, `{ThemeResource
DesignInk}`) or goes through a hand-written wrapper you declare
(`AppTheme.spacing.small`). It reports:

- a reference to a token that does not exist;
- a read of a primitive, which consumers may not read;
- a read of a component's token from a file outside that component's
  owners;
- a CSS fallback that is a literal value;
- a wrapper read that maps to no token.

The checks are regex adapters keyed off the names the emitters write, so
a reference and the generated code cannot drift: the scanner asks each
emitter for the names it would generate. They run in every lint output
format, the gate included, and each reports what it scanned.

This guide uses the token set from the repository's
`internal/selftest/fixtures/clean-consumers` fixture: semantic `surface`,
`ink`, `accent`, `gap` and `hairline`; primitives `gray-50`, `gray-900`,
`blue-500`, `space-2`, `space-4` and `line-1`; and the `button`
component's `button-bg` and `button-ink`.

## 1. Declare consumers

Each platform lists its consumer files as repo-relative globs (the
project root is the directory holding `airframe.yaml`):

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
    externalProperties: ["--radix-*", "--font-*"]
  apple:
    emit: [swift, colorsets]
    out: {swift: apple/Generated/DesignTokens.swift, colorsets: apple/Generated/Assets.xcassets/Tokens/}
    consumers: ["apple/Sources/**/*.swift"]
    wrappers:
      - root: AppTheme.spacing
        map: {small: gap, hairline: hairline}
        patterns: [{match: '^(accent|ink|surface)Tone$', token: '{1}'}]
  android:
    emit: [kotlin]
    out: {kotlin: android/generated/DesignTokens.kt}
    options: {package: com.example.design}
    consumers: ["android/src/**/*.kt"]
    wrappers:
      - root: LocalTokens.current
        generated: true
  windows:
    emit: [xaml]
    out: {xaml: windows/Generated/DesignTokens.xaml}
    consumers: ["windows/App/**/*.{xaml,cs}"]
    wrappers:
      - root: Palette
        map: {Accent: accent, Ink: ink, Surface: surface}
```

- **Generated outputs and the content root are never scanned,** even when
  a glob matches them: every platform's `out` paths (and Android's
  `sharedOut`) and everything under `content`.
- **Every glob must match a file.** A glob that matches nothing (a typo
  such as `*.tssx`) is a `reference-undeclared` finding at `airframe.yaml`
  naming the platform and the glob: a declared input that is never
  scanned is a failure, not a pass. Each dead glob is reported on its own.
- **Symlinks are not followed.** A matched symlink is not scanned, and
  lint says so (`schema-invalid`, listing the links), so a link cannot
  quietly widen or narrow the scan.
- **Every matched file needs an adapter** (below) whose format the
  platform emits. A `.swift` file in a platform that does not emit
  `swift`, or a `.rb` file anywhere, is one `schema-invalid` finding per
  platform at `platforms.<p>.consumers`, listing the files.

## 2. What each adapter matches

The adapter is chosen by the file's extension:

| Adapter | Extensions | Matches |
| --- | --- | --- |
| css | `.css .scss .sass .less .html .htm .vue .svelte .js .jsx .mjs .cjs .ts .tsx` | `var(--name)`, and an optional `, fallback`, read with balanced parentheses to the matching `)` |
| swift | `.swift` | `DesignTokens.<member>` (the naming template's type part), backticked members included |
| kotlin | `.kt .kts` | `DesignTokens.<member>`, and `DesignColorValues.<appearance>.<slot>` |
| xaml | `.xaml` | `{StaticResource Key}` and `{ThemeResource Key}` |
| xaml-in-C# | `.cs` | `Resources["Key"]` |

The names come from the platform's `naming` (the defaults are `--{name}`,
`DesignTokens.{camel}` and `Design{Pascal}`) and from each emitter's own
naming code, runtime suffixes (`DesignGapCompact`) and colour slots
included.

**In a stylesheet, comments are blanked.** In `.css`, `.scss`, `.sass` and
`.less` files, each `/* … */` comment is blanked before matching, so a
commented-out `var(--old)` is not a reference. Exemption markers (below)
are still read from comments. In every other file type the adapters see
strings and comments too; see [Limits](#limits).

### App-local CSS properties

A custom property your own CSS defines is not a token read:

```css
/* web/src/app.css */
:root {
  --sidebar-w: 240px;
}
.app {
  width: var(--sidebar-w);
  color: var(--ink);
  background: var(--surface);
  margin: var(--gap, var(--hairline));
}
```

`--sidebar-w` is defined (`--sidebar-w:`) in a consumer file of the same
platform, so `var(--sidebar-w)` is app-local. So is a property a script
in a consumer file of the platform sets, as `setProperty("--sheet-h", …)`
or a computed key `{ ["--sheet-h"]: h }` (either quote). A name that is neither a
token nor defined anywhere in the platform's consumer files is a typo,
and `reference-undeclared`:

```css
/* web/src/legacy.css */
.banner {
  color: var(--accnet);
  background: var(--gray-50);
  padding: var(--gap, 8px);
}
```

```text
web/src/legacy.css:3:14: error reference-undeclared: --accnet is not a token, and no web consumer file defines it
```

### Third-party CSS properties

A library defines custom properties your CSS reads but no consumer file
defines: Radix Themes' `--radix-accent-9`, the `--font-inter` that
`next/font` sets. Declare them per platform with `externalProperties`, as
the web platform above does: a list of globs over the property name, with
`path.Match` semantics (`*` matches any run of characters). Each entry
must start with `--`, and only a platform that emits css may have the
list.

```tsx
// web/src/sheet.tsx
export const Sheet = ({ el, h }: { el: HTMLElement; h: string }) => {
  el.style.setProperty("--sheet-h", h);
  return <div style={{ ["--sheet-w"]: "320px", height: "var(--sheet-h)", width: "var(--sheet-w)", color: "var(--radix-accent-9)", fontFamily: "var(--font-inter, sans-serif)" }} />;
};
```

A `var()` of a matching property is foreign: it is never
`reference-undeclared`, and its fallback is not checked against a token,
so `var(--font-inter, sans-serif)` is no `fallback-literal`. The list is
declared, never inferred: a prefix it does not name is still a typo.
`--sheet-h` and `--sheet-w` are app-local, set by the script itself.

### Which native names are "ours"

Swift, Kotlin and XAML code is full of symbols Airframe did not generate
(`{ThemeResource SystemControlForegroundBaseHighBrush}`). A matched name
the emitter does not write is a typo only if it is **ours**: it fits the
naming template's literal prefix and suffix. For the XAML default
`Design{Pascal}` that means it starts with `Design`; a template suffix
may be followed by the runtime and shadow-part tails the emitters
append. Any other name is ignored. Generated symbols that stand for no
token (`DesignTokens.Density.compact`, nested types, init labels,
`Colors`) are ignored too.

```xml
<!-- windows/App/MainPage.xaml -->
<Page x:Class="App.MainPage">
  <Grid Background="{ThemeResource DesignSurface}" Padding="{StaticResource DesignGapCompact}">
    <TextBlock Foreground="{ThemeResource SystemControlForegroundBaseHighBrush}" />
    <TextBlock Foreground="{ThemeResource DesignAccnet}" />
  </Grid>
</Page>
```

```text
windows/App/MainPage.xaml:5:28: error reference-undeclared: {ThemeResource DesignAccnet}: DesignAccnet is not a name the xaml emitter writes for platform windows
```

## 3. Wrappers

Native code often reads tokens through a hand-written layer over the
generated file: a theme struct, a composition local, a C# helper. The
scanner cannot see through it, so you declare it. Each entry under
`platforms.<p>.wrappers` names one **root**, one or more dotted
identifiers; `<root>.<member>` in any consumer file of that platform is a
token read, counted by every rule exactly like a direct one. Reads are
always declared, never inferred.

An entry has one of three shapes.

**`map`**: explicit members. In the Windows config above, `Palette`
maps three members:

```csharp
// windows/App/MainPage.xaml.cs
namespace App;

public sealed partial class MainPage
{
    void Paint()
    {
        var accent = Application.Current.Resources["DesignAccent"];
        var system = Resources["SystemAccentColor"];
        var tint = Palette.Ink;
    }
}
```

**`patterns`**: a regex over the member, with `{1}`…`{9}` in `token`
filled from its groups. Patterns are RE2 and tried in order; the first
match wins. Apple's `AppTheme.spacing` uses both: `small` and `hairline`
from the map, and `accentTone`, `inkTone` and `surfaceTone` from the
pattern:

```swift
// apple/Sources/ContentView.swift
import SwiftUI

struct ContentView: View {
    var body: some View {
        Text("Hello")
            .foregroundStyle(DesignTokens.ink)
            .environment(\.density, DesignTokens.Density.compact)
            .padding(AppTheme.spacing.small)
            .border(AppTheme.spacing.accentTone, width: AppTheme.spacing.hairline)
            .background(DesignTokens.buttonBg)
            .padding(AppTheme.spacing.large)
    }
}
```

**`generated: true`**: the members are the platform's generated names, as
if written after `DesignTokens.`. It suits a composition local or an
environment value that exposes the generated class unchanged:

```kotlin
// android/src/main/Screen.kt
package com.example.app

import com.example.design.DesignColorValues
import com.example.design.DesignTokens

val ink = DesignTokens.ink
val darkSurface = DesignColorValues.dark.surface
val gap = LocalTokens.current.gap
```

**Precedence.** A member resolves through `map` first (an exact match),
then `patterns` in order, then, for `generated: true`, the platform's
generated names: its native format (swift, kotlin or xaml) first, then
css. A member both `map` and a pattern cover takes the `map` entry. An
entry is either `generated: true` or has a `map`, `patterns` or both; two
entries on one platform may not share a root.

**What `wrapper-unmapped` reports.** A member none of these resolves
(`AppTheme.spacing.large` above), or one that resolves to a name that is
not a token:

```text
apple/Sources/ContentView.swift:12:22: error wrapper-unmapped: AppTheme.spacing.large: wrapper AppTheme.spacing maps no member large: no map entry, no pattern matches; reads are always declared, never inferred
```

**Dangling map entries.** Config cannot see token names, so a `map` value
that is not a token (a token renamed since the map was written) is
reported at lint time, at `airframe.yaml`
(`platforms.<p>.wrappers.<i>.map.<member>`), whether or not anything
reads it. A pattern's token depends on its captures, so it is checked
only when read.

## 4. Components and owners

A component token (tier `component`) belongs to one component and is
read only by that component's files. Declare the component in
`<content>/components/<id>.yaml`:

```yaml
# design-system/components/button.yaml
id: button
tokens: [button-bg, button-ink]
owners:
  - web/src/button/**
  - apple/Sources/Button*.swift
```

- **`id`** equals the file's basename.
- **`tokens`** lists the component's tokens. Each must exist, be tier
  `component` and say `component: button`; and every token that says
  `component: button` must be listed.
- **`owners`** are repo-relative globs: the files allowed to read the
  tokens. They are required unless **`shared: true`**, which lets any
  consumer read them. Owners are declared, never inferred.
- `noun`, `ruling`, `hostedBy` and `implementations` are accepted and
  type-checked; their rules land with the component inventory.

The owner reads it freely:

```tsx
// web/src/button/Button.tsx
export const Button = () => <button style={{ background: "var(--button-bg)", color: "var(--button-ink)" }} />;
```

`ContentView.swift` above is not an owner, so its `DesignTokens.buttonBg`
is a [`token-owner-violation`](../rules/token-owner-violation.md):

```text
apple/Sources/ContentView.swift:11:25: error token-owner-violation: platform apple: DesignTokens.buttonBg is token button-bg of component button, but apple/Sources/ContentView.swift matches none of its owners (web/src/button/**, apple/Sources/Button*.swift)
```

A component token whose component has no `components/<id>.yaml` has no
owners, so every read of it is a violation, and the message says the
component declares no owners.

## 5. Exemption markers

A marker suppresses one rule for one reference:

```css
/* web/src/vendor.css */
.embed {
  /* airframe-exempt primitive-read-by-consumer: vendor widget theme, see #42 */
  color: var(--blue-500);
  border-color: var(--gray-900); /* airframe-exempt primitive-read-by-consumer */
}
```

- A marker is one rule on one line: `airframe-exempt <rule>: <reason>`,
  in whatever comment the language has, on the reference's own line or
  alone on the line before it. A line before counts only when it is
  nothing but comment, with no code and no reference, so a trailing
  marker exempts its own line and never the next. The reason ends at the
  comment's closer (`*/`, `-->`).
- It needs a reason. A marker with none exempts nothing, and the finding
  says so:

```text
web/src/vendor.css:5:21: error primitive-read-by-consumer: platform web: --gray-900 is token gray-900, of tier primitive, which consumers may not read (it is readable by semantic); read a token of tier semantic instead (airframe-exempt on line 5 has no reason)
```

- Every honoured marker is listed in the gate JSON's `exemptions`, with
  its file, line, reason and the `rule` it suppresses (Airframe's
  addition to the gate protocol, as token `exempt` entries carry too), so
  an exemption is visible in review.

## 6. The rules

| Rule | Default | Fires on |
| --- | --- | --- |
| [`reference-undeclared`](../rules/reference-undeclared.md) | error | a name that is not a token (and, in CSS, not app-local); a consumers glob that matches nothing; a platform whose names cannot be computed because it does not build |
| [`primitive-read-by-consumer`](../rules/primitive-read-by-consumer.md) | error | a read of a token whose tier's `readableBy` lacks `consumers` (component tokens are left to the owner rule) |
| [`token-owner-violation`](../rules/token-owner-violation.md) | error | a component token read from a file none of its component's `owners` match, unless the component is `shared` |
| [`fallback-literal`](../rules/fallback-literal.md) | warn | a `var(--x, …)` fallback that is not itself a `var(`, whether or not it equals the token's value (the message says when it does) |
| [`wrapper-unmapped`](../rules/wrapper-unmapped.md) | error | a wrapper read that resolves to no token, and a `map` entry naming a token that does not exist |

`legacy.css` above trips the other two:

```text
web/src/legacy.css:4:19: error primitive-read-by-consumer: platform web: --gray-50 is token gray-50, of tier primitive, which consumers may not read (it is readable by semantic); read a token of tier semantic instead
web/src/legacy.css:5:23: warn fallback-literal: var(--gap) has the literal fallback 8px; a fallback hides a missing token, so read another var() or none (it equals the token's base value)
```

`fallback-literal` is a warning by default; raise it in `airframe.yaml`
with `lint: {rules: {fallback-literal: error}}`.

Each rule reports what it scanned: in text output, a tally line such as
`token-owner-violation  12 references`; in `--format gate`, a check with
a `scanned` count. A rule with nothing to scan says why (`n/a (no
platform declares consumers)`), and the gate's self-test checks that
each still fires on a fixture built to trip it and stays silent on a
clean one.

## Limits

- **Regex adapters, not parsers.** In code files (`.tsx`, `.swift`,
  `.kt`, `.cs` and the rest) the adapters see strings and comments as
  well as code, so `// was DesignTokens.accnet` is a reference. Only
  stylesheets have their comments blanked. Delete the stale text, or
  exempt it with a reason.
- **Reads are declared, never inferred.** A wrapper the config does not
  declare is invisible: `Theme.ink` through an undeclared `Theme` is not
  checked. Declare every layer you read tokens through; an unmapped
  member of a declared one is reported, never skipped.
- **Wrappers over system types stay undeclared.** An extension such as
  `extension Color { static let brand = DesignTokens.accent }` is not a
  wrapper to declare: `root: Color` would make every `Color.red` an
  unmapped member. Its own direct read (`DesignTokens.accent`) is still
  checked; reads of `Color.brand` are not.
- **Third-party and script-set CSS properties are invisible unless
  declared.** A library's properties need `externalProperties`; a
  property set from JS counts as app-local only when a consumer file sets
  it with a literal name (`setProperty("--x"`, `["--x"`), not one built
  at run time (`setProperty(name, …)`).
- **Owners are declared, never inferred.** Airframe does not guess a
  component's files from its name or folder.
- `component-unruled` and `retired-used` are planned, with the component
  inventory and retired treatments; they are not built yet.

## In CI

`airframe lint --format gate` (the `c65llc/airframe@v0` Action with
`args: lint --format gate`) runs the consumer rules with every other rule: a finding fails
the gate, and the JSON lists each rule's scanned count and every honoured
exemption.
