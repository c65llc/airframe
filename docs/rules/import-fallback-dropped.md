---
severity: warn
since: v0.2.0
---

# import-fallback-dropped

A `var()` alias has a fallback value, which the importer drops.

**Why**

A token alias has no fallback: the aliased token always exists, so the fallback can never apply. The importer keeps the alias and drops the fallback. The equivalence check still proves the generated CSS gives the same values, so this is a warning.

**Fix**

Remove the fallback from the stylesheet, or ignore the warning: the imported token is the alias alone.

**Example**

```css
:root {
  --space: 4px;
  --gap: var(--space, 8px);
}
/* theme.css:3: warn import-fallback-dropped: --gap: var() fallback "8px" dropped from "var(--space, 8px)" */
```
