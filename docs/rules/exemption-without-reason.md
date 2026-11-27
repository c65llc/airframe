---
severity: error
since: v0.1.0
---

# exemption-without-reason

A token is exempt from a rule but gives no reason.

**Why**

An exemption is a decision someone made, and the reason is the record of it. One without a reason exempts nothing, and Gantry's gate protocol fails a run that reports one.

**Fix**

Write the reason, ideally with the issue or ruling that justifies it, or remove the exemption and fix the finding.

**Example**

```json
"legacy-gap": {
  "$type": "number", "$value": 4,
  "$extensions": { "com.c65llc.airframe": { "exempt": { "token-unclassified": "imported from v2; classified in #12" } } }
}
```
