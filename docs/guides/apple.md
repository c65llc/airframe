# Apple: Swift and asset catalogs

The `apple` platform turns your tokens into two things an Xcode project
reads: an **asset catalog** of colorsets (one per colour, Display P3, with
light and dark variants) and a **Swift file** that names every token as a
typed `DesignTokens` member. Colours stay in the catalog, where the system
resolves appearance and the Xcode editor can see them; the Swift file reads
them by name and adds the non-colour tokens (spacing, durations, curves,
fonts, shadows) and the runtime axes.

This guide is for teams with an iOS or macOS app (or several apps sharing
one design system) who want colours and values generated from the token
files rather than copied by hand. Output is byte-stable, so
`airframe build --check` can guard it in CI.

## 1. Configure the platform

Add an `apple` platform to `airframe.yaml`. The repository's
`testdata/apple` directory holds a complete example, with two app targets,
`notes` and `tasks`:

```yaml
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: [":root[data-theme='dark']"]}}
  density: {values: [compact, standard, relaxed], base: standard, binding: runtime, web: {attr: data-density}}
  app: {values: [notes, tasks], base: notes, binding: build, web: {attr: data-app}}
platforms:
  apple:
    emit: [swift, colorsets]
    out: {swift: ios/Shared/DesignTokens.swift, colorsets: "ios/{app}/Assets.xcassets/Tokens/"}
    fanOut: app
    options: {accentColor: accent}
```

- **`emit`** lists the formats. Swift reads colours from the catalog, so a
  token that goes to `swift` and is a colour must go to `colorsets` too;
  otherwise the build fails and says which token.
- **`out`** says where each format is written (repo-relative). Without it
  they go under the content directory, in `generated/apple/`. `{app}` is a
  placeholder for a value of the `fanOut` axis.
- **`fanOut`** names a `binding: build` axis (here, which app is being
  built). The platform writes one output set per value of that axis. With
  `{app}` in the colorsets path you get one catalog per app, each holding
  that app's colours; a colour that differs per app is how `accent` is teal
  in one app and green in another.
- **`options`**: `accentColor: <token>` also writes `AccentColor.colorset`
  at the catalog root from that token (an Xcode target's accent colour
  setting reads it), and `gamut` chooses how an out-of-gamut colour reaches
  Display P3 (see below).
- **`naming`** (top level) sets spellings. The default for Swift is
  `DesignTokens.{camel}`: the token `button-bg` is `DesignTokens.buttonBg`.
  Change the type part to rename the struct; the member part must stay a
  case of the token name (`{camel}`).

Build it:

```bash
airframe build
```

```text
wrote ios/Shared/DesignTokens.swift
wrote ios/notes/Assets.xcassets/AccentColor.colorset/Contents.json
wrote ios/notes/Assets.xcassets/Tokens/Accent.colorset/Contents.json
…
```

## 2. Add the outputs to each app target

The generated files are plain source; Airframe never touches your
`.xcodeproj`.

1. Add the Swift file (`ios/Shared/DesignTokens.swift`) to every app target
   that uses it, as a normal source file.
2. Add each app's generated catalog to that app's target: the folder
   `ios/notes/Assets.xcassets` into the notes target, `ios/tasks/...` into
   tasks. If your project already has a catalog, either merge the generated
   `Tokens` folder into it or add the generated catalog as a second one;
   a target may hold several.

`Color("Tokens/Ink")` resolves from the **main bundle**. A Swift package or
framework that builds `DesignTokens.swift` finds no colours unless the
catalog is in the app target that loads it, so keep the catalog in the app
and the Swift file wherever you like within it.

## 3. Use it

```swift
import SwiftUI

struct Row: View {
    let tokens = DesignTokens(density: .compact)

    var body: some View {
        Text("Hello")
            .foregroundStyle(DesignTokens.ink)
            .padding(tokens.gap)
            .background(tokens.surface)
    }
}
```

A token that does not depend on a runtime axis is `static`; read it as
`DesignTokens.ink`. A token that varies on a **runtime** axis (here
`density`: `gap` and `surface` change with it) is an instance member,
because it depends on the state you hold, so you create a
`DesignTokens(density: .compact)` and read `tokens.gap`. Every static
member also has an instance forwarder, so `tokens.ink` works too and a view
can read every token through one `tokens` value without remembering which
is which:

```swift
import SwiftUI

struct Header: View {
    let tokens = DesignTokens(appearance: .dark, density: .relaxed)

    var body: some View {
        Text("Title")
            .foregroundStyle(tokens.ink)
            .padding(tokens.gap)
    }
}
```

An initialiser takes each runtime axis, defaulted to its base. Appearance
needs no code for a colour that varies only on it: the catalog holds both
the light and dark variants and the system picks, so `DesignTokens.ink` is
right in light and dark. (A colour that also varies on density, like
`surface`, is one colorset per density and so an instance member.)

## What `emit-axis-fixed` means

```text
airframe.yaml: warn emit-axis-fixed: platform apple: channel is build-bound but not fanned out; using its base value stable
```

A build-bound axis is decided when the app is built. The platform writes
one output per value of its `fanOut` axis only; any other build-bound axis
(here `channel`) has no output of its own, so the tokens that vary on it
are resolved at its base value and the other values never reach Apple. The
warning says so once per axis rather than dropping them silently. To fix
it:

- **Fan it out.** If each value needs its own build, make it the platform's
  `fanOut`. One axis fans out per platform, so this is for when `channel`,
  not `app`, is what distinguishes your targets.
- **Make it runtime** (`binding: runtime`) if the app should switch it
  live; it becomes an enum and an initialiser parameter.
- **Accept the base**, if that is right: the warning is non-fatal, and
  `airframe lint` exits 0. Silence it by keeping the tokens that vary on
  that axis out of `emit`, or leave it.

See [emit-axis-fixed](../rules/emit-axis-fixed.md).

## Display P3 and the gamut option

Tokens are authored in OKLCH. Colorsets are written in **Display P3**, the
wide gamut every current Apple display shows. A colour that fits P3 is
converted exactly. One that does not (a vivid green past the P3 edge) must
be brought in, and the `gamut` option says how:

| `gamut` | Behaviour |
| --- | --- |
| `css-color-4` (default) | CSS Color 4 gamut mapping: reduce chroma until it fits, keeping lightness and hue |
| `clip-p3` | Clip each channel to the P3 range; clips each channel independently; can shift hue |

The same colour mapped the same way is what you get on every build; the
choice is stated, never implicit.

## When the build fails

Apple emitters fail closed: a value the platform cannot express is an
`emit-failed` error naming the token, never a guess. The other platforms
still build. The common ones:

- **A third appearance value.** A colorset has a light base and one dark
  variant only. An `appearance` axis with a third value (say `high-contrast`)
  that a colour varies on cannot be written: the error names the value and
  the token.
- **Identifier collisions.** Two tokens that map to one Swift identifier
  or one colorset name, or a token whose name does not make a legal Swift
  identifier (it starts with a digit), fail with both names. Rename one of
  the tokens.
- **A per-app value in a shared Swift file.** If a non-colour token
  (`gutter`) has a different value per app but `out.swift` has no `{app}`,
  one file cannot hold both. Put `{app}` in the Swift path
  (`ios/{app}/DesignTokens.swift`) to get one file per app, or make the
  token the same everywhere. Likewise, when the colorsets path ends in
  `{app}`, the Swift file must be per app, since the catalog namespace it
  reads differs.
- **A colour that goes to Swift but not to colorsets**, or an accent token
  that varies on an axis AccentColor cannot express.

## In CI

```bash
airframe lint --format gate   # the source and the generated files
airframe build --check        # exit 1, naming each generated file that is stale
```

Commit the generated outputs; `emitted-stale` and `build --check` name any
file that no longer matches the tokens.
