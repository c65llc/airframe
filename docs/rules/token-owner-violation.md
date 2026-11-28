---
severity: error
since: v0.6.0
---

# token-owner-violation

A consumer file reads a component token but matches none of the component's owners globs.

**Why**

A component token sizes one component's box. When another screen borrows `--button-bg` it quietly couples itself to the button, and the next change to the button breaks it. Owners are declared, never inferred: `components/<id>.yaml` lists the globs of the files that implement the component, and only those may read its tokens. A component marked `shared: true` may be read anywhere. A component with no `components/<id>.yaml` declares no owners, so every read of its tokens is a violation.

Reads through a declared wrapper count exactly like direct reads.

**Fix**

Read a semantic token instead, or move the code into the component's files. If the file really is part of the component, add a glob for it to `owners`. If the token is meant for everyone, promote it to a semantic token or mark the component `shared: true`.

**Example**

```yaml
# design-system/components/button.yaml
id: button
tokens: [button-bg]
owners: [web/src/button/**]
```

```css
/* web/src/card.css — token-owner-violation: not a button file */
.card { background: var(--button-bg); }
```
