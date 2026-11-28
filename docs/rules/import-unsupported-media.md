---
severity: error
since: v0.2.0
---

# import-unsupported-media

A rule sits inside an `@media` condition the importer does not read.

**Why**

Modes are discrete: a value is on or off. The importer reads conjunctions (`and`) of discrete features such as `prefers-color-scheme`, `prefers-contrast`, `pointer`, `hover`, `forced-colors` and `display-mode`. A range feature (`min-width`), a media type, `not`, `or` or `only` describes a continuum or a union no mode key can stand for, and the equivalence check could not enumerate it.

**Fix**

Keep layout breakpoints out of the token import; put them in your component CSS. If the condition is really a mode, spell it with a discrete feature and give the axis that `web:` spelling in `airframe.yaml`.

**Example**

```css
:root { --gap: 4px; }
@media (min-width: 600px) {
  :root { --gap: 8px; }
}
/* theme.css:3: error import-unsupported-media: unsupported media condition "(min-width: 600px)": media feature "min-width" is not a supported discrete feature */
```
