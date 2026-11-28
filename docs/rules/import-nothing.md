---
severity: error
since: v0.2.0
---

# import-nothing

The import found nothing to import, or nothing it could check.

**Why**

A clean import that read no custom properties would look like success. So would one whose equivalence check compared nothing. The importer says so instead: the files held no custom-property declarations (and no error while parsing them already says why), no platform in `airframe.yaml` emits CSS to compare against, or the generated project could not be loaded.

**Fix**

Pass the stylesheets that declare the theme's custom properties. If the message says no platform emits css, add one (`web: {emit: css}`) to `platforms` in `airframe.yaml`. Read any other message for its cause.

**Example**

```css
:root {
  color: red;
}
/* theme.css: error import-nothing: no custom properties to import in theme.css */
```
