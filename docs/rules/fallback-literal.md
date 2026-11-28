---
severity: warn
since: v0.6.0
---

# fallback-literal

A consumer `var()` has a literal fallback, which hides a missing token.

**Why**

`var(--accent, #3b82f6)` renders the literal whenever `--accent` is missing, so a renamed or deleted token never shows up as broken; it shows up as last year's colour. A fallback that equals the token's value still drifts the day the token changes, and the message says when it does. A fallback that is itself a `var()` is fine: it reads another token, and its own fallback is checked in turn.

**Fix**

Drop the fallback, or fall back to another token. If a literal really is needed (an email template that loads without the stylesheet), exempt the line with a reason.

**Example**

```css
.link { color: var(--accent, #3b82f6); }       /* fallback-literal */
.link { color: var(--accent, var(--ink)); }    /* falls back to a token */
.mail { color: var(--accent, #3b82f6); } /* airframe-exempt fallback-literal: inlined into email */
```
