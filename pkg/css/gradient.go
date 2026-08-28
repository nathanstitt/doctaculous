package css

import (
	"image/color"
	"math"
	"strings"
)

// GradientKind distinguishes the <gradient> functions this package parses.
type GradientKind int

const (
	// GradientLinear is linear-gradient(), whose colour ramp runs along a
	// straight gradient line (CSS Images 3 §3.1).
	GradientLinear GradientKind = iota
	// GradientRadial is radial-gradient(), whose ramp runs outward from a
	// centre through a sized ellipse (CSS Images 3 §3.2).
	GradientRadial
)

// RadialShape selects a radial gradient's ending-shape keyword.
type RadialShape int

const (
	// RadialEllipse is the initial ending shape: an ellipse whose two radii are
	// sized independently per axis. `radial-gradient(...)` with no shape keyword
	// uses this unless a SINGLE <length> size is given, which forces a circle.
	RadialEllipse RadialShape = iota
	// RadialCircle is the `circle` keyword: one radius on both axes.
	RadialCircle
)

// RadialExtent is a radial gradient's ending-shape size keyword (CSS Images 3
// §3.2.1). Each names a rule for deriving the radii from the gradient box and
// the centre position, rather than stating a length.
type RadialExtent int

const (
	// ExtentFarthestCorner is the initial value: the ending shape passes
	// through the box corner farthest from the centre.
	ExtentFarthestCorner RadialExtent = iota
	// ExtentClosestSide: the shape meets the box side(s) nearest the centre.
	ExtentClosestSide
	// ExtentFarthestSide: the shape meets the box side(s) farthest from it.
	ExtentFarthestSide
	// ExtentClosestCorner: the shape passes through the nearest box corner.
	ExtentClosestCorner
	// ExtentExplicit means the radii come from ExplicitR1/ExplicitR2 (a
	// <length-percentage> size) rather than from a keyword rule.
	ExtentExplicit
)

// GradientStop is one parsed colour stop: a colour plus its OPTIONAL declared
// position. HasPos is false for a stop written with no position, which the CSS
// stop-normalization rules fill in by interpolation (see NormalizeStops) — a
// distinction that must survive parsing, because an omitted position and an
// explicit 0% behave differently in a list.
//
// Pos is kept as a Length (percentage or absolute) rather than a resolved
// fraction because a length stop ("black 20px") can only be resolved against the
// gradient LINE's length, which parsing does not know: the line's length depends
// on the gradient box, which is a layout-time fact.
type GradientStop struct {
	Color  color.RGBA
	Pos    Length
	HasPos bool
}

// Gradient is a parsed CSS <gradient> value: the geometry that selects the
// gradient line (or ending shape) plus the colour-stop list, all still in CSS
// terms. Resolving it into device geometry needs the gradient box's size, which
// only layout knows, so this type deliberately holds no pixels.
//
// Repeating reports the repeating-* variant, which reuses the identical geometry
// and stop list and differs only in what happens outside the [0,1] parameter
// range (repeat instead of pad).
type Gradient struct {
	Kind      GradientKind
	Repeating bool

	// AngleDeg is the linear gradient's angle in DEGREES measured per CSS: 0deg
	// points up (toward the top of the box) and angles increase CLOCKWISE, so
	// 90deg points right. This differs from the mathematical convention and from
	// SVG, and the conversion lives at the one place that builds the gradient
	// line (see GradientLine). Meaningful only when Kind is GradientLinear and
	// HasCorner is false.
	AngleDeg float64

	// CornerX, CornerY name a `to <side-or-corner>` target as a pair of unit
	// direction components in CSS box space (X right-positive, Y DOWN-positive,
	// matching page space): `to right` is (1,0), `to bottom` is (0,1), `to top
	// left` is (-1,-1). HasCorner selects this form over AngleDeg.
	//
	// A CORNER is kept as its two components rather than pre-converted to an
	// angle because the spec's corner rule is NOT "45 degrees": the gradient
	// line must be angled so that a line through the corner PERPENDICULAR to
	// the gradient line also passes through that corner — which depends on the
	// box's aspect ratio, a layout-time fact. See GradientLine.
	CornerX, CornerY float64
	HasCorner        bool

	// Shape, Extent, ExplicitR1/R2 and the centre describe a radial gradient's
	// ending shape. They are unused for GradientLinear.
	Shape  RadialShape
	Extent RadialExtent
	// ExplicitR1, ExplicitR2 are the horizontal and vertical radii for
	// ExtentExplicit. A circle repeats its single radius in both.
	ExplicitR1, ExplicitR2 Length
	// Center is the `at <position>` centre, defaulting to 50% 50%. It reuses
	// BackgroundPos because `at <position>` is the SAME <position> grammar
	// background-position uses, so sharing the parser keeps the two from
	// drifting.
	Center BackgroundPos

	Stops []GradientStop
}

// parseGradient parses a complete <gradient> value — linear-gradient(),
// radial-gradient(), or either's repeating-* form — reporting ok=false for
// anything else, including a gradient function this engine does not implement
// (conic-gradient()). A malformed argument list also yields ok=false so the
// caller drops the declaration rather than painting something invented.
func parseGradient(value string) (*Gradient, bool) {
	v := strings.TrimSpace(value)
	// The function name is case-insensitive (LINEAR-GRADIENT(...) is legal), but
	// takeFunc matches it case-sensitively, so match the prefix case-insensitively
	// and hand takeFunc a normalized copy — the same trick parseBackgroundImage
	// uses for url().
	for _, cand := range []struct {
		name      string
		kind      GradientKind
		repeating bool
	}{
		{"repeating-linear-gradient", GradientLinear, true},
		{"repeating-radial-gradient", GradientRadial, true},
		{"linear-gradient", GradientLinear, false},
		{"radial-gradient", GradientRadial, false},
	} {
		prefix := cand.name + "("
		if len(v) < len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
			continue
		}
		arg, rest, found := takeFunc(cand.name+"("+v[len(prefix):], cand.name)
		if !found || strings.TrimSpace(rest) != "" {
			// Trailing text after the closing paren is not a bare <gradient>.
			return nil, false
		}
		switch cand.kind {
		case GradientLinear:
			return parseLinearGradient(arg, cand.repeating)
		default:
			return parseRadialGradient(arg, cand.repeating)
		}
	}
	return nil, false
}

// parseLinearGradient parses the inside of a linear-gradient() / repeating-
// linear-gradient(): an optional leading `<angle>` or `to <side-or-corner>`,
// then a comma-separated colour-stop list of at least two entries.
//
// CSS Images 3 requires at least two stops; a one-stop list is invalid, and
// rejecting it here (rather than painting a solid colour) keeps an authoring
// typo visible instead of silently rendering something plausible.
func parseLinearGradient(arg string, repeating bool) (*Gradient, bool) {
	parts := splitTopLevel(arg, ',')
	if len(parts) < 2 {
		return nil, false
	}
	g := &Gradient{Kind: GradientLinear, Repeating: repeating}

	// The direction is optional. Try the first comma-separated part as one; if it
	// does not parse as a direction it must be the first colour stop, so leave the
	// default (`to bottom`, i.e. 180deg) in place and parse every part as a stop.
	stopParts := parts
	if dirAngle, cx, cy, hasCorner, ok := parseLinearDirection(parts[0]); ok {
		g.AngleDeg, g.CornerX, g.CornerY, g.HasCorner = dirAngle, cx, cy, hasCorner
		stopParts = parts[1:]
		if len(stopParts) < 2 {
			return nil, false
		}
	} else {
		g.AngleDeg = 180 // `to bottom` is the CSS initial direction
	}

	stops, ok := parseColorStopList(stopParts)
	if !ok {
		return nil, false
	}
	g.Stops = stops
	return g, true
}

// parseLinearDirection parses a linear gradient's optional direction: an
// <angle> ("45deg", "0.25turn", "1rad", "100grad"), or `to <side-or-corner>`.
// ok=false means the text is not a direction at all (so the caller treats it as
// the first colour stop).
//
// The two forms are returned separately — an angle as degrees, a side/corner as
// its unit direction components with hasCorner=true — because a CORNER cannot be
// reduced to a fixed angle without knowing the box's aspect ratio (see
// Gradient.CornerX). A SIDE, whose angle is aspect-independent, is normalized to
// degrees here so the common case needs no special handling downstream.
func parseLinearDirection(s string) (angleDeg, cornerX, cornerY float64, hasCorner, ok bool) {
	comps := splitComponents(strings.TrimSpace(s))
	if len(comps) == 0 {
		return 0, 0, 0, false, false
	}
	if strings.EqualFold(comps[0], "to") {
		if len(comps) < 2 || len(comps) > 3 {
			return 0, 0, 0, false, false
		}
		var x, y float64
		var sawX, sawY bool
		for _, c := range comps[1:] {
			switch strings.ToLower(c) {
			case "left":
				if sawX {
					return 0, 0, 0, false, false
				}
				x, sawX = -1, true
			case "right":
				if sawX {
					return 0, 0, 0, false, false
				}
				x, sawX = 1, true
			case "top":
				if sawY {
					return 0, 0, 0, false, false
				}
				y, sawY = -1, true
			case "bottom":
				if sawY {
					return 0, 0, 0, false, false
				}
				y, sawY = 1, true
			default:
				return 0, 0, 0, false, false
			}
		}
		if sawX && sawY {
			return 0, x, y, true, true // a true corner: aspect-dependent, resolved later
		}
		// A single side. Its angle is fixed regardless of the box's shape, so
		// convert now: CSS angles are clockwise from "up", and CornerY is
		// Y-DOWN, so `to top` (y=-1) is 0deg and `to bottom` (y=+1) is 180deg.
		switch {
		case sawX && x > 0:
			return 90, 0, 0, false, true // to right
		case sawX:
			return 270, 0, 0, false, true // to left
		case y < 0:
			return 0, 0, 0, false, true // to top
		default:
			return 180, 0, 0, false, true // to bottom
		}
	}
	if len(comps) != 1 {
		return 0, 0, 0, false, false
	}
	deg, ok := parseAngleDeg(comps[0])
	if !ok {
		return 0, 0, 0, false, false
	}
	return deg, 0, 0, false, true
}

// parseAngleDeg parses a CSS <angle> into degrees, accepting all four CSS units
// (deg, grad, rad, turn). A bare unitless number is NOT an angle in CSS (only
// zero is, and even that only by the <zero> grammar quirk) — accepting one would
// make `linear-gradient(45, red, blue)` paint instead of being dropped, so it is
// rejected here along with every other non-angle token.
func parseAngleDeg(s string) (float64, bool) {
	tok := newTokenizer(strings.TrimSpace(s)).next()
	if tok.Kind != TokenDimension {
		return 0, false
	}
	switch strings.ToLower(tok.Unit) {
	case "deg":
		return tok.Num, true
	case "grad":
		return tok.Num * 360 / 400, true
	case "rad":
		return tok.Num * 180 / math.Pi, true
	case "turn":
		return tok.Num * 360, true
	}
	return 0, false
}

// parseColorStopList parses the comma-separated <color-stop-list>. Each part is
// a colour with an optional position ("red", "red 50%", "red 20px"), or the
// two-position shorthand ("red 20% 80%", CSS Images 4) which expands to two
// stops of the same colour — the form that makes a hard colour band easy to
// write. At least two stops are required.
//
// A bare <length-percentage> with no colour (a "colour hint", which requests a
// non-linear interpolation midpoint) is deliberately NOT supported: honouring it
// requires a non-linear ramp the shared shading seam cannot express, and
// treating it as a stop would silently shift every colour around it. It is
// rejected (ok=false) so the whole declaration is dropped and the author sees
// nothing paint, rather than a subtly wrong ramp.
func parseColorStopList(parts []string) ([]GradientStop, bool) {
	var stops []GradientStop
	for _, p := range parts {
		comps := splitComponents(strings.TrimSpace(p))
		if len(comps) == 0 || len(comps) > 3 {
			return nil, false
		}
		c, ok := parseColor(newTokenizer(comps[0]))
		if !ok {
			return nil, false
		}
		if len(comps) == 1 {
			stops = append(stops, GradientStop{Color: c})
			continue
		}
		for _, posText := range comps[1:] {
			l, ok := parseLength(newTokenizer(posText).next())
			if !ok || l.Unit == UnitAuto || l.Unit == UnitContent {
				return nil, false
			}
			stops = append(stops, GradientStop{Color: c, Pos: l, HasPos: true})
		}
	}
	if len(stops) < 2 {
		return nil, false
	}
	return stops, true
}

// parseRadialGradient parses the inside of a radial-gradient() / repeating-
// radial-gradient(): an optional leading `[<ending-shape> || <size>] [at
// <position>]?`, then a comma-separated colour-stop list of at least two
// entries.
func parseRadialGradient(arg string, repeating bool) (*Gradient, bool) {
	parts := splitTopLevel(arg, ',')
	if len(parts) < 2 {
		return nil, false
	}
	g := &Gradient{
		Kind:      GradientRadial,
		Repeating: repeating,
		Shape:     RadialEllipse,
		Extent:    ExtentFarthestCorner,
		Center:    BackgroundPos{X: pctLen(50), Y: pctLen(50)},
	}

	stopParts := parts
	if ok := parseRadialPrelude(parts[0], g); ok {
		stopParts = parts[1:]
		if len(stopParts) < 2 {
			return nil, false
		}
	}

	stops, ok := parseColorStopList(stopParts)
	if !ok {
		return nil, false
	}
	g.Stops = stops
	return g, true
}

// parseRadialPrelude parses a radial gradient's leading geometry clause into g,
// returning false when the text is not a prelude at all (so the caller treats it
// as the first colour stop instead).
//
// The grammar is `[<ending-shape> || <length-percentage>{1,2}] [at <position>]?`
// — the shape keyword and the size may appear in either order, and either may be
// omitted. It is parsed by scanning components up to an `at`, classifying each,
// then handing everything after `at` to the shared <position> parser.
func parseRadialPrelude(s string, g *Gradient) bool {
	comps := splitComponents(strings.TrimSpace(s))
	if len(comps) == 0 {
		return false
	}

	// Split the prelude at the `at` keyword; everything after it is a <position>.
	geom := comps
	var posComps []string
	for i, c := range comps {
		if strings.EqualFold(c, "at") {
			geom, posComps = comps[:i], comps[i+1:]
			if len(posComps) == 0 {
				return false // a dangling `at` with no position is invalid
			}
			break
		}
	}

	var shapeSet, extentSet bool
	var sizes []Length
	for _, c := range geom {
		switch strings.ToLower(c) {
		case "circle":
			if shapeSet {
				return false
			}
			g.Shape, shapeSet = RadialCircle, true
		case "ellipse":
			if shapeSet {
				return false
			}
			g.Shape, shapeSet = RadialEllipse, true
		case "closest-side":
			if extentSet {
				return false
			}
			g.Extent, extentSet = ExtentClosestSide, true
		case "closest-corner":
			if extentSet {
				return false
			}
			g.Extent, extentSet = ExtentClosestCorner, true
		case "farthest-side":
			if extentSet {
				return false
			}
			g.Extent, extentSet = ExtentFarthestSide, true
		case "farthest-corner":
			if extentSet {
				return false
			}
			g.Extent, extentSet = ExtentFarthestCorner, true
		default:
			l, ok := parseLength(newTokenizer(c).next())
			if !ok || l.Unit == UnitAuto || l.Unit == UnitContent {
				return false
			}
			if l.Value < 0 {
				return false // a negative radius is invalid
			}
			sizes = append(sizes, l)
		}
	}
	if len(sizes) > 0 {
		if extentSet || len(sizes) > 2 {
			return false // an explicit size and an extent keyword are mutually exclusive
		}
		g.Extent = ExtentExplicit
		switch len(sizes) {
		case 1:
			// A SINGLE length is only legal for a circle, and per CSS it also
			// IMPLIES `circle` when no shape keyword was given: an ellipse needs
			// two radii. A percentage is likewise illegal for a circle radius
			// (there is no single axis to resolve it against).
			if g.Shape == RadialEllipse && shapeSet {
				return false
			}
			if sizes[0].Unit == UnitPercent {
				return false
			}
			g.Shape = RadialCircle
			g.ExplicitR1, g.ExplicitR2 = sizes[0], sizes[0]
		case 2:
			// Two lengths are only legal for an ellipse, and imply it.
			if g.Shape == RadialCircle {
				return false
			}
			g.Shape = RadialEllipse
			g.ExplicitR1, g.ExplicitR2 = sizes[0], sizes[1]
		}
	}

	if len(posComps) > 0 {
		p, ok := parseBackgroundPosition(strings.Join(posComps, " "))
		if !ok {
			return false
		}
		g.Center = p
	}

	// A prelude that matched nothing at all (no shape, no extent, no size, no
	// position) is not a prelude — it was the first colour stop.
	return shapeSet || extentSet || len(sizes) > 0 || len(posComps) > 0
}

// ResolvedStop is a colour stop after CSS's stop-position normalization: a
// position as a FRACTION of the gradient line (0 = its start, 1 = its end) and
// the stop's colour. Positions are non-decreasing across a resolved list.
//
// A fraction may legitimately fall outside [0,1] (`linear-gradient(red -20%,
// blue 120%)` is valid and simply pushes the ramp past the line's ends), which
// is why this is a plain float rather than a clamped value.
type ResolvedStop struct {
	Pos   float64
	Color color.RGBA
}

// NormalizeStops turns a parsed stop list into resolved fractional positions
// along a gradient line of lineLen units, implementing CSS Images 3 §3.4.3's
// three rules in the order the spec states them:
//
//  1. If the first stop has no position it gets 0%; if the last has none it
//     gets 100%.
//  2. Any stop whose position is less than the largest position before it is
//     CORRECTED UP to that largest value. This is a forward clamp, NOT a sort:
//     `red 40%, blue 20%` must render red-to-blue with a hard break at 40%, not
//     a reversed ramp — sorting would silently reorder the author's colours.
//  3. A run of stops with no position is spread EVENLY between the positioned
//     stops surrounding it.
//
// lineLen is the gradient line's length in the same units a Length resolves to,
// needed to turn an absolute stop position ("black 20px") into a fraction. A
// non-positive lineLen makes every absolute position degenerate; it is treated
// as 0 so the result stays finite (the caller has already declined to paint a
// zero-area box, so this is a defensive floor rather than a reachable case).
//
// fontSizePt resolves an em position, matching how every other Length in this
// engine resolves em.
func NormalizeStops(stops []GradientStop, lineLen, fontSizePt float64) []ResolvedStop {
	if len(stops) == 0 {
		return nil
	}
	out := make([]ResolvedStop, len(stops))
	has := make([]bool, len(stops))
	for i, s := range stops {
		out[i].Color = s.Color
		if !s.HasPos {
			continue
		}
		out[i].Pos, has[i] = stopFraction(s.Pos, lineLen, fontSizePt), true
	}

	// Rule 1: the endpoints always have a position.
	if !has[0] {
		out[0].Pos, has[0] = 0, true
	}
	if last := len(out) - 1; !has[last] {
		out[last].Pos, has[last] = 1, true
	}

	// Rule 2: correct any decreasing position up to the running maximum. Done
	// BEFORE rule 3 so an unpositioned run interpolates between the corrected
	// endpoints, which is what makes `red, blue 40%, green 20%, yellow` place
	// green at 40% rather than spreading toward a position that no longer exists.
	maxSoFar := out[0].Pos
	for i := range out {
		if !has[i] {
			continue
		}
		if out[i].Pos < maxSoFar {
			out[i].Pos = maxSoFar
		}
		maxSoFar = out[i].Pos
	}

	// Rule 3: spread each unpositioned run evenly between its bracketing stops.
	// Both brackets always exist by rule 1, so the scan never runs off either end.
	for i := 0; i < len(out); i++ {
		if has[i] {
			continue
		}
		j := i
		for j < len(out) && !has[j] {
			j++
		}
		// out[i-1] and out[j] are positioned; the run is out[i..j-1].
		lo, hi := out[i-1].Pos, out[j].Pos
		steps := float64(j - i + 1)
		for k := i; k < j; k++ {
			out[k].Pos = lo + (hi-lo)*float64(k-i+1)/steps
			has[k] = true
		}
		i = j - 1
	}
	return out
}

// GradientLine computes a linear gradient's gradient line for a gradient box of
// boxW x boxH, per CSS Images 3 §3.1: the start and end points, in box-local
// coordinates (origin at the box's top-left, Y DOWN — page-space convention),
// and the line's length.
//
// The line passes through the box's CENTRE at the gradient's angle. Its two ends
// are placed so that a line perpendicular to the gradient line through the start
// point touches the box corner nearest the start, and likewise for the end: that
// is what makes the first stop's colour appear exactly at one corner and the
// last stop's at the opposite one, with no colour "wasted" outside the box.
//
// Concretely, with the unit direction (dx,dy) the half-length is the projection
// of the box's half-diagonal onto that direction, |W/2·dx| + |H/2·dy| — the
// absolute values being what makes the formula correct in all four quadrants.
//
// A `to <corner>` gradient does NOT use 45 degrees. The spec instead requires
// the gradient line be angled so that the PERPENDICULAR through the corner also
// passes through the corner's neighbouring corners — equivalently, the gradient
// line direction is perpendicular to the box's OTHER diagonal. For a box of
// W x H heading to the bottom-right, the other diagonal runs from the
// bottom-left to the top-right with direction (W, -H), so the gradient direction
// is (H, W) normalized. Using 45 degrees instead is right only for a square and
// visibly wrong for any other aspect ratio — the specific bug this comment
// exists to prevent someone from reintroducing.
func (g *Gradient) GradientLine(boxW, boxH float64) (x0, y0, x1, y1, length float64) {
	dx, dy := g.lineDirection(boxW, boxH)
	cx, cy := boxW/2, boxH/2
	half := math.Abs(boxW/2*dx) + math.Abs(boxH/2*dy)
	return cx - dx*half, cy - dy*half, cx + dx*half, cy + dy*half, 2 * half
}

// lineDirection returns the linear gradient's unit direction vector in box space
// (X right, Y DOWN), the direction of INCREASING gradient position.
func (g *Gradient) lineDirection(boxW, boxH float64) (dx, dy float64) {
	if g.HasCorner {
		// Perpendicular to the box's other diagonal — see GradientLine. The
		// corner components are ±1, and multiplying the perpendicular by them
		// selects the right quadrant: heading to (+1,+1) (bottom-right) gives
		// (H, W) normalized, to (-1,+1) (bottom-left) gives (-H, W), and so on.
		vx, vy := g.CornerX*boxH, g.CornerY*boxW
		n := math.Hypot(vx, vy)
		if n == 0 {
			// A degenerate box (zero width AND height). Nothing can paint at
			// that size, but keep the direction finite rather than NaN.
			return 0, 1
		}
		return vx / n, vy / n
	}
	// CSS angles are clockwise from "up". With Y pointing DOWN, "up" is (0,-1)
	// and a clockwise rotation by θ maps it to (sin θ, -cos θ).
	rad := g.AngleDeg * math.Pi / 180
	return math.Sin(rad), -math.Cos(rad)
}

// RadialRadii computes a radial gradient's ending-shape radii for a gradient box
// of boxW x boxH with the centre at box-local (cx,cy), per CSS Images 3 §3.2.1.
// The returned radii are the horizontal and vertical semi-axes of the ending
// ellipse (equal for a circle).
//
// fontSizePt resolves an em radius. A degenerate result (either radius ≤ 0) is
// possible for a zero-size box or a zero explicit radius, and the caller must
// decline to paint rather than divide by it.
func (g *Gradient) RadialRadii(boxW, boxH, cx, cy, fontSizePt float64) (rx, ry float64) {
	// Distances from the centre to each side, and the extremes among them.
	left, right := cx, boxW-cx
	top, bottom := cy, boxH-cy
	nearX, farX := math.Min(math.Abs(left), math.Abs(right)), math.Max(math.Abs(left), math.Abs(right))
	nearY, farY := math.Min(math.Abs(top), math.Abs(bottom)), math.Max(math.Abs(top), math.Abs(bottom))

	switch g.Extent {
	case ExtentExplicit:
		rx = resolveRadius(g.ExplicitR1, boxW, fontSizePt)
		ry = resolveRadius(g.ExplicitR2, boxH, fontSizePt)
		return rx, ry

	case ExtentClosestSide:
		if g.Shape == RadialCircle {
			r := math.Min(nearX, nearY)
			return r, r
		}
		return nearX, nearY

	case ExtentFarthestSide:
		if g.Shape == RadialCircle {
			r := math.Max(farX, farY)
			return r, r
		}
		return farX, farY

	case ExtentClosestCorner:
		if g.Shape == RadialCircle {
			r := math.Hypot(nearX, nearY)
			return r, r
		}
		// An ellipse through a corner keeps the closest-SIDE aspect ratio and is
		// scaled up until it passes through the corner (CSS Images 3: "has the
		// same aspect ratio it would have if closest-side were specified").
		return cornerEllipse(nearX, nearY)

	default: // ExtentFarthestCorner, the initial value
		if g.Shape == RadialCircle {
			r := math.Hypot(farX, farY)
			return r, r
		}
		return cornerEllipse(farX, farY)
	}
}

// cornerEllipse sizes an ellipse that keeps the sideX:sideY aspect ratio and
// passes exactly through the corner at (sideX, sideY). Substituting a scaled
// (k·sideX, k·sideY) into the ellipse equation x²/rx² + y²/ry² = 1 with
// rx = k·sideX, ry = k·sideY gives k = √2, which is the closed form CSS's
// "same aspect ratio as closest-side, scaled to reach the corner" rule reduces
// to. Handling the degenerate zero-side case explicitly keeps the result finite.
func cornerEllipse(sideX, sideY float64) (rx, ry float64) {
	if sideX == 0 || sideY == 0 {
		return sideX, sideY
	}
	return sideX * math.Sqrt2, sideY * math.Sqrt2
}

// resolveRadius resolves an explicit radial-gradient radius against the box axis
// it is measured along.
func resolveRadius(l Length, axis, fontSizePt float64) float64 {
	switch l.Unit {
	case UnitPercent:
		return l.Value / 100 * axis
	case UnitEm:
		return l.Value * fontSizePt
	default:
		return l.Value
	}
}

// stopFraction converts one stop position to a fraction of a gradient line of
// lineLen units. A percentage is the fraction directly; any other unit is an
// absolute length divided by the line's length.
func stopFraction(l Length, lineLen, fontSizePt float64) float64 {
	if l.Unit == UnitPercent {
		return l.Value / 100
	}
	if lineLen <= 0 {
		return 0
	}
	var abs float64
	switch l.Unit {
	case UnitEm:
		abs = l.Value * fontSizePt
	default:
		// px and pt are treated 1:1 throughout this engine (see ComputedStyle's
		// FontSizePt comment); keeping that identity here means a gradient stop
		// lands where a padding of the same length would.
		abs = l.Value
	}
	return abs / lineLen
}
