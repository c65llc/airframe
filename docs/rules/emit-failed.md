---
severity: error
since: v0.1.0
---

# emit-failed

A platform's emitter could not write its output.

**Why**

Airframe fails closed rather than emit something half right: an alias to a token that is not emitted to that format, or a value the format cannot spell, would otherwise become a dangling reference or a dropped declaration. The other platforms still build.

**Fix**

Read the message: it names the platform, the format and the cause. Usually the aliased token needs that format in its `emit` list, or the alias should point elsewhere.

**Example**

```jsonc
{
  "gap": { "$type": "number", "$value": 4,
    "$extensions": { "com.c65llc.airframe": { "emit": ["json"] } } },
  "gap-row": { "$type": "number", "$value": "{gap}" }
}
// platform web emits css: --gap-row aliases gap, which is not emitted to css
```
