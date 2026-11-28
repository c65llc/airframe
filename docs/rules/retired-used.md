---
severity: error
since: v0.7.0
---

# retired-used

Consumer code reads a retired token or spells a retired treatment, or a token aliases a retired token.

**Why**

Retiring something is only worth doing if it cannot quietly come back. A token marked `retired: true` is still emitted, so code keeps compiling while it migrates, but nothing may read it: a consumer read, direct or through a wrapper, is reported, and so is a non-retired token whose alias chain reaches a retired one (at the token, so its `exempt` map applies). A treatment with no token to delete is held in `retired.yaml` by its spellings: RE2 patterns per adapter (`css`, `swift`, `kotlin`, `xaml`, `cs`), matched line by line in the consumer files that adapter reads. Stylesheet comments are blanked first, as the adapters read them, so a spelling in a comment does not fire.

An audited use is escaped by the treatment's `marker` with a reason, inside a comment on the matched line or on the comment-only line above it, or by `airframe-exempt retired-used: <reason>`. A marker with no reason, or outside a comment, escapes nothing. Each escape is listed with its marker in the gate's exemptions. Only stylesheet comments are blanked: in a code file a marker comment that repeats the retired spelling is itself a match, so it does not exempt the line below it. Put the marker on the matched line instead.

**Fix**

Read, alias or write what replaced the retired token or treatment. If a use is deliberate, mark it with a reason.

**Example**

```yaml
# design-system/retired.yaml
- id: flush-left-selection-bar
  spellings: { css: ["inset 3px 0 0"] }
  marker: audited-not-retired-treatment
```

```css
.row { box-shadow: inset 3px 0 0 var(--accent); }  /* retired-used */
.mail { box-shadow: inset 3px 0 0 red; }  /* audited-not-retired-treatment: email clients */
```
