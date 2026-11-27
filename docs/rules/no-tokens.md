---
severity: error
since: v0.1.0
---

# no-tokens

The content directory holds no `*.tokens.json` files.

**Why**

A design system with no tokens has nothing to lint or emit. Reporting success would be a clean gate that read nothing.

**Fix**

Check `content:` in `airframe.yaml` names the right directory, and that token files end in `.tokens.json`.

**Example**

```yaml
content: design-system
# needs at least one file such as
#   design-system/tokens/primitive.tokens.json
```
