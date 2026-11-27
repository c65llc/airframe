---
severity: error
since: v0.1.0
---

# accent-group-partial

An accent mode value sets some of the roles `accentGroups.required` lists, but not all.

**Why**

Each accent group (a per-app or user-chosen accent) retints a fixed set of roles together. If an override sets `accent` but not `accent-ink`, that group pairs its own accent with another group's ink, and the contrast nobody declared goes untested.

**Fix**

Give every role in `accentGroups.required` a value for that mode key, or drop the partial override.

**Example**

```yaml
accentGroups: { required: [accent, accent-ink] }

# accent sets "app:slides"; accent-ink does not
# -> app:slides sets 1 of 2 accent roles; missing accent-ink
```
