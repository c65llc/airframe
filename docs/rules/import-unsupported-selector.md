---
severity: error
since: v0.2.0
---

# import-unsupported-selector

A rule's selector is not `:root` with attribute conditions, so the importer cannot tell which mode it sets.

**Why**

Airframe's modes are conditions on the root element. A class (`.dark`), an element (`body`, `html`) or a descendant selector (`[data-theme=dark] .card`) themes something else, which no token file can express. `html` is rejected too: it matches the same element as `:root` but has lower specificity, so it can lose a cascade `:root` would win.

**Fix**

Write the rule on `:root`, with the mode as attribute conditions: `[attr]`, `[attr='v']`, `:not([attr])` or `:not([attr='v'])`. Spell `html` as `:root`. Rules that theme components belong in your component CSS, not in the token import.

**Example**

```css
:root { --gap: 4px; }
.dark { --gap: 8px; }
/* theme.css:2: error import-unsupported-selector: unsupported selector ".dark": only :root compounds are supported */
/* fix: :root[data-theme='dark'] { --gap: 8px; } */
```
