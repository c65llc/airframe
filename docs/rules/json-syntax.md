---
severity: error
since: v0.1.0
---

# json-syntax

A token file is not valid JSON.

**Why**

A file that does not parse contributes no tokens, so everything that aliases into it would also break. The other token files still load and are linted.

**Fix**

Fix the syntax at the reported line. Common causes are trailing commas, comments and duplicate keys, none of which JSON allows.

**Example**

```json
{
  "gap": { "$type": "number", "$value": 4, },
}
```
