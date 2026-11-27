---
severity: error
since: v0.1.0
---

# emitted-stale

A generated output differs from what `airframe build` would write, or is missing.

**Why**

Emitted files are committed so consumers need no build step, and so they can drift from the source. CI fails on drift; a build that fails is reported, never read as clean.

**Fix**

Run `airframe build` and commit the result. Never edit a generated file by hand: the next build overwrites it.

**Example**

```sh
$ airframe build
$ git add design-system/generated
$ airframe build --check   # exits 0
```
