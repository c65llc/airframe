---
severity: error
since: v0.1.0
---

# alias-cycle

Tokens alias each other in a loop, so none of them has a value.

**Why**

A cycle can never resolve, so every emitter would have to guess or drop the tokens. A cycle that only exists in some mode contexts is still reported; one confined to contexts that never occur together is not.

**Fix**

Break the loop: make one token in the chain a literal, or alias it to a token outside the cycle.

**Example**

```json
{
  "a": { "$type": "number", "$value": "{b}" },
  "b": { "$type": "number", "$value": "{a}" }
}
```
