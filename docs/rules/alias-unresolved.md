---
severity: error
since: v0.1.0
---

# alias-unresolved

A token aliases a token that is not defined.

**Why**

A dangling alias would turn into a dangling reference in every emitted platform, such as a `var(--x)` with nothing behind it. Dangling custom properties are a common way for a theme to break silently; this catches the token-file half of that.

**Fix**

Correct the alias's spelling (an alias names a token by its path, as in `{gray.900}`), or define the token it names.

**Example**

```json
{
  "gap": { "$type": "number", "$value": "{missing}" }
}
```
