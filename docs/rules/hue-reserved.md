---
severity: error
since: v0.1.0
---

# hue-reserved

A colour within a reserved hue's tolerance belongs to a token the invariant does not allow.

**Why**

Some hues carry meaning, such as danger red. If a brand or accent colour lands within a few degrees of it, it reads as an error. Here the reserved hue is declared data, not a hard-coded check.

**Fix**

Move the colour's hue outside the tolerance, or, if the token really does mean what the hue means, add it to the invariant's `only` list. `only` globs that select no colour token are reported as `schema-invalid`.

**Example**

```yaml
- id: danger-hue-reserved
  kind: hue-reserved
  hue: 25
  tolerance: 8
  only: ["danger", "danger-*"]
```
