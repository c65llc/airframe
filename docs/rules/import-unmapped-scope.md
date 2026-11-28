---
severity: error
since: v0.2.0
---

# import-unmapped-scope

A rule's selector and media conditions are well-formed, but no mode key in `airframe.yaml` is spelled that way on the web.

**Why**

The importer never invents an axis: it maps each block back to a mode key by expanding every key with the same `web:` spellings `airframe build` emits, and looking the block up. A block that matches none of them would have no place in the token files, and dropping it would change the theme.

**Fix**

Read the message: it names the scope and the `web:` spelling that would produce it. Add the value to an axis, or correct the axis's `web:` spelling, in `airframe.yaml`, so the generated CSS uses the selector you already ship. Or change the stylesheet to the spelling the config already has.

**Example**

```css
:root { --gap: 4px; }
:root[data-mode='dim'] { --gap: 8px; }
/* theme.css:2: error import-unmapped-scope: :root[data-mode='dim'] matches no mode in airframe.yaml: an axis value whose web spelling is ":root[data-mode='dim']" would produce it */
```
