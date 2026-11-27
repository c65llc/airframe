---
severity: error
since: v0.1.0
---

# yaml-syntax

`airframe.yaml` or `invariants.yaml` is not valid YAML.

**Why**

A config that does not parse stops loading, since everything else depends on it. An invariants file that does not parse checks no invariants, which would read as passing.

**Fix**

Fix the syntax at the reported line. Common causes are unclosed `[` or `{`, and tabs used for indentation.

**Example**

```yaml
version: [1        # yaml-syntax: did not find expected ',' or ']'
content: design-system
```
