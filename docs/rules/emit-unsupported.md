---
severity: warn
since: v0.1.0
---

# emit-unsupported

A platform emits a format this release of Airframe does not build yet.

**Why**

`airframe.yaml` may declare every platform it will have (`swift`, `colorsets`, `kotlin`, `xaml` besides `css` and `json`) so the config does not churn as emitters land. Until a format's emitter ships, nothing is written for it, and saying so is the difference between "built and clean" and "never built". It is a warning so a config that plans ahead still lints clean; `--strict` makes it an error. A format Airframe has never heard of (`csss`) is `schema-invalid`, not this.

**Fix**

Nothing, if the platform is planned: the warning goes away when the emitter ships. Otherwise remove the format from the platform's `emit` list (and its `out` key).

**Example**

```yaml
platforms:
  web:   { emit: css }
  apple: { emit: [swift, colorsets] }   # emit-unsupported, once per format
```
