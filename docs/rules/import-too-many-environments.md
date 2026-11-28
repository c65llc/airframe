---
severity: error
since: v0.2.0
---

# import-too-many-environments

The stylesheets mention so many attributes and media features that the equivalence check would compare more than 200,000 environments.

**Why**

The check proves equivalence by enumerating every environment: each attribute unset, set to each value seen, or set to some other value, times each media feature unset or set to each value seen. That product grows fast, and the check refuses rather than run for minutes or report less than it promised.

**Fix**

Import fewer files at once, or leave out rules that set attributes no mode uses. Each attribute or media feature that appears only in rules you are not importing shrinks the product.

**Example**

```text
airframe.yaml: error import-too-many-environments: more than 200000 environments (14 attributes and media features in play)
```
