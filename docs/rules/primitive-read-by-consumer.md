---
severity: error
since: v0.6.0
---

# primitive-read-by-consumer

A consumer file reads a token whose tier consumers may not read, such as a primitive.

**Why**

Primitives hold raw values; semantic tokens name the roles an app uses. An app that reads `--blue-500` directly cannot be re-themed by changing what `accent` means, and the dark appearance or another accent never reaches it. A tier is readable by consumers when its `readableBy` in `tiers` lists `consumers`; the default lets them read semantic tokens only. Component tokens are judged by `token-owner-violation` instead.

A read through a declared wrapper counts exactly like a direct read. The rule's name reflects the default tiers, where only primitives are closed to consumers; with custom `tiers` it fires for any non-component tier whose `readableBy` lacks `consumers`, and the message names the tiers that list it.

**Fix**

Read the semantic token that names the role, adding one if none fits. If a primitive really is meant for apps, say so in config by adding `consumers` to its tier's `readableBy`.

**Example**

```css
.banner { background: var(--blue-500); }  /* primitive-read-by-consumer */
.banner { background: var(--accent); }    /* reads the role */
```

```swift
Text("Hi").padding(AppTheme.spacing.space2) // through a wrapper: still a primitive
```
