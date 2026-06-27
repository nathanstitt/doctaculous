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
