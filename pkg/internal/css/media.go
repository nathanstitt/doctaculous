package css

import "strings"

// Media is a parsed media type. Only the media TYPE is honored (print / screen /
// all); full media queries (feature tests like `(min-width: 40em)`) degrade to
// "matches if the type matches". A top-level rule is MediaAll.
type Media int

const (
	// MediaAll applies in any context (the default for top-level rules and @media all).
	MediaAll Media = iota
	// MediaScreen applies in screen contexts (the default HTML render context).
	MediaScreen
	// MediaPrint applies in print contexts (PDF output honors these).
	MediaPrint
)

// mediaFromPrelude extracts the media type from an @media prelude's remainder
// (everything after "@media"). It reads the first media-type identifier: "print",
// "screen", or "all"; anything else (a bare feature query, "only screen", a comma
// list) falls back to MediaAll so the block still participates, per the "type match"
// degradation. "only screen"/"not print" are reduced to their trailing type.
func mediaFromPrelude(rest string) Media {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(rest)))
	for _, f := range fields {
		switch f {
		case "print":
			return MediaPrint
		case "screen":
			return MediaScreen
		case "all":
			return MediaAll
		case "only", "not", "and":
			continue // modifiers/combinators: keep scanning for the type
		default:
			// A feature query token like "(min-width:40em)" or an unknown type: stop
			// and treat as all (participates in any context).
			return MediaAll
		}
	}
	return MediaAll
}

// RulesForMedia returns the sheet's rules that apply in media context m: every rule
// tagged MediaAll plus rules tagged m. Top-level rules are MediaAll, so the default
// (screen) render is unchanged.
func (s *Stylesheet) RulesForMedia(m Media) []Rule {
	var out []Rule
	for _, r := range s.Rules {
		if r.Media == MediaAll || r.Media == m {
			out = append(out, r)
		}
	}
	return out
}
