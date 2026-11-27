---
severity: error
since: v0.1.0
---

# mode-ambiguous

Two of a token's mode keys both match some context and give different values.

**Why**

When `appearance:dark` and `density:compact` both apply, a token with a value for each but none for the pair has no single answer. CSS would pick by source order and the resolver by key order, so they could disagree.

**Fix**

Add a value for the combined key (`appearance:dark+density:compact`), or make the two values equal.

**Example**

```json
"gap": {
  "$type": "number", "$value": 4,
  "$extensions": { "com.c65llc.airframe": { "modes": {
    "density:compact": 2, "appearance:dark": 3,
    "appearance:dark+density:compact": 2 } } }
}
```
