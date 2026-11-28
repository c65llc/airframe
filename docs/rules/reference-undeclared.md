---
severity: error
since: v0.6.0
---

# reference-undeclared

A consumer file reads a token name that does not exist, or a platform's consumers glob matches no file.

**Why**

A reference to a token that is not there fails quietly at runtime: CSS falls back to the inherited value, and a renamed Swift or XAML key may still compile against a stale build. A `consumers` glob that matches nothing is worse, because the checks it was meant to run never run, so it fails lint instead of passing clean. A platform whose emitter cannot render its names is reported here too, since none of its references could be checked.

Names an adapter does not own are left alone: a CSS custom property some consumer file defines (`--sidebar-w: 240px`) is app-local, and a Swift, Kotlin or XAML name outside the naming template's prefix and suffix (`{ThemeResource SystemControlForegroundBaseHighBrush}`) is foreign.

**Fix**

Correct the name, or add the token. For an empty scan, fix the glob so it matches the files you meant (the message names the platform and the glob), or remove it. For unavailable names, fix the emit failure first.

**Example**

```css
.card { color: var(--accnet); } /* reference-undeclared: --accent was meant */
```

```yaml
platforms:
  web:
    consumers: ["web/**/*.tssx"]   # reference-undeclared: matches no file
```
