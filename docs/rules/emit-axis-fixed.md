---
severity: warn
since: v0.3.0
---

# emit-axis-fixed

A native platform emits a token that varies on a build-bound axis it does not fan out over, so it uses the axis's base value.

**Why**

A build-bound axis is chosen when the app is built, not while it runs. A native platform (`swift`, `colorsets`, `kotlin`, `xaml`) builds one output set per value of its `fanOut` axis; any other build-bound axis has no output of its own, so its tokens are resolved at the axis's base value and the other values never reach that platform. That may be what you want (a web-only release channel), but it should be said rather than silently dropped. It fires once per platform and axis, and only when some token the platform emits varies on the axis, directly or through an alias. It is a warning; `--strict` makes it an error.

**Fix**

Make the axis the platform's `fanOut` if each value needs its own build, make it `binding: runtime` if the app should switch it live, or keep the tokens that vary on it off the platform with `emit`. If the base value is right for the platform, nothing.

**Example**

```yaml
modes:
  app:     { values: [drafts, tasks], base: drafts, binding: build }
  channel: { values: [stable, next], base: stable, binding: build }
platforms:
  apple: { emit: [swift], fanOut: app }   # emit-axis-fixed: channel uses stable
```
