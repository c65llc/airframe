---
severity: error
since: v0.1.0
---

# tier-violation

A token aliases a token its tier may not read.

**Why**

Tiers keep one component's measurement from becoming a de facto foundation: primitives hold values, semantic tokens name roles, and component tokens size one component's box. So a primitive aliases nothing, a semantic token never reads a component token, and a component token reads semantic tokens, never primitives or another component's unshared tokens.

**Fix**

Alias a token from a tier this one may read, adding a semantic role if none fits. If two components need the same size, mark the component token `shared: true` or promote it to a semantic token.

**Example**

```jsonc
"tabstrip-h": {
  "$type": "dimension",
  "$value": "{space.9}",   // tier-violation: a component reads a primitive
  "$extensions": { "com.c65llc.airframe": { "tier": "component", "component": "tabstrip" } }
}
```
