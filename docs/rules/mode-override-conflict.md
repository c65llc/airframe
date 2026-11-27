---
severity: error
since: v0.1.0
---

# mode-override-conflict

A token defines the same mode key twice: once in its own `modes`, and once in a mode file.

**Why**

A theme file (one with a file-level `mode`) and a token's `modes` map are two ways to write the same override. If both write it, which one wins is an accident of load order.

**Fix**

Keep one: delete the value from the token's `modes`, or from the mode file. The finding names both places.

**Example**

```jsonc
// themes/dark.tokens.json
{
  "$extensions": { "com.c65llc.airframe": { "mode": "appearance:dark" } },
  "gap": { "$value": 3 }
}
```
