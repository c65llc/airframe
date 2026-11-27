---
severity: error
since: v0.1.0
---

# mode-base-value

A mode key names an axis's base value, the one that `$value` already is.

**Why**

An axis value whose only web selector is bare `:root` is the base: `$value` is that value. A key naming it would be emitted as a second `:root` block that competes with `$value` by source order.

**Fix**

Delete the base pair from the key: write the value as `$value`, or write `app:slides` rather than `app:slides+appearance:light`.

**Example**

```jsonc
"modes": {
  "appearance:light": 2,           // mode-base-value: use $value
  "app:slides+appearance:light": 3  // mode-base-value: write "app:slides"
}
```
