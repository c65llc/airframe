---
severity: error
since: v0.7.0
---

# component-incomplete

A component's implementations or hostedBy is not complete and true: an unknown or missing platform, a file that does not exist, an n/a with no reason, an unknown host or a host cycle.

**Why**

A component's inventory says where each platform builds it. It is only worth reading if it is complete and true, so every platform that declares `consumers` needs an `implementations` entry, and each entry is either a repo-relative file that exists or `n/a: <reason>` saying why that platform does not build it. A key that is not a configured platform is a typo or a platform that went away. `hostedBy` names the container component that hosts this one; it must be a component, and a chain of hosts must end, so a cycle (`a → b → a`) is reported once, naming every member.

**Fix**

Add the missing platform's file, or `n/a:` with a reason. Correct a path that moved, remove a platform that is not configured, and point `hostedBy` at a real container, or set it to `null`.

**Example**

```yaml
# design-system/components/tabstrip.yaml
implementations:
  web: web/src/tabstrip/TabStrip.tsx
  apple: ios/Shared/TabStrp.swift   # component-incomplete: does not exist
  android: "n/a:"                   # component-incomplete: n/a needs a reason
```
