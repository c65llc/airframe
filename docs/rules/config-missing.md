---
severity: error
since: v0.1.0
---

# config-missing

There is no `airframe.yaml` at the project root.

**Why**

Every command starts from `airframe.yaml`: it names the content directory, the mode axes and the platforms. Without it there is nothing to lint, and a clean result would mean that nothing was read.

**Fix**

Run `airframe init`, or run Airframe from the directory that holds `airframe.yaml`.

**Example**

```yaml
version: 1
content: design-system
modes:
  appearance: { values: [light, dark], base: light }
```
