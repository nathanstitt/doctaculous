package css

import (
	"strings"

	gcss "github.com/nathanstitt/doctaculous/pkg/css"
	"github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// resolveCounters walks the box tree in document order applying the CSS counter
// system (CSS Lists §4): counter-reset / counter-set / counter-increment on each
// box, plus the implicit "list-item" counter that a display:list-item box
// auto-increments. It then materializes the two things counters drive:
//   - a list-item box's marker text (from its list-style-type + the list-item value);
//   - a box's `content: counter()/counters()` text (rendered into a synthetic text
//     child so the inline layout shows it).
//
// It is a no-op for a tree with no list items, counter ops, or counter content, so a
// document that uses none of these is unchanged. root may be nil.
func resolveCounters(root *cssbox.Box) {
	if root == nil {
		return
	}
	(&counterState{counters: map[string][]int{}}).walk(root)
}

// counterState holds the active counters during the walk. Each name maps to a stack
// of values (one per nesting scope that reset it); the innermost (last) is the
// "current" value, and counters(name, sep) joins the whole stack. This models CSS's
// nested counter scopes closely enough for list nesting and counters().
type counterState struct {
	counters map[string][]int
}

// walk processes one box: apply its counter ops, materialize its marker/content, then
// recurse its children. A counter-reset opens a new scope level whose effect must
// reach the resetting element's following siblings (CSS Lists §4.4), so the level is
// popped by the *parent* after its whole child list is walked — not at the resetting
// box's own subtree end. walk therefore returns the names this box reset, and a parent
// pops every level its children opened once it finishes iterating them. This gives
// nested lists independent counters, makes counters() join the ancestor chain, and lets
// a flat counter-reset (e.g. on an <h2>) carry to the <h3>s that follow it.
func (s *counterState) walk(b *cssbox.Box) (reset []string) {
	// 1. counter-reset: push a new scope level for each named counter. These leave
	//    scope when this box's PARENT finishes its children (see the returned names),
	//    so a following sibling still sees the reset value.
	for _, op := range b.Style.CounterReset {
		s.counters[op.Name] = append(s.counters[op.Name], op.Value)
		reset = append(reset, op.Name)
	}
	// 2. The implicit list-item counter: a display:list-item box increments "list-item".
	//    If no ancestor reset it, create it at 0 first (CSS implicit scope at the root).
	if b.Display == cssbox.DisplayListItem {
		s.ensure("list-item")
		s.increment("list-item", 1)
	}
	// 3. counter-increment.
	for _, op := range b.Style.CounterIncrement {
		s.ensure(op.Name)
		s.increment(op.Name, op.Value)
	}
	// 4. counter-set.
	for _, op := range b.Style.CounterSet {
		s.ensure(op.Name)
		s.set(op.Name, op.Value)
	}

	// Materialize the marker for a list item (unless list-style-type:none). The
	// marker is recorded on the box AND prepended as a leading inline text child so it
	// renders in front of the item's content. (This renders inside/outside markers at
	// the content's left edge; the precise outside-gutter offset is a refinement — the
	// UA padding-left already indents the list so the marker sits in the indent.)
	if b.Display == cssbox.DisplayListItem && b.Style.ListStyleType != "none" {
		if m := s.markerFor(b); m != nil {
			b.Marker = m
			b.Children = append([]*cssbox.Box{makeCounterText(b, m.Text)}, b.Children...)
		}
	}
	// Materialize counter()/counters() content into a synthetic text child.
	if len(b.Style.Content) > 0 {
		if txt := s.renderContent(b.Style.Content); txt != "" {
			b.Children = append([]*cssbox.Box{makeCounterText(b, txt)}, b.Children...)
		}
	}

	// Walk children, collecting every scope level they opened. Each child's resets stay
	// live for its following siblings; we pop them all once the child list is done.
	var childReset []string
	for _, c := range b.Children {
		childReset = append(childReset, s.walk(c)...)
	}
	for _, name := range childReset {
		if v := s.counters[name]; len(v) > 0 {
			s.counters[name] = v[:len(v)-1]
		}
	}
	return reset
}

func (s *counterState) ensure(name string) {
	if len(s.counters[name]) == 0 {
		s.counters[name] = []int{0}
	}
}

func (s *counterState) increment(name string, by int) {
	v := s.counters[name]
	v[len(v)-1] += by
}

func (s *counterState) set(name string, to int) {
	v := s.counters[name]
	v[len(v)-1] = to
}

// current returns the innermost value of name (0 if unset).
func (s *counterState) current(name string) int {
	v := s.counters[name]
	if len(v) == 0 {
		return 0
	}
	return v[len(v)-1]
}

// markerFor builds the marker for a list-item box: a bullet glyph for the bullet
// styles, else the formatted list-item counter value plus the ". " suffix browsers
// add to numeric markers. Position comes from list-style-position.
func (s *counterState) markerFor(b *cssbox.Box) *cssbox.MarkerContent {
	style := b.Style.ListStyleType
	text := gcss.FormatCounter(s.current("list-item"), style)
	if text == "" {
		return nil
	}
	if isNumericListStyle(style) {
		text += ". "
	} else {
		text += " " // a bullet gets a trailing space before the content
	}
	return &cssbox.MarkerContent{Text: text, Outside: b.Style.ListStylePosition != "inside"}
}

// renderContent renders a parsed `content` value's counter pieces (and literal
// strings) to a single string. A counter()/counters() resolves against the active
// counters; counters(name, sep) joins the whole scope stack with sep.
func (s *counterState) renderContent(parts []gcss.ContentPart) string {
	var b strings.Builder
	for _, p := range parts {
		switch p.Kind {
		case gcss.ContentString:
			b.WriteString(p.Text)
		case gcss.ContentCounter:
			b.WriteString(gcss.FormatCounter(s.current(p.Name), styleOr(p.Style)))
		case gcss.ContentCounters:
			vals := s.counters[p.Name]
			parts := make([]string, 0, len(vals))
			for _, v := range vals {
				parts = append(parts, gcss.FormatCounter(v, styleOr(p.Style)))
			}
			b.WriteString(strings.Join(parts, p.Sep))
		}
	}
	return b.String()
}

func styleOr(s string) string {
	if s == "" {
		return "decimal"
	}
	return s
}

// isNumericListStyle reports whether a list-style-type is a numbering style (gets a
// "." suffix), as opposed to a bullet glyph.
func isNumericListStyle(style string) bool {
	switch style {
	case "disc", "circle", "square", "none":
		return false
	default:
		return true
	}
}

// makeCounterText builds a synthetic inline text box carrying generated `content`
// text, inheriting the box's text style so it shapes with the element's font. The
// copied style has its Content AND counter ops cleared so the walk neither
// re-generates content for this synthetic box (infinite recursion) nor re-applies the
// parent's counter-reset/increment/set when it recurses into the child (which would
// double-count, e.g. an element with `counter-increment` plus a marker/content).
func makeCounterText(parent *cssbox.Box, text string) *cssbox.Box {
	st := parent.Style
	st.Content = nil
	st.CounterReset = nil
	st.CounterIncrement = nil
	st.CounterSet = nil
	return &cssbox.Box{
		Kind:    cssbox.BoxText,
		Display: cssbox.DisplayInline,
		Style:   st,
		Text:    text,
	}
}
