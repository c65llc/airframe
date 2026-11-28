---
severity: error
since: v0.6.0
---

# wrapper-unmapped

A read through a declared wrapper root maps to no token.

**Why**

A hand-written wrapper (`AppTheme.spacing.small`, `theme.space.large`) hides which token a read stands for. Airframe checks those reads only because `platforms.<p>.wrappers` declares the mapping: reads are always declared, never inferred. A member the wrapper's `map` and `patterns` do not cover, a mapping to a name that is not a token, or a `generated: true` member the platform does not generate would otherwise be a read no rule can check.

**Fix**

Add the member to the wrapper's `map`, or a pattern that covers it; correct a mapping whose token was renamed; or read the token directly.

**Example**

```yaml
wrappers:
  - root: AppTheme.spacing
    map: { small: space-2 }
    patterns: [ { match: '^space(\d+)$', token: 'space-{1}' } ]
```

```swift
AppTheme.spacing.large   // wrapper-unmapped: no map entry, no pattern matches
```
