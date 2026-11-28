---
severity: error
since: v0.7.0
---

# component-unruled

A component, or a retired treatment's ruling, does not answer to a ruled Gantry ruling: it is missing, malformed, unknown or not ruled.

**Why**

A component with no ruling is not in the system. `ruling: "@ruling:<id>"` names the design decision a component answers to, so nobody ships a box the design never agreed to. When the repository has a Gantry project (a `gantry.yaml` next to `airframe.yaml`, or the one the `gantry:` key names), the reference is resolved against its `rulings/<id>.md` front matter: the ruling must exist and its `status` must be `ruled`. A `proposed` ruling is not yet ruled, and a `superseded` one names the ruling that replaced it. With no Gantry project the check is syntax only. A `retired.yaml` entry's optional `ruling` is checked the same way, and a ruling record that cannot be read is reported here, at the record.

**Fix**

Add the component's ruling, or correct its spelling (`@ruling:` then a-z, 0-9 and `-`). Point a superseded ruling at its replacement, rule a proposed one in Gantry, or fix the ruling record the finding names.

**Example**

```yaml
# design-system/components/tabstrip.yaml
id: tabstrip
ruling: "@ruling:old-tabs"   # component-unruled: superseded by tab-strip
tokens: [tabstrip-h]
owners: [web/src/tabstrip/**]
```
