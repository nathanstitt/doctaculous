package css

import (
	"strconv"
	"strings"
)

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
// class, id, and pseudo-class qualifiers. A universal "*" sets none of them.
type simpleSelector struct {
	tag     string // "" means any (universal or qualifier-only)
	id      string
	classes []string
	pseudos []string  // pseudo-class names, lowercased (e.g. "link", "hover"); no leading ":"
	nths    []nthTest // structural :nth-*() pseudo-classes (An+B), parsed
}

// nthTest is one parsed :nth-child()/:nth-last-child()/:nth-of-type()/
// :nth-last-of-type() qualifier: position p (1-based) matches when p = a·k + b
// for some integer k ≥ 0. fromEnd counts from the last sibling; ofType restricts
// the sibling walk to elements with the subject's tag.
type nthTest struct {
	a, b    int
	fromEnd bool
	ofType  bool
}

// matchesPos reports whether 1-based position p satisfies the An+B test.
func (t nthTest) matchesPos(p int) bool {
	if t.a == 0 {
		return p == t.b
	}
	d := p - t.b
	return d%t.a == 0 && d/t.a >= 0
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
		sp.Classes += len(p.pseudos) // a pseudo-class counts at the class level (CSS)
		sp.Classes += len(p.nths)    // ... including the functional structural ones
		if p.tag != "" {
			sp.Types++
		}
	}
	return sp
}

// Matches reports whether the selector matches node n. The last part must match
// n itself; earlier parts must each match some ancestor, in order (descendant
// combinator). Matching walks ancestors greedily from the subject outward.
func (s Selector) Matches(n Node) bool {
	if len(s.parts) == 0 {
		return false
	}
	last := len(s.parts) - 1
	if !s.parts[last].matches(n) {
		return false
	}
	// Match remaining parts (right-to-left) against ancestors.
	cur := n.Parent()
	i := last - 1
	for i >= 0 {
		matched := false
		for cur != nil {
			if s.parts[i].matches(cur) {
				cur = cur.Parent()
				matched = true
				break
			}
			cur = cur.Parent()
		}
		if !matched {
			return false
		}
		i--
	}
	return true
}

// matches reports whether a single simple selector matches node n.
func (ss simpleSelector) matches(n Node) bool {
	if ss.tag != "" && ss.tag != n.Tag() {
		return false
	}
	if ss.id != "" && ss.id != n.ID() {
		return false
	}
	for _, c := range ss.classes {
		if !hasClass(n.Classes(), c) {
			return false
		}
	}
	for _, p := range ss.pseudos {
		if !matchPseudoClass(p, n) {
			return false
		}
	}
	for _, t := range ss.nths {
		pos, ok := structuralPos(n, t.ofType, t.fromEnd)
		if !ok || !t.matchesPos(pos) {
			return false
		}
	}
	return true
}

// structuralPos returns n's 1-based position among its parent's element children
// (restricted to n's tag when ofType, counted from the last sibling when fromEnd),
// or ok=false when the Node does not expose sibling positions (a DOM that does not
// implement SiblingIndexer never matches structural pseudo-classes). A root
// element counts as the first and only child.
func structuralPos(n Node, ofType, fromEnd bool) (int, bool) {
	si, ok := n.(SiblingIndexer)
	if !ok {
		return 0, false
	}
	pos, last, typePos, typeLast := si.SiblingIndex()
	if pos < 1 { // defensive: implementation failed to locate the element
		return 0, false
	}
	switch {
	case ofType && fromEnd:
		return typeLast, true
	case ofType:
		return typePos, true
	case fromEnd:
		return last, true
	default:
		return pos, true
	}
}

// matchPseudoClass reports whether pseudo-class p (lowercased, no ":") matches n in
// this static, history-less renderer:
//   - "link" matches a hyperlink (a/area/link with a non-empty href). With no browsing
//     history, every hyperlink is unvisited, so :link is "is a link".
//   - "visited" matches nothing (no history; also the standard privacy stance).
//   - every other recognized dynamic/state pseudo (hover/focus/active/target/checked/…)
//     matches nothing in a static render, so its rule is inert.
//
// An unmatched pseudo makes the simple selector fail, which is exactly what makes a
// :hover or :visited rule simply not apply — the correct static behavior.
func matchPseudoClass(p string, n Node) bool {
	switch p {
	case "link":
		return isHyperlink(n)
	case "first-child":
		pos, ok := structuralPos(n, false, false)
		return ok && pos == 1
	case "last-child":
		pos, ok := structuralPos(n, false, true)
		return ok && pos == 1
	case "only-child":
		pos, ok := structuralPos(n, false, false)
		if !ok || pos != 1 {
			return false
		}
		last, _ := structuralPos(n, false, true)
		return last == 1
	case "first-of-type":
		pos, ok := structuralPos(n, true, false)
		return ok && pos == 1
	case "last-of-type":
		pos, ok := structuralPos(n, true, true)
		return ok && pos == 1
	case "only-of-type":
		pos, ok := structuralPos(n, true, false)
		if !ok || pos != 1 {
			return false
		}
		last, _ := structuralPos(n, true, true)
		return last == 1
	default:
		// "visited" and all dynamic/state pseudo-classes: never match statically.
		return false
	}
}

// isHyperlink reports whether n is a hyperlink element with an href: <a>, <area>, or
// <link> carrying a non-empty href attribute (CSS :link/:visited apply only to these).
func isHyperlink(n Node) bool {
	switch n.Tag() {
	case "a", "area", "link":
		href, ok := n.Attr("href")
		return ok && href != ""
	}
	return false
}

func hasClass(have []string, want string) bool {
	for _, c := range have {
		if c == want {
			return true
		}
	}
	return false
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

// parseSimple parses one compound simple selector like "div.intro#lead", "a:link", or
// "*". Pseudo-classes (:name) are captured; pseudo-elements (::name or the legacy
// :before/:after/:first-line/:first-letter) and functional pseudos (:name(...)) cause
// the whole selector to be dropped (ok=false) so the rule never falsely matches — its
// other comma-separated selectors are unaffected (parseSelectorList isolates each).
func parseSimple(f string) (simpleSelector, bool) {
	var ss simpleSelector
	if f == "*" {
		return ss, true
	}
	if f == "" {
		return simpleSelector{}, false
	}
	// A '(' also ends a name fragment so a functional pseudo (:not(...), :nth-child(...))
	// is detected rather than mis-split on inner . / # / : characters.
	isMarker := func(c byte) bool { return c == '.' || c == '#' || c == ':' || c == '(' }
	i := 0
	// leading type selector
	for i < len(f) && !isMarker(f[i]) {
		i++
	}
	ss.tag = strings.ToLower(f[:i])
	if ss.tag == "*" {
		ss.tag = "" // the universal type: matches any element, zero type specificity
	}
	for i < len(f) {
		marker := f[i]
		if marker == '(' {
			return simpleSelector{}, false // '(' without a preceding :name: drop
		}
		i++
		if marker == ':' && i < len(f) && f[i] == ':' {
			return simpleSelector{}, false // pseudo-element (::name): drop the selector
		}
		start := i
		for i < len(f) && !isMarker(f[i]) {
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
		case ':':
			lower := strings.ToLower(name)
			if isLegacyPseudoElement(lower) {
				return simpleSelector{}, false // :before/:after/… as pseudo-element: drop
			}
			if i < len(f) && f[i] == '(' {
				// A functional pseudo-class. The structural :nth-*(An+B) family is
				// supported; any other functional (:not, :is, :where, …) drops the
				// selector so its rule never falsely matches.
				close := strings.IndexByte(f[i:], ')')
				if close < 0 {
					return simpleSelector{}, false // unterminated argument
				}
				arg := f[i+1 : i+close]
				i += close + 1
				t, ok := parseNthPseudo(lower, arg)
				if !ok {
					return simpleSelector{}, false
				}
				ss.nths = append(ss.nths, t)
				continue
			}
			ss.pseudos = append(ss.pseudos, lower)
		}
	}
	return ss, true
}

// parseNthPseudo builds the nthTest for one of the four structural functional
// pseudo-classes from its raw argument. Any other name, or a malformed An+B
// argument, returns ok=false (the caller drops the selector).
func parseNthPseudo(name, arg string) (nthTest, bool) {
	var t nthTest
	switch name {
	case "nth-child":
	case "nth-last-child":
		t.fromEnd = true
	case "nth-of-type":
		t.ofType = true
	case "nth-last-of-type":
		t.fromEnd, t.ofType = true, true
	default:
		return nthTest{}, false
	}
	a, b, ok := parseANpB(arg)
	if !ok {
		return nthTest{}, false
	}
	t.a, t.b = a, b
	return t, true
}

// parseANpB parses the CSS An+B microsyntax: "even", "odd", "5", "n", "-n+3",
// "2n", "2n+1", "3n-2" — with optional whitespace around the sign. Returns
// ok=false on anything else.
func parseANpB(src string) (a, b int, ok bool) {
	s := strings.ToLower(strings.TrimSpace(src))
	switch s {
	case "even":
		return 2, 0, true
	case "odd":
		return 2, 1, true
	case "":
		return 0, 0, false
	}
	nIdx := strings.IndexByte(s, 'n')
	if nIdx < 0 {
		// Plain integer: B only.
		b, err := strconv.Atoi(s)
		if err != nil {
			return 0, 0, false
		}
		return 0, b, true
	}
	// Coefficient before 'n': "", "-", "+", or an integer.
	coef := strings.TrimSpace(s[:nIdx])
	switch coef {
	case "", "+":
		a = 1
	case "-":
		a = -1
	default:
		v, err := strconv.Atoi(coef)
		if err != nil {
			return 0, 0, false
		}
		a = v
	}
	// Offset after 'n': empty, or sign + integer (whitespace allowed around the sign).
	rest := strings.TrimSpace(s[nIdx+1:])
	if rest == "" {
		return a, 0, true
	}
	sign := 1
	switch rest[0] {
	case '+':
	case '-':
		sign = -1
	default:
		return 0, 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(rest[1:]))
	if err != nil || v < 0 { // the sign is separate; "2n+-3" is invalid
		return 0, 0, false
	}
	return a, sign * v, true
}

// isLegacyPseudoElement reports whether name (lowercased) is one of the four pseudo-
// elements that also accept the single-colon legacy syntax. Treating them as pseudo-
// elements (dropping the selector) avoids matching them as if they were pseudo-classes.
func isLegacyPseudoElement(name string) bool {
	switch name {
	case "before", "after", "first-line", "first-letter":
		return true
	}
	return false
}
