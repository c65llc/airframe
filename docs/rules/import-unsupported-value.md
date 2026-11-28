---
severity: error
since: v0.2.0
---

# import-unsupported-value

A custom property's value is not one the importer can turn into a token value.

**Why**

A token's value must be something every platform can spell: a colour, a `px` or `rem` dimension, a number, an `ms` or `s` duration, a `cubic-bezier()`, a font family or weight, a shadow, or a `var()` alias. `calc()`, `clamp()`, other units, keywords and `!important` have no token equivalent. So does a `var()` to a property the imported CSS never declares, since the alias would dangle.

**Fix**

Replace the value with a literal, or alias a token that holds it. Write a computed value out (`calc(2px * 2)` is `4px`). A bare word list is a font family only when the property name ends in `-font`, `-family` or `font-family`. Drop `!important`: Airframe never emits it. Import the file that declares an aliased property along with this one.

**Example**

```css
:root {
  --gap: calc(2px * 2);
}
/* theme.css:2: error import-unsupported-value: --gap: unsupported value "calc(2px * 2)" */
```
