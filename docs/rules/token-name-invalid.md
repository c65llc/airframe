---
severity: error
since: v0.1.0
---

# token-name-invalid

A token or group name is not lowercase words joined by `-`.

**Why**

A token's name becomes an identifier on every platform (`--tab-strip`, `TabStrip`, `tabStrip`). Only one spelling converts cleanly to all of them.

**Fix**

Rename it to lowercase words joined by `-`, and update the aliases that name it.

**Example**

```jsonc
{
  "Tab Strip": { ... }   // token-name-invalid
  "tab-strip": { ... }   // ok
}
```
