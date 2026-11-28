---
severity: error
since: v0.1.0
---

# schema-invalid

A file does not match its schema: an unknown key, a missing field, or a value of the wrong shape.

**Why**

Airframe reads `airframe.yaml`, the token files, `invariants.yaml` and the `components/<id>.yaml` files strictly. An unknown key is usually a typo whose intent would otherwise be silently ignored. An invariant with a missing field is reported here rather than checked against zero values, and so is one whose selector picks no token (a min-dimension `match`, or hue-reserved `only` globs, naming nothing), since a typo'd absolute would otherwise read as not applicable. A component file is also checked against the tokens: every token it lists must exist, be component-tier and name it, and every component-tier token naming it must be listed. A platform's `wrappers` are checked here too: one shape per entry, a unique dotted `root`, and `patterns` whose `match` compiles and whose `token` uses only the capture groups it has.

**Fix**

Read the message and path: they name the key and what was expected. `airframe schema` prints the schemas.

**Example**

```yaml
version: 1
content: design-system
colour: blue   # schema-invalid: unknown key "colour"
```
