package css

import (
	"strconv"
	"strings"
)

// The engine uses a 96dpi px-as-pt scalar: 1 CSS px == 1 layout "pt" unit, and 96 of
// them per inch (consistent with LetterWidthPt = 816 = 8.5in × 96). So absolute CSS
// units convert to the layout scalar at 96 per inch.
const (
	pxPerIn = 96.0
	pxPerCm = pxPerIn / 2.54
	pxPerMm = pxPerCm / 10.0
	pxPerPt = pxPerIn / 72.0 // CSS pt is 1/72 in
	pxPerPc = pxPerIn / 6.0  // 1 pica = 12 pt = 1/6 in
)

// pageSizeKeywords maps a CSS @page size keyword to its (width, height) in the layout
// px-as-pt scalar, portrait orientation (CSS Paged Media §6.2). The standard physical
// sizes are converted at 96dpi (the engine's px:pt convention) so they compose with
// the existing LetterWidthPt/LetterHeightPt constants. `landscape`/`portrait` swap the
// axes (handled in parsePageSize).
var pageSizeKeywords = map[string][2]float64{
	// ISO A/B series (mm → px at 96dpi).
	"a5":     {148 * pxPerMm, 210 * pxPerMm},
	"a4":     {210 * pxPerMm, 297 * pxPerMm},
	"a3":     {297 * pxPerMm, 420 * pxPerMm},
	"b5":     {176 * pxPerMm, 250 * pxPerMm},
	"b4":     {250 * pxPerMm, 353 * pxPerMm},
	"jis-b5": {182 * pxPerMm, 257 * pxPerMm},
	"jis-b4": {257 * pxPerMm, 364 * pxPerMm},
	// North American (in → px at 96dpi). letter == LetterWidthPt × LetterHeightPt.
	"letter": {8.5 * pxPerIn, 11 * pxPerIn},
	"legal":  {8.5 * pxPerIn, 14 * pxPerIn},
	"ledger": {11 * pxPerIn, 17 * pxPerIn},
}

// parsePageSize parses an @page `size` value into a (width, height) in the layout
// scalar. Supported forms (CSS Paged Media §6.2):
//
//	auto                      → ok false (no explicit size; caller keeps its default)
//	<length>                  → square page (w == h)
//	<length> <length>         → explicit width height
//	<keyword>                 → a named page size, portrait
//	<keyword> landscape       → the keyword's axes swapped (w > h)
//	<keyword> portrait        → the keyword, portrait (explicit; same as bare)
//	landscape / portrait      → orientation only with no keyword → ok false (no size
//	                            basis without a keyword/length; caller's default size,
//	                            orientation ignored — a documented limitation)
//
// ok is false for `auto`, an orientation-only value, or anything unparseable.
func parsePageSize(value string) (w, h float64, ok bool) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(value)))
	if len(fields) == 0 {
		return 0, 0, false
	}
	if len(fields) == 1 && fields[0] == "auto" {
		return 0, 0, false
	}

	var (
		keyword     string
		orientation string
		lengths     []float64
	)
	for _, f := range fields {
		switch f {
		case "portrait", "landscape":
			orientation = f
			continue
		case "auto":
			continue
		}
		if _, isKeyword := pageSizeKeywords[f]; isKeyword {
			keyword = f
			continue
		}
		if v, lok := parseAbsLengthPx(f); lok {
			lengths = append(lengths, v)
			continue
		}
		// Unrecognized token: fail the whole value (graceful — caller keeps default).
		return 0, 0, false
	}

	switch {
	case keyword != "":
		dims := pageSizeKeywords[keyword]
		w, h = dims[0], dims[1]
		if orientation == "landscape" {
			w, h = h, w // swap to wide
		}
		return w, h, true
	case len(lengths) == 1:
		return lengths[0], lengths[0], true // square
	case len(lengths) == 2:
		return lengths[0], lengths[1], true
	default:
		// orientation-only, or 0/3+ lengths: no usable size.
		return 0, 0, false
	}
}

// parsePageMarginShorthand parses an @page `margin` shorthand (1–4 absolute lengths,
// CSS box order top/right/bottom/left) into the four resolved edges in the layout
// scalar. ok is false if any component is not an absolute length (percentages on page
// margins are rare and treated as unsupported here → caller keeps defaults).
func parsePageMarginShorthand(value string) (top, right, bottom, left float64, ok bool) {
	t, r, b, l, ok := expandBox(strings.Fields(strings.TrimSpace(value)))
	if !ok {
		return 0, 0, 0, 0, false
	}
	vt, okT := parseAbsLengthPx(t)
	vr, okR := parseAbsLengthPx(r)
	vb, okB := parseAbsLengthPx(b)
	vl, okL := parseAbsLengthPx(l)
	if !okT || !okR || !okB || !okL {
		return 0, 0, 0, 0, false
	}
	return vt, vr, vb, vl, true
}

// parseAbsLengthPx parses a single absolute-length token (e.g. "2cm", "1in", "72pt",
// "20px", "0") into the layout px-as-pt scalar. Unlike the cascade's parseLength it
// resolves the physical print units (cm/mm/in/pt/pc) common in @page rules, since page
// geometry must reduce to a concrete scalar (there is no later resolution context for a
// page box). em/rem and percentages are not absolute → ok false. A bare "0" is 0.
func parseAbsLengthPx(tok string) (float64, bool) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return 0, false
	}
	// Split the numeric prefix from the unit suffix.
	i := len(tok)
	for j := 0; j < len(tok); j++ {
		c := tok[j]
		if (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-' {
			continue
		}
		i = j
		break
	}
	numStr, unit := tok[:i], strings.ToLower(tok[i:])
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, false
	}
	switch unit {
	case "px", "":
		// A unitless value is only valid when it is 0 (matches CSS / parseLength).
		if unit == "" && num != 0 {
			return 0, false
		}
		return num, true
	case "in":
		return num * pxPerIn, true
	case "cm":
		return num * pxPerCm, true
	case "mm":
		return num * pxPerMm, true
	case "pt":
		return num * pxPerPt, true
	case "pc":
		return num * pxPerPc, true
	}
	return 0, false
}
