---
name: airframe-adopt
description: Use when bringing Airframe into an existing app, to import its CSS theme as tokens with an equivalence check, then declare its platforms, consumers, wrappers and components and gate it in CI, keeping airframe lint passing at every commit.
---

# Airframe: adopt an existing app

Adoption turns a theme an app already ships into Airframe's source of truth without changing what anyone sees: the custom properties are imported as tokens and proved equivalent, the generated outputs replace the hand-written ones, and then the app code that reads them is brought under the consumer rules one area at a time, so `airframe lint` passes at every commit. Read the `airframe` skill first for the file formats and the check loop.

## When to use

- A repository without `airframe.yaml` whose app has a CSS theme of custom properties (`:root { --surface: …; }`), or native token code, that Airframe should own
- A project adding a platform, or a part of the app, whose code reads tokens but is not yet in a platform's `consumers`

## Workflow

1. **Initialise.** Run `airframe init`. It writes `airframe.yaml`, a starter content root (`design-system/`) and its built outputs, and it refuses to overwrite anything that exists. Then describe the app's real mode axes in `airframe.yaml`: each axis's values, its base value and its `web:` spellings, which must match the selectors and media queries the CSS already uses (`:root[data-theme='dark']`, `@media (prefers-color-scheme: dark)`, a `data-density` attribute). The importer never invents a mode.

2. **Import the CSS theme.** Delete the starter `*.tokens.json` files `airframe init` wrote, since the import writes its own and checks only those. Then check the import, writing nothing:

   ```bash
   airframe import css --dry-run theme.css
   airframe import css theme.css
   ```

   It reads the stylesheets, maps each block to a mode key, and proves that the CSS `airframe build` would generate resolves every custom property to the same value in every environment the stylesheets can tell apart; if anything fails, nothing is written. Pass token-only files: a selector other than `:root` with attribute conditions, or an at-rule such as `@font-face`, fails the import by design. Fix each finding by its ID (`airframe explain <id>` prints its page), such as the `import-unmapped-scope` finding (no mode key is spelled that way: fix `airframe.yaml`) or the `import-not-equivalent` finding (the generated CSS differs somewhere).

3. **Assign tiers, then build.** The importer guesses tiers: all-literal tokens are `primitive`, tokens with a `var()` are `semantic`. Correct each one, as the `airframe-add-token` skill describes: a semantic token aliases a primitive, a value one component owns is a `component` token. Run `airframe fmt`, `airframe build` and `airframe lint`, and replace the hand-written stylesheet with the generated CSS at the web platform's `out` path. Commit only when `airframe lint` passes.

4. **Add the native platforms.** For each native app, add a platform to `airframe.yaml` (`apple` emitting `swift` and `colorsets`, `android` emitting `kotlin`, `windows` emitting `xaml`, with their `out` paths and options), following the platform's guide in the Airframe repository (`docs/guides/apple.md`, `android.md` or `windows.md`). Run `airframe build` and add the outputs to the app's build. Commit with `airframe lint` passing.

5. **Survey the consumers, without committing.** Give each platform the full `consumers` globs of the app code that reads tokens, such as `web/src/**/*.{css,tsx}` or `ios/**/*.swift`, and run `airframe lint --json`. The `reference-undeclared`, `primitive-read-by-consumer`, `fallback-literal` and `token-owner-violation` findings are the to-do list. Group them into areas of the app and write the plan down. Then narrow each platform's `consumers` to nothing yet adopted, so `airframe lint` passes again.

6. **Declare the wrappers.** Native code often reads tokens through a hand-written layer (a theme struct, a composition local, a C# helper). Declare each one under the platform's `wrappers`, with `generated: true` when its members are the generated names, or a `map` and `patterns` for irregular names, so its reads count like direct ones. A wrapper read that maps to no token is the `wrapper-unmapped` rule.

7. **Widen the consumers one area at a time.** Add that area's glob to the platform's `consumers` (every glob must match a file), run `airframe lint --json`, and fix the area's findings: read the role token instead of a primitive or a literal, as the `airframe` skill says. An audited exception gets an `airframe-exempt <rule>: <reason>` comment on its line, with a reason, and only when the person agrees. Run `airframe fmt`, `airframe build` and `airframe lint`, and commit the area on its own only when `airframe lint` passes. Repeat until the full globs from step 5 are back.

8. **Inventory the components.** For each component whose tokens are tier `component`, write `components/<id>.yaml` with the `airframe-component` skill: its owned tokens, its `owners`, an `implementations` entry for every platform with consumers, and its `@ruling:` reference to the Gantry ruling it answers to. Commit with `airframe lint` passing.

9. **Gate it in CI.** Add the GitHub Action, which runs one released, checksummed binary:

   ```yaml
   - uses: c65llc/airframe@v0
     with:
       version: vX.Y.Z   # the exact release the project uses
       args: lint --format gate
   ```

   Add a step that runs `airframe build --check` too, and, so reviewers see what a pull request does to the tokens, one that writes `airframe diff "origin/$GITHUB_BASE_REF" --format md` to `$GITHUB_STEP_SUMMARY` on pull requests (with `fetch-depth: 0` on the checkout). Commit.

## Rules

- Commit only with `airframe lint` passing; do not skip it, and never commit while it reports errors.
- Import with `--dry-run` first, and never hand-edit the imported values to make the equivalence check pass; fix `airframe.yaml`'s spellings or the stylesheet instead.
- Widen `consumers` one area at a time; never commit a glob whose files still have findings.
- Adoption changes no pixels: import what the app ships, not what it should be. Change values later with `airframe-add-token`.
- Never add an `airframe-exempt` marker, or set a rule `off`, to get an area through; fix the read, or ask the person.
- Never hand-edit `generated/`; `airframe build` rewrites it.
- Do not change the app's look or behaviour during adoption beyond swapping hand-written values for the tokens that hold them.
