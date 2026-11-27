---
severity: error
since: v0.1.0
---

# density-missing

A component dimension lacks a value for every density and gives no chrome reason.

**Why**

A component box that is sized for one density is wrong in the others, usually cramped on touch. So a tier-3 dimension says what it is at every density, or says why it doesn't vary.

**Fix**

Add a `density` value for every value of the density axis, or set `chrome` to the reason the size is fixed.

**Example**

```json
"tabstrip-h": {
  "$type": "dimension",
  "$value": { "value": 36, "unit": "px" },
  "$extensions": { "com.c65llc.airframe": { "tier": "component", "component": "tabstrip",
    "density": { "compact": 32, "standard": 36, "relaxed": 40 } } }
}
```
