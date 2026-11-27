---
severity: error
since: v0.1.0
---

# contrast-below

A contrast invariant's foreground on its background falls below the minimum ratio in some context.

**Why**

Contrast is declared once, as a pair, and checked in every context it applies to (each appearance, each accent group), so a retint cannot quietly break a pair that passed in one theme. The finding names the invariant, the pair, the context and the measured ratio.

**Fix**

Change the lightness of the foreground or the background until the pair meets the minimum in every context listed. Lowering `min` is a design decision, not a fix.

**Example**

```yaml
- id: selection-text-contrast
  kind: contrast
  fg: accent-ink
  bg: accent-soft
  min: 4.5
  forEach: [accentGroup, appearance]
```
