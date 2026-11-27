---
severity: error
since: v0.1.0
---

# dimension-below

A dimension that a `min-dimension` invariant matches resolves below its minimum in some context. The contexts include the base one, the render with no attribute set, where the token is its `$value`.

**Why**

Touch targets have a floor that every density must respect. Here the floor is declared data, not a hard-coded check. The finding names the invariant, the token, the context and the measured size.

**Fix**

Raise the token's value (or its compact density value) to at least the minimum. If the token is not a target, rename it so the invariant's `match` pattern no longer covers it. A `match` that selects no dimension token is reported as `schema-invalid`.

**Example**

```yaml
- id: touch-floor
  kind: min-dimension
  match: "*-target*"
  min: 44
  densities: all
```
