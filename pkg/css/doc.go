// Package css is a pure-Go, hand-written CSS engine: it tokenizes and parses CSS
// (CSS Syntax Level 3), matches selectors against a DOM with correct specificity,
// and runs the cascade and inheritance to compute the effective style of each
// element. It performs no layout and no rendering — it emits a ComputedStyle that
// the layout engine (pkg/layout/css) consumes.
//
// Scope boundary: the cascade is property-agnostic, but ComputedStyle carries a
// deliberately minimal typed property set (the normal-flow subset: display,
// color, background-color, the font-* group, line-height, text-align, margin/
// padding/border, width/height). Properties outside that set are parsed and kept
// as raw declarations on each rule so later sub-projects can extend ComputedStyle
// without re-parsing, but they do not yet populate a typed computed field. That
// is expected, not a gap.
//
// Like the rest of the toolkit, this package never panics on malformed input:
// unrecognized tokens, rules, and declarations are skipped (and optionally logged)
// so a single bad rule cannot discard a whole stylesheet.
package css
