package css

import "strings"

// Specificity is a CSS specificity triple (a,b,c): id count, class count, type
// count. Compared field-by-field, a dominates b dominates c.
type Specificity struct {
	IDs, Classes, Types int
}

// Less reports whether s is lower specificity than o.
func (s Specificity) Less(o Specificity) bool {
	if s.IDs != o.IDs {
		return s.IDs < o.IDs
	}
	if s.Classes != o.Classes {
		return s.Classes < o.Classes
	}
	return s.Types < o.Types
}

// simpleSelector matches a single element: an optional type, plus any number of
// class and id qualifiers. A universal "*" sets neither type nor qualifiers.
type simpleSelector struct {
	tag     string // "" means any (universal or qualifier-only)
	id      string
	classes []string
}

// Selector is a sequence of simpleSelectors joined by descendant combinators,
// read left (ancestor) to right (subject). The last element is the subject the
// selector matches.
type Selector struct {
	parts []simpleSelector
}

// Specificity sums the selector's parts.
func (s Selector) Specificity() Specificity {
	var sp Specificity
	for _, p := range s.parts {
		if p.id != "" {
			sp.IDs++
		}
		sp.Classes += len(p.classes)
		if p.tag != "" {
			sp.Types++
		}
	}
	return sp
}

// parseSelectorList parses a comma-separated selector group into individual
// Selectors. Whitespace between simple selectors is a descendant combinator.
// Parsing is total: a malformed group is skipped rather than erroring, so one bad
// selector cannot void a rule's other selectors.
func parseSelectorList(src string) []Selector {
	var out []Selector
	for _, group := range strings.Split(src, ",") {
		sel, ok := parseOneSelector(strings.TrimSpace(group))
		if ok {
			out = append(out, sel)
		}
	}
	return out
}

func parseOneSelector(src string) (Selector, bool) {
	fields := strings.Fields(src) // descendant combinator = whitespace
	if len(fields) == 0 {
		return Selector{}, false
	}
	var sel Selector
	for _, f := range fields {
		ss, ok := parseSimple(f)
		if !ok {
			return Selector{}, false
		}
		sel.parts = append(sel.parts, ss)
	}
	return sel, true
}

// parseSimple parses one compound simple selector like "div.intro#lead" or "*".
func parseSimple(f string) (simpleSelector, bool) {
	var ss simpleSelector
	if f == "*" {
		return ss, true
	}
	if f == "" {
		return simpleSelector{}, false
	}
	i := 0
	// leading type selector
	for i < len(f) && f[i] != '.' && f[i] != '#' {
		i++
	}
	ss.tag = strings.ToLower(f[:i])
	for i < len(f) {
		marker := f[i]
		i++
		start := i
		for i < len(f) && f[i] != '.' && f[i] != '#' {
			i++
		}
		name := f[start:i]
		if name == "" {
			return simpleSelector{}, false
		}
		switch marker {
		case '.':
			ss.classes = append(ss.classes, name)
		case '#':
			ss.id = name
		}
	}
	return ss, true
}
