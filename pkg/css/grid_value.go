package css

import "strings"

// trackKind is the kind of a single track sizing function component.
type trackKind int

const (
	trackLength     trackKind = iota // a fixed <length>/<percentage> (Len holds it)
	trackFlex                        // a <flex> value (Fr holds the factor), e.g. 1fr
	trackAuto                        // the `auto` keyword
	trackMinContent                  // the `min-content` keyword
	trackMaxContent                  // the `max-content` keyword
)

// SizingFn is one side (min or max) of a track sizing function.
type SizingFn struct {
	Kind trackKind
	Len  Length  // for trackLength
	Fr   float64 // for trackFlex
}

// TrackSize is a resolved single track: minmax(Min, Max). A bare function f sets
// both Min and Max to f, EXCEPT a bare flex `Nfr` whose Min is auto and Max is the
// flex value (CSS Grid §7.2.3: a flexible track's min sizing function is auto), and
// a bare `auto`/min-content/max-content which sets both sides to that keyword.
type TrackSize struct {
	Min, Max SizingFn
}

// repeatKind distinguishes a fixed-count repeat from the auto-repeat forms.
type repeatKind int

const (
	repeatFixed    repeatKind = iota // repeat(N, …)
	repeatAutoFill                   // repeat(auto-fill, …)
	repeatAutoFit                    // repeat(auto-fit, …)
)

// trackEntry is one element of a parsed track list: either a single track, or a
// repeat() run holding an inner track list. Exactly one of Single/Repeat applies.
type trackEntry struct {
	isRepeat bool
	single   TrackSize
	rep      repeatRun
}

type repeatRun struct {
	kind  repeatKind
	count int         // for repeatFixed
	inner []TrackSize // the inner track list (already flattened — no nested repeats)
}

// TrackList is a parsed grid-template-columns/-rows value: an ordered list of
// entries, expanded to concrete tracks at layout time (auto-repeat needs the
// container size). The zero value is an empty list (display:grid with no template
// => all-implicit tracks).
type TrackList struct {
	entries []trackEntry
}

// IsEmpty reports whether the track list defines no explicit tracks.
func (tl TrackList) IsEmpty() bool { return len(tl.entries) == 0 }

// Expand returns the concrete explicit tracks. containerSize is the container's
// definite content size on this axis (px) for resolving auto-fill/auto-fit; pass 0
// when indefinite (auto-repeat then yields 1 repetition, the spec fallback). gap is
// ignored here (the caller folds gaps into the repeat-count math via ExpandGap);
// Expand assumes 0 gap and is the common path used by fixed lists + tests.
func (tl TrackList) Expand(containerSize float64) []TrackSize {
	return tl.ExpandGap(containerSize, 0)
}

// ExpandGap is Expand with an explicit inter-track gap used only by the auto-repeat
// count computation (CSS Grid §7.2.3.1: the repeat count fills the container with
// repetitions + gaps). Fixed lists ignore both arguments.
func (tl TrackList) ExpandGap(containerSize, gap float64) []TrackSize {
	var out []TrackSize
	for _, e := range tl.entries {
		if !e.isRepeat {
			out = append(out, e.single)
			continue
		}
		switch e.rep.kind {
		case repeatFixed:
			for i := 0; i < e.rep.count; i++ {
				out = append(out, e.rep.inner...)
			}
		default: // auto-fill / auto-fit
			n := autoRepeatCount(e.rep.inner, containerSize, gap)
			for i := 0; i < n; i++ {
				out = append(out, e.rep.inner...)
			}
		}
	}
	return out
}

// autoRepeatCount computes how many repetitions of inner fit in containerSize with
// gaps between every track. Indefinite (size <= 0) => 1. A track with no definite
// base contributes its minmax-min length when fixed, else 0 (degrades — auto-repeat
// of an intrinsic track is uncommon; 1 repetition then).
func autoRepeatCount(inner []TrackSize, containerSize, gap float64) int {
	if containerSize <= 0 {
		return 1
	}
	sum := 0.0
	for _, t := range inner {
		sum += fixedBase(t)
	}
	if sum <= 0 {
		return 1
	}
	// n repetitions occupy n*sum + (n*len(inner)-1)*gap (len(inner) tracks per rep).
	// Solve the largest n with n*sum + (n*len(inner)-1)*gap <= containerSize. Use a
	// simple forward count (track counts are small); avoids float division edge cases.
	n := 0
	for {
		tracks := (n + 1) * len(inner)
		used := float64(n+1)*sum + float64(tracks-1)*gap
		if used > containerSize+0.001 {
			break
		}
		n++
	}
	if n < 1 {
		n = 1
	}
	return n
}

// fixedBase returns a track's fixed base length in px if it has one (length min
// sizing function in px/pt), else 0. Percentages and intrinsic functions are not
// fixed for the auto-repeat count (return 0 => the list degrades to 1 repetition).
func fixedBase(t TrackSize) float64 {
	if t.Min.Kind == trackLength && (t.Min.Len.Unit == UnitPx || t.Min.Len.Unit == UnitPt) {
		return t.Min.Len.Value
	}
	return 0
}

// parseTrackList parses a grid-template-columns/-rows value into a TrackList. ok is
// false (and the caller keeps the default empty list) for any unparseable input.
// Line-name tokens ([name]) are not produced by the tokenizer's bracket handling in
// this slice and are skipped if present (deferred — see spec Degradation).
func parseTrackList(s string) (TrackList, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "none" {
		return TrackList{}, false
	}
	if s == "subgrid" {
		return TrackList{}, false // subgrid deferred — caller logs; acts as none
	}
	tz := newTokenizer(s)
	var tl TrackList
	for {
		t := nextNonWhitespace(tz)
		if t.Kind == TokenEOF {
			break
		}
		entry, ok := parseTrackEntry(tz, t)
		if !ok {
			return TrackList{}, false
		}
		tl.entries = append(tl.entries, entry)
	}
	if len(tl.entries) == 0 {
		return TrackList{}, false
	}
	return tl, true
}

// parseTrackEntry parses one entry (a single track or a repeat()) given its first
// token (already consumed).
func parseTrackEntry(tz *tokenizer, first Token) (trackEntry, bool) {
	if first.Kind == TokenIdent && strings.EqualFold(first.Text, "repeat") {
		rep, ok := parseRepeat(tz)
		return trackEntry{isRepeat: true, rep: rep}, ok
	}
	ts, ok := parseTrackSize(tz, first)
	return trackEntry{single: ts}, ok
}

// parseTrackSize parses one track sizing function (a length/%, fr, keyword, or
// minmax()) given its first token.
func parseTrackSize(tz *tokenizer, first Token) (TrackSize, bool) {
	if first.Kind == TokenIdent && strings.EqualFold(first.Text, "minmax") {
		return parseMinmax(tz)
	}
	fn, ok := parseSizingFn(first)
	if !ok {
		return TrackSize{}, false
	}
	// A bare flex value: min = auto, max = the flex (CSS Grid §7.2.3).
	if fn.Kind == trackFlex {
		return TrackSize{Min: SizingFn{Kind: trackAuto}, Max: fn}, true
	}
	return TrackSize{Min: fn, Max: fn}, true
}

// parseSizingFn parses a single non-minmax sizing function from one token.
func parseSizingFn(t Token) (SizingFn, bool) {
	switch t.Kind {
	case TokenDimension:
		if strings.EqualFold(t.Unit, "fr") {
			if t.Num < 0 {
				return SizingFn{}, false
			}
			return SizingFn{Kind: trackFlex, Fr: t.Num}, true
		}
		l, ok := parseLength(t)
		if !ok {
			return SizingFn{}, false
		}
		return SizingFn{Kind: trackLength, Len: l}, true
	case TokenPercent:
		return SizingFn{Kind: trackLength, Len: Length{t.Num, UnitPercent}}, true
	case TokenNumber:
		if t.Num == 0 {
			return SizingFn{Kind: trackLength, Len: Length{0, UnitPx}}, true
		}
		return SizingFn{}, false
	case TokenIdent:
		switch strings.ToLower(t.Text) {
		case "auto":
			return SizingFn{Kind: trackAuto}, true
		case "min-content":
			return SizingFn{Kind: trackMinContent}, true
		case "max-content":
			return SizingFn{Kind: trackMaxContent}, true
		}
	}
	return SizingFn{}, false
}

// parseMinmax parses the remainder of minmax(min, max) with tz positioned after the
// "minmax" ident (the "(" not yet consumed).
func parseMinmax(tz *tokenizer) (TrackSize, bool) {
	if nextNonWhitespace(tz).Kind != TokenLParen {
		return TrackSize{}, false
	}
	minFn, ok := parseSizingFn(nextNonWhitespace(tz))
	if !ok {
		return TrackSize{}, false
	}
	if nextNonWhitespace(tz).Kind != TokenComma {
		return TrackSize{}, false
	}
	maxFn, ok := parseSizingFn(nextNonWhitespace(tz))
	if !ok {
		return TrackSize{}, false
	}
	if nextNonWhitespace(tz).Kind != TokenRParen {
		return TrackSize{}, false
	}
	// A flex min sizing function is invalid in minmax() (CSS Grid §7.2.3); coerce to
	// auto rather than rejecting the whole declaration (graceful).
	if minFn.Kind == trackFlex {
		minFn = SizingFn{Kind: trackAuto}
	}
	return TrackSize{Min: minFn, Max: maxFn}, true
}

// GridAreas is a parsed grid-template-areas value: a map from area name to its
// rectangle (1-based grid line numbers, end-inclusive cell numbers). Rows is the
// number of string rows; Cols the number of columns. The zero value has no areas.
type GridAreas struct {
	Named map[string]GridRect
	Rows  int
	Cols  int
}

// GridRect is a 1-based, inclusive cell rectangle in the named grid.
// RowStart..RowEnd and ColStart..ColEnd are the first and last cells (inclusive).
type GridRect struct{ RowStart, RowEnd, ColStart, ColEnd int }

// lineKind classifies a grid placement endpoint.
type lineKind int

const (
	lineAuto lineKind = iota // `auto`
	lineNum                  // an explicit (possibly negative) line number
	lineSpan                 // `span <n>`
	lineName                 // a named line / area-name endpoint (resolved at layout)
)

// GridLine is one endpoint of grid-column/grid-row (start or end).
type GridLine struct {
	Kind lineKind
	N    int    // line number (lineNum) or span count (lineSpan)
	Name string // lineName (area name or named line — area names resolve in placement)
}

// GridPlacement is an item's full placement: the four resolved endpoints plus an
// optional area name (from `grid-area: name`). AreaName != "" means "place by the
// named area if it exists, else fall through to the endpoints/auto".
type GridPlacement struct {
	ColStart, ColEnd, RowStart, RowEnd GridLine
	AreaName                           string
}

// parseTemplateAreas parses a grid-template-areas value (a sequence of quoted
// strings) into a GridAreas. ok is false if the input is empty, rows have
// differing column counts (ragged), or any named area is non-rectangular.
func parseTemplateAreas(s string) (GridAreas, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "none" {
		return GridAreas{}, false
	}
	tz := newTokenizer(s)

	// Collect rows: each row is a slice of cell names (empty string = null cell).
	var rows [][]string
	for {
		tok := nextNonWhitespace(tz)
		if tok.Kind == TokenEOF {
			break
		}
		if tok.Kind != TokenString {
			return GridAreas{}, false
		}
		// Split the string value on whitespace to get cell names.
		cells := strings.Fields(tok.Text)
		if len(cells) == 0 {
			return GridAreas{}, false
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return GridAreas{}, false
	}

	// Validate: all rows must have the same column count (not ragged).
	cols := len(rows[0])
	for _, row := range rows[1:] {
		if len(row) != cols {
			return GridAreas{}, false
		}
	}

	numRows := len(rows)

	// Build the bounding box for each named area and track cell counts.
	type rectInfo struct {
		rect  GridRect
		count int // number of cells with this name
	}
	info := make(map[string]*rectInfo)
	for r, row := range rows {
		for c, name := range row {
			if strings.TrimLeft(name, ".") == "" {
				continue // null cell — skip (CSS §7.3: one or more "." characters)
			}
			ri, exists := info[name]
			if !exists {
				ri = &rectInfo{rect: GridRect{
					RowStart: r + 1, RowEnd: r + 1,
					ColStart: c + 1, ColEnd: c + 1,
				}}
				info[name] = ri
			} else {
				if r+1 < ri.rect.RowStart {
					ri.rect.RowStart = r + 1
				}
				if r+1 > ri.rect.RowEnd {
					ri.rect.RowEnd = r + 1
				}
				if c+1 < ri.rect.ColStart {
					ri.rect.ColStart = c + 1
				}
				if c+1 > ri.rect.ColEnd {
					ri.rect.ColEnd = c + 1
				}
			}
			ri.count++
		}
	}

	// Verify rectangularity: every name's bounding box must be fully occupied by
	// that name, and the bounding box area must equal the count.
	for name, ri := range info {
		boxArea := (ri.rect.RowEnd - ri.rect.RowStart + 1) * (ri.rect.ColEnd - ri.rect.ColStart + 1)
		if boxArea != ri.count {
			return GridAreas{}, false
		}
		// Defensive cross-check: the area==count test above already implies
		// rectangular contiguity; this scan guards against any future change
		// to that invariant.
		for r := ri.rect.RowStart - 1; r < ri.rect.RowEnd; r++ {
			for c := ri.rect.ColStart - 1; c < ri.rect.ColEnd; c++ {
				if rows[r][c] != name {
					return GridAreas{}, false
				}
			}
		}
	}

	named := make(map[string]GridRect, len(info))
	for name, ri := range info {
		named[name] = ri.rect
	}
	return GridAreas{Named: named, Rows: numRows, Cols: cols}, true
}

// parseGridLine parses one grid-line endpoint value. The recognized forms are:
// `auto` → lineAuto; `span N` / `span` → lineSpan; a non-zero integer → lineNum;
// an identifier → lineName. ok is false only for truly unrecognizable input.
// Invalid span counts (zero or negative) are clamped to 1 rather than failing,
// per the project's graceful-degradation policy.
func parseGridLine(s string) (GridLine, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return GridLine{}, false
	}
	tz := newTokenizer(s)
	tok := nextNonWhitespace(tz)
	switch tok.Kind {
	case TokenIdent:
		if strings.EqualFold(tok.Text, "auto") {
			return GridLine{Kind: lineAuto}, true
		}
		if strings.EqualFold(tok.Text, "span") {
			// Look for an optional integer after "span".
			next := nextNonWhitespace(tz)
			if next.Kind == TokenNumber {
				n := int(next.Num)
				if n < 1 {
					n = 1
				}
				return GridLine{Kind: lineSpan, N: n}, true
			}
			// span with no number → span 1
			return GridLine{Kind: lineSpan, N: 1}, true
		}
		return GridLine{Kind: lineName, Name: tok.Text}, true
	case TokenNumber:
		n := int(tok.Num)
		if n == 0 {
			return GridLine{}, false // line 0 is invalid
		}
		return GridLine{Kind: lineNum, N: n}, true
	}
	return GridLine{}, false
}

// splitSlashParts splits s on `/` tokens and returns the raw substrings.
// Returns nil if no `/` is found (single-value case).
func splitSlashParts(s string) []string {
	tz := newTokenizer(s)
	var parts []string
	start := 0
	pos := 0
	for {
		tok := tz.next()
		if tok.Kind == TokenEOF {
			parts = append(parts, s[start:])
			break
		}
		if tok.Kind == TokenDelim && tok.Text == "/" {
			parts = append(parts, s[start:pos])
			start = pos + 1
		}
		pos = tz.pos
	}
	if len(parts) == 1 {
		return nil // no slash found
	}
	return parts
}

// parseGridColumnRow parses a grid-column or grid-row value (start [/ end]) into
// a pair of GridLine endpoints. For a single value with no `/`: if it's an ident,
// the end copies the name; otherwise end is auto. ok is false for bad input.
func parseGridColumnRow(s string) (start, end GridLine, ok bool) {
	s = strings.TrimSpace(s)
	parts := splitSlashParts(s)
	if parts == nil {
		// Single value.
		gl, gok := parseGridLine(s)
		if !gok {
			return GridLine{}, GridLine{}, false
		}
		if gl.Kind == lineName {
			// CSS: a bare ident sets both start and end to that name.
			return gl, gl, true
		}
		return gl, GridLine{Kind: lineAuto}, true
	}
	if len(parts) != 2 {
		return GridLine{}, GridLine{}, false
	}
	startLine, ok1 := parseGridLine(parts[0])
	endLine, ok2 := parseGridLine(parts[1])
	if !ok1 || !ok2 {
		return GridLine{}, GridLine{}, false
	}
	return startLine, endLine, true
}

// parseGridArea parses a grid-area value. If it's a single custom-ident (no `/`),
// return {AreaName: s}. Otherwise it's <row-start> / <col-start> [/ <row-end> [/ <col-end>]].
// Omitted trailing values default per CSS spec.
func parseGridArea(s string) (GridPlacement, bool) {
	s = strings.TrimSpace(s)
	parts := splitSlashParts(s)
	if parts == nil {
		// Single value: if it's an ident, treat as area name.
		s2 := strings.TrimSpace(s)
		tz := newTokenizer(s2)
		tok := nextNonWhitespace(tz)
		if tok.Kind == TokenIdent && !strings.EqualFold(tok.Text, "auto") {
			// Confirm it's just one token.
			if nextNonWhitespace(tz).Kind == TokenEOF {
				return GridPlacement{AreaName: tok.Text}, true
			}
		}
		// Single non-ident value: parse as row-start, rest auto.
		gl, ok := parseGridLine(s2)
		if !ok {
			return GridPlacement{}, false
		}
		return GridPlacement{RowStart: gl}, true
	}

	// 2–4 slash-separated values: row-start / col-start / row-end / col-end.
	// Omitted values: if 2 parts: row-end = row-start (if ident) or auto; col-end = col-start or auto.
	// The CSS default for omitted values in grid-area shorthand:
	//   2 values: row-start/col-start; row-end=row-start if ident else auto; col-end=col-start if ident else auto.
	//   3 values: row-start/col-start/row-end; col-end=col-start if ident else auto.
	//   4 values: row-start/col-start/row-end/col-end.
	if len(parts) < 1 || len(parts) > 4 {
		return GridPlacement{}, false
	}

	lines := make([]GridLine, len(parts))
	for i, p := range parts {
		gl, ok := parseGridLine(p)
		if !ok {
			return GridPlacement{}, false
		}
		lines[i] = gl
	}

	var p GridPlacement
	p.RowStart = lines[0]
	if len(lines) >= 2 {
		p.ColStart = lines[1]
	}
	if len(lines) >= 3 {
		p.RowEnd = lines[2]
	} else {
		// Omitted row-end: copy row-start if ident, else auto.
		if p.RowStart.Kind == lineName {
			p.RowEnd = p.RowStart
		}
		// else lineAuto (zero value)
	}
	if len(lines) >= 4 {
		p.ColEnd = lines[3]
	} else {
		// Omitted col-end: copy col-start if ident, else auto.
		if p.ColStart.Kind == lineName {
			p.ColEnd = p.ColStart
		}
		// else lineAuto (zero value)
	}
	return p, true
}

// parseRepeat parses repeat(<count|auto-fill|auto-fit>, <track-list>) with tz
// positioned after the "repeat" ident.
func parseRepeat(tz *tokenizer) (repeatRun, bool) {
	if nextNonWhitespace(tz).Kind != TokenLParen {
		return repeatRun{}, false
	}
	countTok := nextNonWhitespace(tz)
	var rk repeatKind
	count := 0
	switch {
	case countTok.Kind == TokenNumber && countTok.Num >= 1:
		rk = repeatFixed
		count = int(countTok.Num)
	case countTok.Kind == TokenIdent && strings.EqualFold(countTok.Text, "auto-fill"):
		rk = repeatAutoFill
	case countTok.Kind == TokenIdent && strings.EqualFold(countTok.Text, "auto-fit"):
		rk = repeatAutoFit
	default:
		return repeatRun{}, false
	}
	if nextNonWhitespace(tz).Kind != TokenComma {
		return repeatRun{}, false
	}
	// Parse the inner track list until the closing ")". No nested repeat().
	var inner []TrackSize
	for {
		t := nextNonWhitespace(tz)
		if t.Kind == TokenRParen {
			break
		}
		if t.Kind == TokenEOF {
			return repeatRun{}, false
		}
		if t.Kind == TokenIdent && strings.EqualFold(t.Text, "repeat") {
			return repeatRun{}, false // nested repeat not allowed
		}
		ts, ok := parseTrackSize(tz, t)
		if !ok {
			return repeatRun{}, false
		}
		inner = append(inner, ts)
	}
	if len(inner) == 0 {
		return repeatRun{}, false
	}
	return repeatRun{kind: rk, count: count, inner: inner}, true
}
