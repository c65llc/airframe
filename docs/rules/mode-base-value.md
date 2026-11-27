---
severity: error
since: v0.1.0
---

# mode-base-value

A mode key names an axis's base value, the one that `$value` already is.

**Why**

Every loaded axis has exactly one base value: the value rendered when no attribute is set. It is the value whose only web selector is bare `:root` (appearance's `light: ":root"`), else the axis's explicit `base:` (`density: { values: [compact, standard, relaxed], base: standard }`). `$value` *is* the base value, so a key naming it either repeats `$value` or contradicts it, and in CSS it would compete with `$value` by source order.

"Unset" is modelled as the base context: lint checks every invariant in it like any other context, so a `$value` that breaks a floor is caught even when every listed density clears it. An `also` media fallback (density relaxed on a coarse pointer, applied only while the attribute is unset) is covered too, because every value's context is linted.

The `density` shorthand is the one allowance: it may list the base density when that entry equals `$value` (a full table reads better), and the entry is dropped. A base entry that differs from `$value` is this finding. An explicit `modes` key naming a base value is always this finding, equal or not.

**Fix**

Delete the base pair from the key: write the value as `$value`, or write `app:slides` rather than `app:slides+appearance:light`. In the density shorthand, make the base entry equal `$value` or drop it.

**Example**

```jsonc
"modes": {
  "appearance:light": 2,           // mode-base-value: use $value
  "app:slides+appearance:light": 3  // mode-base-value: write "app:slides"
},
"density": { "compact": 44, "standard": 48 }  // with $value 40px: mode-base-value at standard
```
