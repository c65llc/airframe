---
severity: error
since: v0.2.0
---

# import-no-base

A custom property is set only under some mode, never on bare `:root`, so its token would have no `$value`.

**Why**

Every token needs a base value: it is what a platform uses when no mode applies, and what the mode values override. In CSS a property that is only set in dark mode is simply undefined in light mode, which no token can say.

**Fix**

Declare the property on bare `:root` too, with the value it should have when no mode applies.

**Example**

```css
:root { --surface: #fff; }
:root[data-theme='dark'] {
  --surface: #111;
  --glow: #333;
}
/* theme.css:4: error import-no-base: --glow is set only under appearance:dark and never on bare :root, so it has no $value */
```
