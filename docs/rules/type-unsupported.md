---
severity: error
since: v0.1.0
---

# type-unsupported

A token's `$type` is not one Airframe supports.

**Why**

Every emitter must be able to write every token, so Airframe accepts only the DTCG types it can emit on every platform: color, cubicBezier, dimension, duration, fontFamily, fontWeight, number and shadow. A token of another type is left out, never half-emitted.

**Fix**

Use a supported type, or keep the value outside the token files until Airframe supports it.

**Example**

```jsonc
{
  "fade": { "$type": "gradient", "$value": [] }   // type-unsupported
}
```
