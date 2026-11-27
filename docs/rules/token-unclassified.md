---
severity: error
since: v0.1.0
---

# token-unclassified

A token has no tier, or is a component token that names no component.

**Why**

Tier rules can only judge a token that says which tier it is in. An unclassified token can read, and be read by, anything.

**Fix**

Set `tier` to `primitive`, `semantic` or `component`, and for a component token, set `component` too.

**Example**

```json
"gap": {
  "$type": "number", "$value": 4,
  "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } }
}
```
