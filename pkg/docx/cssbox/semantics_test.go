package cssbox

import (
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	lcssbox "github.com/nathanstitt/doctaculous/pkg/layout/cssbox"
)

// findSemTag returns the first box (depth-first) whose SemTag equals tag, or nil.
func findSemTag(b *lcssbox.Box, tag string) *lcssbox.Box {
	if b.SemTag == tag {
		return b
	}
	for _, c := range b.Children {
		if got := findSemTag(c, tag); got != nil {
			return got
		}
	}
	return nil
}

// paraStyled builds a paragraph with the given style id and text.
func paraStyled(styleID, text string) *docx.Paragraph {
	p := paraWith(text)
	p.Props.StyleID = styleID
	return p
}

func TestParagraphSemanticsHeadings(t *testing.T) {
	cases := []struct {
		styleID string
		wantTag string
		wantLvl int
	}{
		{"Heading1", "h1", 1},
		{"Heading2", "h2", 2},
		{"heading 3", "h3", 3}, // display form, case/space insensitive
		{"Heading9", "h6", 6},  // clamp to 6
		{"Title", "h1", 0},
		{"Subtitle", "h2", 0},
		{"Quote", "blockquote", 0},
		{"IntenseQuote", "blockquote", 0},
	}
	for _, tc := range cases {
		gotTag, gotLvl := paragraphSemantics(tc.styleID)
		if gotTag != tc.wantTag || gotLvl != tc.wantLvl {
			t.Errorf("paragraphSemantics(%q) = (%q,%d), want (%q,%d)",
				tc.styleID, gotTag, gotLvl, tc.wantTag, tc.wantLvl)
		}
	}
}

func TestParagraphSemanticsPlain(t *testing.T) {
	// An ordinary or unknown style yields no annotation.
	for _, id := range []string{"", "Normal", "BodyText", "MadeUp"} {
		if tag, lvl := paragraphSemantics(id); tag != "" || lvl != 0 {
			t.Errorf("paragraphSemantics(%q) = (%q,%d), want empty", id, tag, lvl)
		}
	}
}

func TestLowerHeadingBox(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Body:    []docx.Block{{Paragraph: paraStyled("Heading2", "Chapter")}},
	}
	root := lowerDoc(t, d)
	h := findSemTag(root, "h2")
	if h == nil {
		t.Fatal("no heading box with SemTag h2")
	}
	if h.HeadingLvl != 2 {
		t.Errorf("HeadingLvl = %d, want 2", h.HeadingLvl)
	}
}

func TestLowerHyperlinkURL(t *testing.T) {
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Rels: map[string]docx.Relationship{
			"rId5": {ID: "rId5", Target: "https://example.com/doc", External: true},
		},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{
			Content: []docx.ParaChild{{Hyperlink: &docx.Hyperlink{
				RelID: "rId5",
				Runs:  []docx.Run{{Text: "here"}},
			}}},
		}}},
	}
	root := lowerDoc(t, d)
	a := findSemTag(root, "a")
	if a == nil {
		t.Fatal("no link box with SemTag a")
	}
	if a.Href != "https://example.com/doc" {
		t.Errorf("Href = %q, want https://example.com/doc", a.Href)
	}
}

func TestLowerHyperlinkUnresolved(t *testing.T) {
	// An internal-anchor link (no rel target) degrades to an empty Href.
	d := &docx.Document{
		Section: docx.SectionProps{PageW: 12240, PageH: 15840, MarginLeft: 1440, MarginRight: 1440, MarginTop: 1440, MarginBottom: 1440},
		Body: []docx.Block{{Paragraph: &docx.Paragraph{
			Content: []docx.ParaChild{{Hyperlink: &docx.Hyperlink{
				Anchor: "bookmark1",
				Runs:   []docx.Run{{Text: "jump"}},
			}}},
		}}},
	}
	root := lowerDoc(t, d)
	a := findSemTag(root, "a")
	if a == nil {
		t.Fatal("no link box with SemTag a")
	}
	if a.Href != "" {
		t.Errorf("Href = %q, want empty", a.Href)
	}
}
