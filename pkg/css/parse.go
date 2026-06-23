package css

import "strings"

// Declaration is one property: value pair from a rule body, with the !important
// flag. Value is the raw value text (trimmed); typed interpretation happens in
// the cascade so unknown properties are retained losslessly.
type Declaration struct {
	Property  string
	Value     string
	Important bool
}

// parseDeclarations parses a rule body (the text between { and }) into
// declarations. Malformed declarations (no colon, empty property, empty value)
// are skipped so one bad declaration cannot void the rest.
func parseDeclarations(body string) []Declaration {
	var out []Declaration
	// NOTE: the body is split naively on ';'. A value containing a literal
	// semicolon (e.g. a data: URI in url(...)) will be split incorrectly; that is
	// an accepted limitation for the CSS subset this engine targets.
	for _, chunk := range strings.Split(body, ";") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		colon := strings.IndexByte(chunk, ':')
		if colon < 0 {
			continue
		}
		prop := strings.TrimSpace(chunk[:colon])
		val := strings.TrimSpace(chunk[colon+1:])
		if prop == "" || val == "" {
			continue
		}
		important := false
		// Match !important only as the trailing token (suffix + preceding
		// whitespace), case-insensitively; the suffix is ASCII so cutting len(bang)
		// bytes off the original is safe. This avoids the substring false-positive
		// where "url(x!important.png)" would otherwise be flagged.
		const bang = "!important"
		if strings.HasSuffix(strings.ToLower(val), bang) {
			candidate := val[:len(val)-len(bang)]
			if candidate == "" || isWhitespace(candidate[len(candidate)-1]) {
				important = true
				val = strings.TrimSpace(candidate)
			}
		}
		if val == "" {
			continue
		}
		out = append(out, Declaration{Property: strings.ToLower(prop), Value: val, Important: important})
	}
	return out
}
