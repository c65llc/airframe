---
severity: error
since: v0.1.0
---

# emitted-stale

A generated output differs from what `airframe build` would write, or is missing. A colorset in a catalog folder airframe wrote (its `Contents.json` says `"author": "airframe"`) that no token makes any more, after a rename or removal, is stale too.

**Why**

Emitted files are committed so consumers need no build step, and so they can drift from the source. CI fails on drift; a build that fails is reported, never read as clean.

**Fix**

Run `airframe build` and commit the result. Never edit a generated file by hand: the next build overwrites it. `airframe build` does not delete anything, so remove a stale colorset folder by hand.

**Example**

```sh
$ airframe build
$ git add design-system/generated
$ airframe build --check   # exits 0
```
