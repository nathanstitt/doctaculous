package css

import "strings"

// BackgroundPos is a CSS background-position: an X and Y component, each a length or
// percentage (keywords are normalized to percentages: left/top→0%, center→50%,
// right/bottom→100%). The percentage is resolved late, against (paint-area − image)
// size per axis; a length is an absolute offset from the leading edge. The zero value
// is 0px 0px (Length's zero) — i.e. the top-left, matching the CSS initial 0% 0%; the
// cascade sets initialBackgroundPosition() explicitly regardless.
type BackgroundPos struct {
	X, Y Length
}

// BackgroundSizeKind selects how a background image is sized.
type BackgroundSizeKind int

const (
	// BgSizeAuto is the initial: each axis is the image's intrinsic size (a single
	// non-auto axis scales the other to preserve the intrinsic ratio).
	BgSizeAuto BackgroundSizeKind = iota
	// BgSizeCover scales the image (preserving ratio) to cover the origin box.
	BgSizeCover
	// BgSizeContain scales the image (preserving ratio) to fit inside the origin box.
	BgSizeContain
	// BgSizeExplicit sizes each axis from W/H (a length/percentage, or auto).
	BgSizeExplicit
)

// BackgroundSize is a CSS background-size. For BgSizeExplicit, W and H are the per-axis
// sizes (each may be UnitAuto). The zero value is BgSizeAuto.
type BackgroundSize struct {
	Kind BackgroundSizeKind
	W, H Length
}

// initialBackgroundPosition is the CSS initial background-position (0% 0%).
func initialBackgroundPosition() BackgroundPos {
	return BackgroundPos{X: Length{0, UnitPercent}, Y: Length{0, UnitPercent}}
}

// parseBackgroundPosition parses a CSS background-position into X/Y length components.
// It handles one or two space-separated components, each a keyword (left/center/right
// for X; top/center/bottom for Y), a percentage, or a length. A single component sets
// that axis and centers the other. Two keyword components may appear in either order
// (the X/Y keyword sets are disjoint); a length/percentage pair is X then Y. The 3- and
// 4-value edge-offset forms ("right 10px bottom 20px") are not parsed (ok=false, the
// caller keeps the initial). Lengths may be px/em/% (em resolved later by layout).
func parseBackgroundPosition(value string) (BackgroundPos, bool) {
	comps := splitComponents(value)
	if len(comps) == 0 || len(comps) > 2 {
		return BackgroundPos{}, false
	}
	// Default both axes to center; a single component overrides one and the other stays
	// centered (CSS background-position single-value rule).
	pos := BackgroundPos{X: pctLen(50), Y: pctLen(50)}
	var xSet, ySet bool // an explicit axis keyword (left/right, top/bottom) locked the axis

	// First pass: axis-locking keywords (left/right → X, top/bottom → Y). These bind
	// regardless of order, so "bottom right" and "right bottom" are equivalent.
	for _, c := range comps {
		switch strings.ToLower(c) {
		case "left":
			pos.X, xSet = pctLen(0), true
		case "right":
			pos.X, xSet = pctLen(100), true
		case "top":
			pos.Y, ySet = pctLen(0), true
		case "bottom":
			pos.Y, ySet = pctLen(100), true
		}
	}

	// Second pass: assign each non-axis-keyword value (a length/percentage, or "center")
	// to the next axis still free, in X-then-Y order — so "left 25%" puts 25% on Y, and
	// "center 10px" puts 10px on Y (the 2-value rule: first → X, second → Y, minus the
	// axes the keywords already claimed).
	apply := func(c string) bool {
		switch strings.ToLower(c) {
		case "left", "right", "top", "bottom":
			return true // already handled in the first pass
		case "center":
			if !xSet {
				xSet = true // center → 50%, the default already in pos
			} else {
				ySet = true
			}
			return true
		default:
			l, ok := parseLength(newTokenizer(c).next())
			if !ok || l.Unit == UnitAuto {
				return false
			}
			if !xSet {
				pos.X, xSet = l, true
			} else if !ySet {
				pos.Y, ySet = l, true
			} else {
				return false // a third value targeting an already-filled axis
			}
			return true
		}
	}
	for _, c := range comps {
		if !apply(c) {
			return BackgroundPos{}, false
		}
	}
	return pos, true
}

// parseBackgroundSize parses a CSS background-size: the keywords cover/contain, or one
// or two per-axis sizes (a length, percentage, or auto). A single explicit value sizes
// the width and leaves the height auto (CSS: the omitted axis is auto). ok=false leaves
// the caller's initial.
func parseBackgroundSize(value string) (BackgroundSize, bool) {
	comps := splitComponents(value)
	if len(comps) == 0 || len(comps) > 2 {
		return BackgroundSize{}, false
	}
	if len(comps) == 1 {
		switch strings.ToLower(comps[0]) {
		case "cover":
			return BackgroundSize{Kind: BgSizeCover}, true
		case "contain":
			return BackgroundSize{Kind: BgSizeContain}, true
		}
	}
	sz := BackgroundSize{Kind: BgSizeExplicit, W: Length{0, UnitAuto}, H: Length{0, UnitAuto}}
	axis := []*Length{&sz.W, &sz.H}
	for i, c := range comps {
		l, ok := parseLength(newTokenizer(c).next())
		if !ok {
			return BackgroundSize{}, false
		}
		*axis[i] = l
	}
	return sz, true
}

// parseBackgroundImage parses a CSS background-image value, returning the resolved
// url() reference. "none" yields ("", true) to clear the image. A gradient or other
// unsupported <image> (linear-gradient(...), image-set(...), etc.) yields ok=false so
// the caller leaves the prior value — gradients are a separate feature, not an error.
func parseBackgroundImage(value string) (ref string, ok bool) {
	v := strings.TrimSpace(value)
	if strings.EqualFold(v, "none") || v == "" {
		return "", true
	}
	// url() is case-insensitive (URL(...)/Url(...) are legal). takeFunc matches the
	// function name case-sensitively, so normalize just the leading "url(" prefix.
	if len(v) >= 4 && strings.EqualFold(v[:4], "url(") {
		if u, _, found := takeFunc("url("+v[4:], "url"); found {
			return unquote(strings.TrimSpace(u)), true
		}
	}
	return "", false
}

// pctLen builds a percentage Length.
func pctLen(percent float64) Length { return Length{percent, UnitPercent} }

// normalizeBoxValue maps a CSS background-origin/background-clip box keyword to its
// canonical form, returning ok=false for an unrecognized value.
func normalizeBoxValue(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "border-box":
		return "border-box", true
	case "padding-box":
		return "padding-box", true
	case "content-box":
		return "content-box", true
	}
	return "", false
}
