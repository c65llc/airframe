---
severity: error
since: v0.2.0
---

# import-not-equivalent

The CSS Airframe would generate from the imported tokens resolves a custom property differently from the original stylesheet in some environment.

**Why**

The import is only safe to adopt if the generated CSS is a drop-in replacement. The check enumerates every combination of the attributes and media features either stylesheet mentions and compares every property in each. A difference usually means the original CSS covers a mode only some of the ways `airframe.yaml` spells it, so the generated CSS applies the mode where the original did not. The message names the property, one environment and both values.

**Fix**

Compare the environment in the message with the axis's `web:` spelling in `airframe.yaml`. Either make the config match what the stylesheet does (drop a spelling it never uses), or add the missing block to the stylesheet, then import again. After 20 properties the rest are counted in one line.

**Example**

```css
:root { --ink: #000; }
:root[data-theme='dark'] { --ink: #fff; }
/* airframe.yaml spells dark as the attribute AND @media (prefers-color-scheme: dark):
   theme.css:1: error import-not-equivalent: --ink differs with prefers-color-scheme=dark:
   the original CSS gives #000, the generated CSS gives oklch(100% 0 0) (+1 more environments) */
```
