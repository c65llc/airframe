# Android: Kotlin and Jetpack Compose

The `android` platform turns your tokens into Kotlin source a Jetpack
Compose app compiles: a **shared file** declaring a `DesignTokens` class
that names every token as a typed member (colours, spacing, durations,
curves, fonts, shadows) and takes the runtime axes as parameters, and, for
each app, a **`DesignColorValues`** object holding that app's colours for
light and dark, and optionally a Material 3 colour scheme per appearance.

Android has no asset catalog, so colours are data: `DesignColors` is a data
class with one `Color` per colour, and each app's `DesignColorValues`
supplies the values. One `DesignTokens` class serves every app; the colours
you hand it decide which app it is.

This guide is for teams with an Android app (or several apps sharing one
design system) who want colours and values generated from the token files
rather than copied by hand. Output is byte-stable, so `airframe build
--check` can guard it in CI.

## 1. Configure the platform

Add an `android` platform to `airframe.yaml`. The repository's
`testdata/android` directory holds a complete example, with two apps,
`notes` and `tasks`:

```yaml
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: [":root[data-theme='dark']"]}}
  density: {values: [compact, standard, relaxed], base: standard, binding: runtime, web: {attr: data-density}}
  app: {values: [notes, tasks], base: notes, binding: build, web: {attr: data-app}}
platforms:
  android:
    emit: [kotlin]
    out: {kotlin: "android/{app}/src/main/kotlin/DesignColorValues.kt"}
    fanOut: app
    options:
      package: com.example.design
      sharedOut: android/shared/src/main/kotlin/DesignTokens.kt
      material3Scheme: {primary: accent, onPrimary: button-ink, tertiary: highlight}
```

- **`emit: [kotlin]`** is the one format.
- **`out`** is where each app's `DesignColorValues.kt` is written
  (repo-relative). Without it, it goes under the content directory, in
  `generated/android/`. `{app}` is a placeholder for a value of the
  `fanOut` axis.
- **`fanOut`** names a `binding: build` axis (here, which app is being
  built). The platform writes one `DesignColorValues.kt` per value of that
  axis; a colour that differs per app is how `accent` is `blue.500` in
  notes and `green.500` in tasks.
- **`naming`** (top level) sets spellings. The default for Kotlin is
  `DesignTokens.{camel}`: the token `button-bg` is `buttonBg` on
  `DesignTokens`. Change the type part to rename the class; the colour
  types follow it, less a trailing `Tokens` (`AppTokens.{camel}` gives
  `AppColors` and `AppColorValues`). The member part is the token name in
  a case (`{camel}` or `{Pascal}`); a template that yields something that
  is not a Kotlin identifier, such as `{name}` (hyphens: `button-bg`),
  fails naming the token.

### Options

| Option | Value | Default |
| --- | --- | --- |
| `package` | The Kotlin package both files declare, as in `com.example.design`. **Required.** | none |
| `sharedOut` | Repo-relative path of the shared `DesignTokens.kt`. Required when `out` contains `{app}`; it cannot contain `{app}` itself, since it is written once. | none: the shared declarations open the values file, which then must not fan out |
| `colorSpace` | `srgb`: `Color(0xAARRGGBB)`. `display-p3`: `Color(red =, green =, blue =, alpha =, colorSpace = ColorSpaces.DisplayP3)`. | `srgb` |
| `gamut` | How a colour outside `colorSpace` is brought in. `css-color-4`: CSS Color 4 gamut mapping (reduce chroma, keep lightness and hue). `clip`: clip each channel independently (can shift hue). | `css-color-4` |
| `material3Scheme` | A map from Material 3 `ColorScheme` roles to colour tokens, as in `{primary: accent}`. Writes `lightScheme` and `darkScheme` into each app's `DesignColorValues`. Roles you leave out keep Material's defaults. | none: no schemes |

An unknown option, a role that is not a `ColorScheme` parameter, or a value
of the wrong kind is `schema-invalid`, naming the key.

Build it:

```bash
airframe build
```

```text
wrote android/notes/src/main/kotlin/DesignColorValues.kt
wrote android/shared/src/main/kotlin/DesignTokens.kt
wrote android/tasks/src/main/kotlin/DesignColorValues.kt
```

## 2. Add the outputs to each app

The generated files are plain source; Airframe never touches your Gradle
build.

1. Put the shared `DesignTokens.kt` where every app can compile it: a
   shared library module, or a source directory each app adds.
2. Put each app's `DesignColorValues.kt` in that app's module only, since
   every app's file declares the same `com.example.design.DesignColorValues`.

The files use Compose's `ui-graphics`, `ui-unit`, `ui-text` and
`animation-core` (and `material3` when `material3Scheme` is set), which a
Compose app already has.

## 3. Use it

`DesignTokens` takes the colours and each runtime axis:

```kotlin
package com.example.notes

import com.example.design.DesignColorValues
import com.example.design.DesignTokens

val tokens = DesignTokens(DesignColorValues.light)
```

`colors` comes first and has no default; every runtime axis is a parameter
defaulted to its base (`appearance = Appearance.Light`, `density =
Density.Standard`). In a composable, choose dark with
`isSystemInDarkTheme()`. Pass the matching `appearance` too: colours come
from `colors`, but a non-colour token can vary on appearance as well (here
`hairline`), and that one reads the parameter:

```kotlin
package com.example.notes

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.Composable
import com.example.design.DesignColorValues
import com.example.design.DesignTokens

@Composable
fun designTokens(density: DesignTokens.Density = DesignTokens.Density.Standard): DesignTokens {
    val dark = isSystemInDarkTheme()
    return DesignTokens(
        colors = if (dark) DesignColorValues.dark else DesignColorValues.light,
        appearance = if (dark) DesignTokens.Appearance.Dark else DesignTokens.Appearance.Light,
        density = density,
    )
}
```

To hand the tokens down a tree, provide that value through your own
`CompositionLocal`, as you would any theme object.

### Static and instance members

A non-colour token that does not depend on a runtime axis is a member of
the companion object, so it needs no instance: `DesignTokens.space1`,
`DesignTokens.durationFast`. Every colour, and every token that varies on a
runtime axis (here `density`: `gap` and `surface` change with it), is an
instance member, because it depends on the colours and state you hold.
Every static member also has an instance forwarder, so `tokens.space1`
works too and code can read every token through one value without
remembering which is which:

```kotlin
package com.example.notes

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.Dp
import com.example.design.DesignColorValues
import com.example.design.DesignTokens
import kotlin.time.Duration

val fast: Duration = DesignTokens.durationFast
val padding: Dp = DesignTokens.space1

val compact = DesignTokens(DesignColorValues.dark, density = DesignTokens.Density.Compact)
val sameFast: Duration = compact.durationFast
val gap: Dp = compact.gap
val surface: Color = compact.surface
```

### Material 3

With `material3Scheme` set, each app's `DesignColorValues` also holds
`lightScheme` and `darkScheme`, built with `lightColorScheme` and
`darkColorScheme` from the roles you mapped. Hand one to `MaterialTheme`:

```kotlin
package com.example.notes

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import com.example.design.DesignColorValues

@Composable
fun NotesTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    MaterialTheme(
        colorScheme = if (dark) DesignColorValues.darkScheme else DesignColorValues.lightScheme,
        content = content,
    )
}
```

With no `appearance` axis there is `lightScheme` alone.

## What `emit-axis-fixed` means

```text
airframe.yaml: warn emit-axis-fixed: platform android: channel is build-bound but not fanned out; using its base value stable
```

A build-bound axis is decided when the app is built. The platform writes
one output per value of its `fanOut` axis only; any other build-bound axis
(here `channel`) has no output of its own, so the tokens that vary on it
are resolved at its base value and the other values never reach Android.
The warning says so once per axis rather than dropping them silently. To
fix it, fan it out, make it `binding: runtime` (it becomes an enum and a
`DesignTokens` parameter), or accept the base: the warning is non-fatal.
See [emit-axis-fixed](../rules/emit-axis-fixed.md).

## Limits

The Kotlin emitter fails closed: a value the platform cannot express is an
`emit-failed` error naming the token, never a guess. The other platforms
still build.

- **Non-colour tokens are shared across apps.** The `DesignTokens` class is
  written once, so a spacing, duration or shadow geometry that differs per
  app fails (`token "gutter" varies per app; Kotlin non-colour tokens are
  shared`). Only colours, including a shadow's colour, may vary per app.
  Make the token the same everywhere, or keep it out of `kotlin`'s `emit`.
- **Material 3 role tokens** must be colour tokens emitted to `kotlin` (a
  token whose `emit` list leaves `kotlin` out fails, naming the role), and
  must not vary on a runtime axis other than `appearance`: a scheme has one
  colour per role per appearance, so a role mapped to `surface`, which
  varies on density, fails naming the axis. A role token may vary per app;
  each app's scheme gets its own value. The appearance axis must be
  `light` and `dark` only.
- **Dimensions are `dp`, not `sp`.** Every dimension token is a `Dp`: `px`
  as is, `rem` times 16. Text that should follow the user's font scale
  needs an `sp` you convert yourself.
- **Package segments are ASCII identifiers.** Each dot-separated segment of
  `package` must match `[A-Za-z_][A-Za-z0-9_]*` and not be a Kotlin
  keyword (`com.in.app` and `com.2x.app` are `schema-invalid`). Airframe
  does not backtick segments.
- **Identifier collisions.** Two tokens that map to one Kotlin identifier
  or one colour slot fail with both names; rename one of the tokens. A
  name starting with a digit is fine (`2xl` becomes `_2xl`), and a member
  named like a keyword is backticked.

## In CI

```bash
airframe lint --format gate   # the source and the generated files
airframe build --check        # exit 1, naming each generated file that is stale
```

Commit the generated outputs; `emitted-stale` and `build --check` name any
file that no longer matches the tokens. A colour edit names each app's
`DesignColorValues.kt`; a non-colour edit names the shared `DesignTokens.kt`.
