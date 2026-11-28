---
severity: error
since: v0.2.0
---

# import-type-conflict

One custom property holds values of different types in different modes.

**Why**

A token has one `$type`. CSS will hold a colour in one block and a length in another, but a token file cannot, and no typed platform (Swift, Kotlin, XAML) can declare a constant that is sometimes a colour. The message names both declarations.

**Fix**

Split the property in two, one per type, and point each use at the right one. If one of the values was a mistake, correct it.

**Example**

```css
:root { --edge: 1px; }
:root[data-theme='dark'] { --edge: #fff; }
/* theme.css:2: error import-type-conflict: --edge is a dimension (theme.css:1) and a color (theme.css:2); one token has one type */
```
