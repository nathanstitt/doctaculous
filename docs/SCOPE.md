# Scope

Out of scope — don't gold-plate these without a concrete need:

- Full ICC colour management
- JavaScript
- Interactive AcroForm widget rendering
- Tagged-PDF / accessibility
- Digital-signature verification
- Vertical alternate glyph forms (the `vert`/`vrt2` OpenType features) and
  `text-combine-upright`

EPUB was previously listed here. It landed as an input format when the any⇄any conversion
goal made it a requirement; DRM-protected books stay refused by design.

`writing-mode` was once headed here too — an earlier remediation pass decided to record it
as a deferred gap with its cost stated, and that entry was never written. It is moot now:
vertical text ships (see FEATURES.md), and what remains is ordinary outstanding work
tracked in `docs/CSS-LAYOUT.md`, not a scope exclusion. The vertical-glyph-forms entry
above is the part that genuinely is excluded — a rotated glyph is the rotated Latin form
rather than a purpose-designed vertical one, and substituting real vertical forms means
GSUB feature application this engine does not do.
