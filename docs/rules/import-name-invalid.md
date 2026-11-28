---
severity: error
since: v0.2.0
---

# import-name-invalid

A custom property's name, without the leading `--`, is not a valid token name.

**Why**

Token names are lowercase words joined by `-`, so every platform can spell them: a Swift or Kotlin identifier, a XAML key and a CSS custom property all derive from the one name. A name with capitals, underscores or other characters would emit differently, or not at all, on some platform.

**Fix**

Rename the property in the stylesheet, and in every `var()` that reads it, to lowercase words joined by `-`, then import again.

**Example**

```css
:root {
  --Gap_Large: 4px;
}
/* theme.css:2: error import-name-invalid: --Gap_Large: a token name must match ^[a-z0-9]+(-[a-z0-9]+)*$ (lowercase words joined by -) */
/* fix: --gap-large */
```
