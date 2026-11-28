# Windows: WinUI 3 XAML resource dictionaries

The `windows` platform turns your tokens into a WinUI 3
**`ResourceDictionary`** per app: every colour as a theme-aware
`SolidColorBrush` (Light, Dark, and HighContrast or Default), and every
other token (spacing, radii, durations, curves, fonts, weights, shadow
geometry) as a typed resource. A WinUI app merges the file into
`App.xaml` and reads tokens by key, with `{ThemeResource}` or
`{StaticResource}`.

This guide is for teams with a Windows App SDK app (or several apps
sharing one design system) who want colours and values generated from the
token files rather than copied by hand. Output is byte-stable, so
`airframe build --check` can guard it in CI.

## 1. Configure the platform

Add a `windows` platform to `airframe.yaml`. The repository's
`testdata/windows` directory holds a complete example, with two apps,
`notes` and `tasks`:

```yaml
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ["@media (prefers-color-scheme: dark) :root:not([data-theme='light'])", ":root[data-theme='dark']"]}}
  density: {values: [compact, standard, relaxed], base: standard, binding: runtime, web: {attr: data-density}}
  app: {values: [notes, tasks], base: notes, binding: build, web: {attr: data-app}}
  channel: {values: [stable, next], base: stable, binding: build, web: {attr: data-channel}}
platforms:
  windows:
    emit: [xaml]
    out: "windows/{app}/DesignTokens.xaml"
    fanOut: app
    options:
      gamut: clip-srgb
      highContrast:
        accent: SystemColorHighlightColor
        banner: SystemColorButtonFaceColor
        blue-500: SystemColorHotlightColor
        button-bg: SystemColorButtonFaceColor
        button-ink: SystemColorButtonTextColor
        card-shadow: SystemColorWindowTextColor
        elevation: SystemColorWindowTextColor
        gray-200: SystemColorGrayTextColor
        gray-50: SystemColorWindowColor
        gray-800: SystemColorGrayTextColor
        gray-900: SystemColorWindowTextColor
        green-500: SystemColorHighlightColor
        highlight: SystemColorHighlightColor
        ink: SystemColorWindowTextColor
        shadow-sm: SystemColorWindowTextColor
        surface: SystemColorWindowColor
        vivid: SystemColorHighlightColor
```

- **`emit: [xaml]`** is the one format.
- **`out`** is where each dictionary is written (repo-relative). Without
  it, it goes under the content directory, at
  `generated/windows/DesignTokens.xaml`. `{app}` is a placeholder for a
  value of the `fanOut` axis; with no placeholder there is one file, and a
  token that differs per app fails (`token "x" varies per app; put {app}
  in the xaml out path`).
- **`fanOut`** names a `binding: build` axis (here, which app is being
  built). The platform writes one dictionary per value of that axis; that
  is how `accent` is `blue.500` in notes and `green.500` in tasks.
- **`channel`** is a second build-bound axis that is not fanned out, so
  Windows gets its base value, `stable`; see
  [What `emit-axis-fixed` means](#what-emit-axis-fixed-means).
- **`naming`** (top level) sets spellings. The default for XAML is
  `Design{Pascal}`: the token `button-bg` is the key `DesignButtonBg`. A
  token's `names.xaml` overrides it. A key must match
  `[A-Za-z_][A-Za-z0-9_]*`; XAML has no escape, so a name that is not one
  (`my-ink`) fails naming the token.

### Options

| Option | Value | Default |
| --- | --- | --- |
| `gamut` | How a colour outside sRGB reaches `#AARRGGBB`. `css-color-4`: CSS Color 4 gamut mapping (reduce chroma, keep lightness and hue). `clip-srgb`: clip each channel independently (can shift hue). | `css-color-4` |
| `highContrast` | A map from every colour and shadow token to a WinUI high-contrast system colour: `SystemColorWindowTextColor`, `SystemColorWindowColor`, `SystemColorHighlightColor`, `SystemColorHighlightTextColor`, `SystemColorHotlightColor`, `SystemColorGrayTextColor`, `SystemColorButtonFaceColor` or `SystemColorButtonTextColor`. Writes a `HighContrast` theme dictionary. | none: a `Default` dictionary repeating the base appearance |

An unknown option, an empty `highContrast`, an unknown system colour, or a
value that is not a string is `schema-invalid`, naming the key.

Build it:

```bash
airframe build
```

```text
wrote windows/notes/DesignTokens.xaml
wrote windows/tasks/DesignTokens.xaml
```

## 2. Merge the dictionary in `App.xaml`

The generated file is a plain XAML resource dictionary; Airframe never
touches your `.csproj`. Add each app's `DesignTokens.xaml` to that app's
project (the default `Page` build action for a `.xaml` file is right), and
merge it into the application's resources after the WinUI controls'
resources:

```xml
<Application
    x:Class="Notes.App"
    xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation"
    xmlns:x="http://schemas.microsoft.com/winfx/2006/xaml">
    <Application.Resources>
        <ResourceDictionary>
            <ResourceDictionary.MergedDictionaries>
                <XamlControlsResources xmlns="using:Microsoft.UI.Xaml.Controls" />
                <ResourceDictionary Source="ms-appx:///DesignTokens.xaml" />
            </ResourceDictionary.MergedDictionaries>
        </ResourceDictionary>
    </Application.Resources>
</Application>
```

The `ms-appx:///` path is the file's path inside the project: a file in a
`Styles` folder is `ms-appx:///Styles/DesignTokens.xaml`.

## 3. Use it

### `{ThemeResource}` and `{StaticResource}`

Colours live in `ResourceDictionary.ThemeDictionaries`, so read them with
**`{ThemeResource}`**: it re-resolves when the user switches between light,
dark and high contrast. Every other token is at the dictionary's root, so
**`{StaticResource}`** reads it once:

```xml
<Border
    xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation"
    Background="{ThemeResource DesignSurface}"
    Padding="{StaticResource DesignSpace2}">
    <TextBlock
        Foreground="{ThemeResource DesignInk}"
        FontFamily="{StaticResource DesignFontBody}"
        Margin="{StaticResource DesignGap}"
        Text="Notes" />
</Border>
```

A **non-colour token that varies on appearance** (here `hairline`, wider
in dark) is not at the root: it is written into every theme dictionary,
Light and Dark at their values and HighContrast (or Default) at the base
appearance's, beside the brushes. Read it with `{ThemeResource
DesignHairline}` like a colour, or it will not follow the theme. A shadow
whose geometry varies on appearance is themed the same way.

### Runtime axes: the suffixed keys

A token that varies on a runtime axis other than appearance (here
`density`) gets one resource per value, its key suffixed with the value in
Pascal case, plus the plain key holding the base value:

```xml
<ResourceDictionary
    xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation"
    xmlns:x="http://schemas.microsoft.com/winfx/2006/xaml">
    <x:Double x:Key="DesignGap">4</x:Double>
    <x:Double x:Key="DesignGapCompact">4</x:Double>
    <x:Double x:Key="DesignGapRelaxed">8</x:Double>
    <x:Double x:Key="DesignGapStandard">4</x:Double>
</ResourceDictionary>
```

`{StaticResource DesignGap}` always resolves (to `standard`, the base);
name the variant you want (`{StaticResource DesignGapCompact}`) or choose
it in code. A colour that varies on density (`surface`) gets suffixed
brushes in every theme dictionary: `{ThemeResource DesignSurfaceCompact}`.
With several such axes, the suffixes follow in axis order.

### Durations, weights and curves

- **Durations are `x:Double` milliseconds**, and the key carries no unit:
  `DesignDurationFast` is `150`. XAML cannot turn a double into a
  `Duration` or `KeyTime`, so convert it in code.
- **Font weights are `x:Double`** (`DesignWeightStrong` is `650`), because
  XAML has no literal for an arbitrary `FontWeight`. Convert it with
  `new FontWeight((ushort)w)`.
- **Curves are `KeySpline`** resources, for a `SplineDoubleKeyFrame`'s
  `KeySpline`.
- **Shadows** are one brush per layer (`DesignElevationLayer1`, in the
  theme dictionaries) and its geometry at the root
  (`DesignElevationLayer1OffsetX`, `…OffsetY`, `…Blur`, `…Spread`).
  Composition's `DropShadow` takes offset, blur and colour; it has no
  spread.

```csharp
using System;
using Microsoft.UI.Xaml;
using Windows.UI.Text;

static class Design
{
    static double Number(string key) => (double)Application.Current.Resources[key];

    public static TimeSpan DurationFast => TimeSpan.FromMilliseconds(Number("DesignDurationFast"));

    public static FontWeight WeightStrong => new FontWeight((ushort)Number("DesignWeightStrong"));
}
```

## High contrast

With `highContrast` set, the `HighContrast` theme dictionary maps every
brush to the system colour you chose, as
`Color="{ThemeResource SystemColorWindowTextColor}"`, so the app follows
the user's contrast theme. One mapping covers all of a shadow token's
layers. Every colour and shadow token must be mapped: one left out fails
the build, listing every unmapped token, because a HighContrast dictionary
missing a key fails at runtime in high-contrast mode. A mapped token that
is not a colour or shadow token emitted to `xaml` fails too.

Without `highContrast`, the dictionary has a `Default` theme repeating the
base appearance's values instead, so every key still resolves in high
contrast, but with your colours rather than the user's. Set
`highContrast` for accessible high-contrast support.

## What `emit-axis-fixed` means

```text
airframe.yaml: warn emit-axis-fixed: platform windows: channel is build-bound but not fanned out; using its base value stable
```

A build-bound axis is decided when the app is built. The platform writes
one dictionary per value of its `fanOut` axis only; any other build-bound
axis (here `channel`) has no output of its own, so the tokens that vary on
it are resolved at its base value and the other values never reach
Windows. The warning says so once per axis rather than dropping them
silently. To fix it, fan it out, make it `binding: runtime` (its values
become suffixed keys), or accept the base: the warning is non-fatal. See
[emit-axis-fixed](../rules/emit-axis-fixed.md).

## Limits

The XAML emitter fails closed: a value WinUI cannot express is an
`emit-failed` error naming the token, never a guess. The other platforms
still build.

- **No inset shadows.** WinUI has no inset shadow, so a shadow with an
  inset layer fails (`token "well": shadow layer 1 is inset, and WinUI has
  no inset shadow`).
- **A shadow's layer count must match across apps** (and across themes),
  because every app's dictionary holds the same keys.
- **Two appearances.** WinUI's themes are Light and Dark; an `appearance`
  axis with any other value fails, naming it.
- **Generic font families pass through.** CSS generic families such as
  `system-ui` and `sans-serif` are written as they are
  (`Inter, system-ui`). WinUI does not know them and falls back silently
  to its default font, so put a Windows font (`Segoe UI Variable`) before
  them.
- **No commas in a family name.** A XAML `FontFamily` reads commas as a
  fallback list, so a family name holding one fails.
- **Font weights are 1 to 999**, the range `FontWeight` takes.
- **Key collisions.** Two tokens that make one key in a dictionary fail
  with both names; rename one, or set `names.xaml`.
- **Checked, not compiled.** The output is checked structurally with
  Go's `encoding/xml` (root and namespaces, unique keys, the same brush
  keys in every theme, only known element types), not compiled by WinUI:
  that needs a Windows host with the Windows App SDK. `KeySpline` as a
  shared resource in particular is unverified on a real WinUI build.
  Report anything WinUI rejects.

## In CI

```bash
airframe lint --format gate   # the source and the generated files
airframe build --check        # exit 1, naming each generated file that is stale
```

Commit the generated outputs; `emitted-stale` and `build --check` name any
dictionary that no longer matches the tokens.
