package css

import (
	"image/color"
	"strings"
)

// BoxShadow is one entry of a `box-shadow` list, per CSS Backgrounds 3 §6.
//
// The lengths stay in their authored unit, exactly as every other Length on
// ComputedStyle does: em resolves against the box's own font size and this
// package has no access to it. The layout engine resolves them (see
// pkg/layout/css's boxShadows).
//
// Color is kept as the RESOLVED colour plus a HasColor flag rather than as a
// raw token, because the whole grammar is parsed here anyway and the only
// context-dependent case is the omitted/`currentColor` one — which HasColor
// records. A false HasColor means "use the box's own `color` property", which
// is the spec's default and which only the cascade knows.
type BoxShadow struct {
	// OffsetX, OffsetY displace the shadow from the box. Positive Y is DOWN,
	// matching page space.
	OffsetX, OffsetY Length
	// Blur is the blur radius: a non-negative <length>. A negative value makes
	// the whole declaration invalid (spec).
	Blur Length
	// Spread grows (positive) or shrinks (negative) the shadow's shape before
	// it is blurred.
	Spread Length
	// Color is the shadow's colour when HasColor is true. When HasColor is
	// false the author omitted it (or wrote `currentColor`) and the consumer
	// must substitute the element's own `color`.
	Color    color.RGBA
	HasColor bool
	// Inset selects the inner-shadow rendering: the shadow is painted INSIDE
	// the padding box rather than outside the border box, and its shape is the
	// complement of the outer one. It is not a sign flip — see
	// pkg/layout/paint's paintBoxShadow.
	Inset bool
}

// parseBoxShadow parses a `box-shadow` declaration value into its shadow list,
// in SOURCE ORDER — which is PAINT order back-to-front reversed: the spec says
// the first shadow in the list is on TOP. The painter, not the parser, applies
// that reversal, so this slice reads the way the author wrote it.
//
// ok is false for `none` (which the caller normalizes to an empty list) and for
// any malformed value. CSS error handling makes an invalid declaration ignored
// ENTIRELY, so `box-shadow: 2px 2px, garbage` yields NO shadow rather than the
// half that parsed — the same rule pkg/filtereffects.Parse follows for the
// `filter` shorthand.
//
// The grammar accepted is the spec's full one:
//
//	<shadow># where <shadow> = inset? && <length>{2,4} && <color>?
//
// `&&` means the three components may appear in ANY ORDER, so `inset red 2px
// 2px`, `red 2px 2px inset` and `2px 2px inset red` are all the same shadow.
// Only the LENGTHS are order-significant among themselves (x, y, blur, spread).
// Accepting only the common `<lengths> <color>` ordering would reject valid
// author CSS that browsers render, so the loop below dispatches on token kind
// rather than on position.
func parseBoxShadow(value string) ([]BoxShadow, bool) {
	if strings.EqualFold(strings.TrimSpace(value), "none") {
		return nil, false
	}
	tz := newTokenizer(value)
	var out []BoxShadow
	for {
		sh, more, ok := parseOneBoxShadow(tz)
		if !ok {
			return nil, false
		}
		out = append(out, sh)
		if !more {
			break
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// parseOneBoxShadow reads one <shadow> from tz, stopping at the comma that
// separates it from the next or at end of input. more reports whether a comma
// was consumed and another shadow must follow.
//
// It is written as a token loop rather than as a positional match because the
// grammar's `&&` combinator permits any interleaving of the three components
// (see parseBoxShadow). Each iteration classifies the next token and routes it:
// a length-shaped token extends the length run, `inset` sets the flag, and
// anything else is offered to parseColor. A duplicate `inset`, a second colour,
// a fifth length, or a token no branch accepts all invalidate the declaration.
func parseOneBoxShadow(tz *tokenizer) (sh BoxShadow, more, ok bool) {
	var lens []Length
	sawInset, sawColor := false, false
	for {
		// The position is saved before each token because the colour branch
		// hands the tokenizer to parseColor, which reads from the CURRENT
		// position — an rgb() colour spans several tokens, so parseColor must
		// see the leading ident itself and cannot be given an already-consumed
		// one.
		save := *tz
		tok := tz.next()
		switch tok.Kind {
		case TokenEOF:
			sh, ok = finishBoxShadow(lens, sh, sawInset)
			return sh, false, ok

		case TokenWhitespace:
			continue

		case TokenComma:
			sh, ok = finishBoxShadow(lens, sh, sawInset)
			return sh, true, ok

		case TokenNumber, TokenDimension, TokenPercent:
			l, lok := parseLength(tok)
			// A PERCENTAGE is not a <length> and is rejected, which invalidates
			// the whole declaration — matching how the `filter` length resolver
			// treats `blur(10%)`. parseLength accepts TokenPercent and "auto",
			// so both units are re-checked here rather than trusted.
			if !lok || l.Unit == UnitPercent || l.Unit == UnitAuto || l.Unit == UnitContent {
				return sh, false, false
			}
			if len(lens) >= 4 {
				return sh, false, false // a fifth length: <length>{2,4} is exceeded
			}
			lens = append(lens, l)
			continue

		case TokenIdent:
			if strings.EqualFold(tok.Text, "inset") {
				if sawInset {
					return sh, false, false // `inset inset` is not `inset?`
				}
				sawInset = true
				continue
			}
			if strings.EqualFold(tok.Text, "currentcolor") {
				// `currentColor` is the one colour keyword parseColor does not
				// know, and it means exactly what an OMITTED colour means. It
				// FILLS the grammar's single <color> slot (so `currentColor red`
				// is still invalid) while leaving HasColor false, which is how
				// the consumer is told to substitute the box's own `color`.
				if sawColor {
					return sh, false, false
				}
				sawColor = true
				continue
			}
		}

		// Anything else — a #hash, a named-colour ident, `rgb(` — must be the
		// shadow's <color>. Rewind so parseColor starts at the token itself.
		if sawColor {
			return sh, false, false // a second colour is not `<color>?`
		}
		*tz = save
		c, cok := parseColor(tz)
		if !cok {
			return sh, false, false
		}
		sh.Color, sh.HasColor, sawColor = c, true, true
	}
}

// finishBoxShadow validates one shadow's collected lengths and folds them into
// sh. `<length>{2,4}` means the two offsets are REQUIRED and blur/spread are
// optional, in that order; a missing one is zero.
//
// A NEGATIVE blur radius is an error (spec) and invalidates the declaration. A
// negative SPREAD is legal and shrinks the shadow — the two are deliberately
// not treated alike, and conflating them would silently accept CSS a browser
// drops.
func finishBoxShadow(lens []Length, sh BoxShadow, inset bool) (BoxShadow, bool) {
	if len(lens) < 2 {
		return sh, false
	}
	sh.OffsetX, sh.OffsetY = lens[0], lens[1]
	if len(lens) > 2 {
		if lens[2].Value < 0 {
			return sh, false
		}
		sh.Blur = lens[2]
	}
	if len(lens) > 3 {
		sh.Spread = lens[3]
	}
	sh.Inset = inset
	return sh, true
}
