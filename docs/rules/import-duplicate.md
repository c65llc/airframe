---
severity: warn
since: v0.2.0
---

# import-duplicate

The same custom property is declared twice for the same mode; the later declaration wins.

**Why**

The browser applies the later declaration, and so does the importer, so the result is the same theme. But the earlier value is dead code, and often a sign that two edits collided. It is a warning, so it does not stop the import.

**Fix**

Delete the declaration that loses, or merge the two blocks, so each property is set once per mode.

**Example**

```css
:root {
  --gap: 4px;
  --gap: 6px;
}
/* theme.css:3: warn import-duplicate: --gap is declared twice for bare :root (theme.css:2 and theme.css:3); the later one wins */
```
