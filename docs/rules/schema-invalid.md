---
severity: error
since: v0.1.0
---

# schema-invalid

A file does not match its schema: an unknown key, a missing field, or a value of the wrong shape.

**Why**

Airframe reads `airframe.yaml`, the token files and `invariants.yaml` strictly. An unknown key is usually a typo whose intent would otherwise be silently ignored. An invariant with a missing field is reported here rather than checked against zero values, and so is one whose selector picks no token (a min-dimension `match`, or hue-reserved `only` globs, naming nothing), since a typo'd absolute would otherwise read as not applicable.

**Fix**

Read the message and path: they name the key and what was expected. `airframe schema` prints the schemas.

**Example**

```yaml
version: 1
content: design-system
colour: blue   # schema-invalid: unknown key "colour"
```
