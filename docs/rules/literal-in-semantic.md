---
severity: error
since: v0.1.0
---

# literal-in-semantic

A semantic token, or one of its mode values, is a literal instead of an alias to a primitive.

**Why**

Tier 2 names roles and tier 1 holds values. A literal in a semantic token is a second source for a value, and two sources drift apart.

**Fix**

Alias a primitive. If no primitive has the value, add one to the primitive tier and alias it.

**Example**

```json
"ink": {
  "$type": "color",
  "$value": "{gray.900}",
  "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } }
}
```
