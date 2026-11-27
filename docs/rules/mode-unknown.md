---
severity: error
since: v0.1.0
---

# mode-unknown

A mode key names an axis or a value that `airframe.yaml` does not declare.

**Why**

A value for a mode nobody can select is dead, and usually a typo (`appearance:drak`) of one that matters, which then silently keeps its default.

**Fix**

Correct the key to a declared `axis:value`, or declare the value under `modes` in `airframe.yaml`. Combined keys join pairs with `+`.

**Example**

```jsonc
"modes": {
  "appearance:sepia": 2
}
// mode key "appearance:sepia": unknown value "sepia" for axis appearance
```
