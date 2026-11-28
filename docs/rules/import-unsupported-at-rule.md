---
severity: error
since: v0.2.0
---

# import-unsupported-at-rule

A stylesheet uses an at-rule other than `@media`, which the importer does not read.

**Why**

An at-rule such as `@layer`, `@supports`, `@import` or `@font-face` changes which declarations apply, or brings in more of them, in ways the importer cannot map onto mode keys. Skipping it would import a theme that differs from the one the browser renders.

**Fix**

Move the custom properties out of the at-rule: write them at the top level, or inside an `@media` block whose conditions map to a mode. Leave rules that hold no custom properties (`@font-face`) out of the files you import.

**Example**

```css
@layer base {
  :root { --gap: 4px; }
}
/* theme.css:1: error import-unsupported-at-rule: the at-rule @layer is not supported */
```
