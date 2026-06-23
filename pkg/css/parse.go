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
		if i := strings.LastIndex(strings.ToLower(val), "!important"); i >= 0 {
			important = true
			val = strings.TrimSpace(val[:i])
		}
		if val == "" {
			continue
		}
		out = append(out, Declaration{Property: strings.ToLower(prop), Value: val, Important: important})
	}
	return out
}
