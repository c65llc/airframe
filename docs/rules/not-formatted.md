---
severity: error
since: v0.1.0
---

# not-formatted

A token, config, invariants or component file differs from what `airframe fmt` would write.

**Why**

One canonical layout keeps diffs about meaning, not whitespace or key order, which matters when reviewers read token changes. CRLF-only differences do not count.

**Fix**

Run `airframe fmt` and commit the result.

**Example**

```sh
$ airframe fmt
$ airframe fmt --check   # exits 0
```
