---
severity: error
since: v0.1.0
---

# prefix-multiple-hues

Colour tokens that share a first name segment span more than 8° of hue.

**Why**

A name prefix reads as a family (`blue-*`), so a member of a different hue misleads everyone who picks by name.

**Fix**

Rename the stray token into the family its hue belongs to, or bring its hue within 8° of the others.

**Example**

```jsonc
"blue": {
  "$type": "color",
  "1": { "$value": { "colorSpace": "oklch", "components": [0.6, 0.12, 250] } },
  "2": { "$value": { "colorSpace": "oklch", "components": [0.6, 0.12, 30] } }
}
// blue-2 is an orange: name it orange-*
```
