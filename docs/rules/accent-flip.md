---
severity: error
since: v0.1.0
---

# accent-flip

A token's hue moves more than 1° between appearances, which a `same-hue-across-appearances` invariant forbids.

**Why**

An accent is one hue in every theme; only lightness and chroma move. Without this check an accent ramp can drift between light and dark, because nothing asserts that both themes are one accent. The finding names the invariant, the token, both appearances and the hue distance.

**Fix**

Give the dark value the light value's hue, and adjust only L and C. If the token really should change hue, take it out of the invariant's `tokens` list.

**Example**

```yaml
# invariants.yaml
- id: accent-hue-stable
  kind: same-hue-across-appearances
  tokens: [accent, accent-ink]

# accent: hue 75 in light, 200 in dark -> accent-flip
```
