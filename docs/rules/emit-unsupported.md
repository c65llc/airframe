---
severity: warn
since: v0.1.0
retired: v0.5.0
---

# emit-unsupported

A platform emitted a format that release of Airframe did not build yet.

Retired in v0.5.0: every format `airframe.yaml` accepts is now built, so no format triggers it any more. Nothing reports this ID; the page stays because rule IDs are addresses, and `airframe rules` no longer lists it.

**Why**

`airframe.yaml` could declare every platform it would have (`xaml` besides `css`, `json`, `swift`, `colorsets` and `kotlin`) so the config did not churn as emitters landed. Until a format's emitter shipped, nothing was written for it, and saying so was the difference between "built and clean" and "never built". It was a warning so a config that planned ahead still linted clean; `--strict` made it an error. A format Airframe has never heard of (`csss`) is `schema-invalid`, not this.

**Fix**

Nothing: upgrade to v0.5.0 or later, and the format is built. On an older release, remove the format from the platform's `emit` list (and its `out` key), or leave it planned and accept the warning.

**Example**

```yaml
platforms:
  web:   { emit: css }
  windows: { emit: [xaml] }   # emit-unsupported before v0.5.0; built since
```
