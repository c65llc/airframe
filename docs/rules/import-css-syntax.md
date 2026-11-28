---
severity: error
since: v0.2.0
---

# import-css-syntax

A stylesheet given to `airframe import css` is not well-formed CSS, or uses syntax the importer does not read.

**Why**

The importer reads stylesheets the way a browser would only when it can read all of them: an unclosed block, string or comment, a declaration without a `:`, or a rule nested inside a rule would leave it guessing where the next declaration starts. It reports the line and imports nothing rather than import part of the file.

**Fix**

Read the message: it names what is wrong at that line. Close the block, string or comment, or add the missing `:`. Nested rules (CSS nesting) are not read: write each rule at the top level, or one level inside `@media`.

**Example**

```css
:root {
  --gap 4px;
}
/* theme.css:2: error import-css-syntax: the declaration --gap 4px has no ':' */
```
