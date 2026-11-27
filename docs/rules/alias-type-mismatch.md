---
severity: error
since: v0.1.0
---

# alias-type-mismatch

A token aliases a token of a different `$type`.

**Why**

A dimension that aliases a colour has no meaningful value, and each emitter would fail in its own way. The finding names both types and where the aliased token is declared.

**Fix**

Alias a token of the same type, or correct the `$type` of one of the two tokens.

**Example**

```json
{
  "ink": { "$type": "color", "$value": { "colorSpace": "oklch", "components": [0.2, 0.01, 250] } },
  "gap": { "$type": "dimension", "$value": "{ink}" }
}
```
