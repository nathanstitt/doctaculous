package docx

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"math/rand"
	"reflect"
	"testing"
)

// mustRoundTrip writes doc, reopens it, and returns the reparsed document.
func mustRoundTrip(t *testing.T, doc *Document) *Document {
	t.Helper()
	data, err := Bytes(doc)
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	got, err := OpenBytes(data)
	if err != nil {
		t.Fatalf("OpenBytes(Write(doc)): %v", err)
	}
	return got
}

// normalizeForCompare reduces both sides of a round-trip comparison to the
// canonical form. The normalizations are the documented seams where Write may
// differ textually from a hand-built document while remaining semantically
// identical:
//   - hyperlink relationship ids: a Target-only link gains an allocated id on
//     write, so links compare by URL (RelID cleared wherever Target is known);
//   - the rel table itself (structural rels are allocated on write);
//   - Sections is recomputed from the paragraph section breaks + body section;
//   - a zero SectionProps means "Letter defaults" (what Write emits and the
//     parser substitutes);
//   - a style with no Type is a paragraph style.
func normalizeForCompare(d *Document) {
	def := defaultSection()
	normSect := func(s *SectionProps) {
		if *s == (SectionProps{}) {
			*s = def
		}
	}
	normSect(&d.Section)

	var sections []SectionProps
	var walkChildren func(children []ParaChild)
	walkChildren = func(children []ParaChild) {
		for i := range children {
			switch {
			case children[i].Hyperlink != nil:
				if children[i].Hyperlink.Target != "" {
					children[i].Hyperlink.RelID = ""
				}
			case children[i].Revision != nil:
				walkChildren(children[i].Revision.Content)
			}
		}
	}
	var walkBlocks func(blocks []Block)
	walkBlocks = func(blocks []Block) {
		for i := range blocks {
			switch {
			case blocks[i].Paragraph != nil:
				p := blocks[i].Paragraph
				if p.Props.SectPr != nil {
					normSect(p.Props.SectPr)
					sections = append(sections, *p.Props.SectPr)
				}
				walkChildren(p.Content)
			case blocks[i].Table != nil:
				for _, row := range blocks[i].Table.Rows {
					for r := range row.Cells {
						walkBlocks(row.Cells[r].Blocks)
					}
				}
			}
		}
	}
	walkBlocks(d.Body)
	for _, notes := range []*Notes{d.Footnotes, d.Endnotes} {
		if notes == nil {
			continue
		}
		for _, blocks := range notes.ByID {
			walkBlocks(blocks)
		}
	}
	for _, c := range d.Comments {
		walkBlocks(c.Body)
	}
	for _, hf := range d.Headers {
		walkBlocks(hf.Blocks)
	}
	for _, hf := range d.Footers {
		walkBlocks(hf.Blocks)
	}
	d.Sections = append(sections, d.Section)
	d.Rels = nil
	if d.Styles != nil {
		for _, s := range d.Styles.ByID {
			if s.Type == "" {
				s.Type = "paragraph"
			}
		}
	}
}

// assertRoundTrip pins Parse(Write(doc)) ≡ doc under normalizeForCompare.
func assertRoundTrip(t *testing.T, name string, doc *Document) {
	t.Helper()
	got := mustRoundTrip(t, doc)
	normalizeForCompare(doc)
	normalizeForCompare(got)
	if !reflect.DeepEqual(doc, got) {
		t.Errorf("%s: round trip diverged\n want: %#v\n  got: %#v", name, doc, got)
	}
}

// modelFixture is one constructed-document round-trip case.
type modelFixture struct {
	name  string
	build func() *Document
}

// run/paragraph helpers for fixture construction. Runs follow the parse shape:
// text OR break OR reference per run (the parser splits combined runs).
func textRun(text string, props RunProps) ParaChild {
	return ParaChild{Run: &Run{Text: text, Props: props}}
}

func paraOf(props ParagraphProps, children ...ParaChild) Block {
	return Block{Paragraph: &Paragraph{Props: props, Content: children}}
}

func rgba(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 0xFF} }

// modelCore is the per-feature constructed corpus: every vocabulary item the
// writer emits, one fixture each, round-tripped through the real parser.
var modelCore = []modelFixture{
	{"paragraph-props", func() *Document {
		return &Document{Body: []Block{paraOf(ParagraphProps{
			StyleID: "Heading2", Justify: JustifyCenter, HasJustify: true,
			SpacingBefore: 240, HasSpacingBefore: true, SpacingAfter: 120, HasSpacingAfter: true,
			Line: 360, LineRule: LineRuleExact, HasLine: true,
			IndentLeft: 720, HasIndentLeft: true, IndentRight: 360, HasIndentRight: true,
			FirstLine: -180, HasFirstLine: true, PageBreakBefore: true,
			TabStops: []TabStop{{PosTwips: 2880, Align: "center"}, {PosTwips: 5760, Align: "right"}},
		}, textRun("styled paragraph", RunProps{}))}}
	}},
	{"run-toggles", func() *Document {
		return &Document{Body: []Block{paraOf(ParagraphProps{},
			textRun("bold", RunProps{Bold: true, HasBold: true}),
			textRun("explicit-off", RunProps{Bold: false, HasBold: true, Italic: false, HasItalic: true}),
			textRun("styled", RunProps{
				Italic: true, HasItalic: true, Strike: true, HasStrike: true,
				Caps: true, HasCaps: true, SmallCaps: false, HasSmallCaps: true,
				SizeHalfPts: 28, HasSize: true, Color: rgba(0xAA, 0x00, 0x11), HasColor: true,
				Family: "Georgia", StyleID: "CodeChar",
			}),
			textRun("underlined", RunProps{Underline: true, HasUnderline: true, UnderlineStyle: "double",
				UnderlineColor: rgba(0, 0x80, 0), HasUnderlineColor: true}),
			textRun("no-underline", RunProps{Underline: false, HasUnderline: true}),
			textRun("marked", RunProps{Highlight: rgba(0xFF, 0xFF, 0), HasHighlight: true,
				Shd: Shading{Fill: rgba(0xEE, 0xEE, 0xEE), HasFill: true}, VertAlign: VertAlignSuperscript}),
		)}}
	}},
	{"text-shapes", func() *Document {
		return &Document{Body: []Block{paraOf(ParagraphProps{},
			textRun(" leading and trailing ", RunProps{}),
			textRun("tab\tseparated\tcells", RunProps{}),
			textRun("escapes <>&\"' fine", RunProps{}),
			ParaChild{Run: &Run{Break: BreakLine}},
			ParaChild{Run: &Run{Break: BreakPage}},
			ParaChild{Run: &Run{Break: BreakColumn}},
		)}}
	}},
	{"hyperlinks", func() *Document {
		return &Document{Body: []Block{paraOf(ParagraphProps{},
			ParaChild{Hyperlink: &Hyperlink{Target: "https://example.com/a?x=1&y=2",
				Runs: []Run{{Text: "external"}}}},
			ParaChild{Hyperlink: &Hyperlink{Target: "https://example.com/a?x=1&y=2",
				Runs: []Run{{Text: "same target"}}}},
			ParaChild{Hyperlink: &Hyperlink{Anchor: "section-2", Runs: []Run{{Text: "internal"}}}},
		)}}
	}},
	{"revisions", func() *Document {
		return &Document{Body: []Block{paraOf(ParagraphProps{},
			textRun("kept ", RunProps{}),
			ParaChild{Revision: &Revision{Kind: RevisionInsert, ID: 1, Author: "Ada", Date: "2026-07-01T00:00:00Z",
				Content: []ParaChild{
					textRun("added ", RunProps{}),
					{Revision: &Revision{Kind: RevisionInsert, ID: 2, Author: "Bo",
						Content: []ParaChild{textRun("nested", RunProps{})}}},
				}}},
			ParaChild{Revision: &Revision{Kind: RevisionDelete, ID: 3, Author: "Cy",
				Content: []ParaChild{textRun("removed text", RunProps{})}}},
		)}}
	}},
	{"prop-changes", func() *Document {
		return &Document{Body: []Block{paraOf(ParagraphProps{
			Justify: JustifyRight, HasJustify: true,
			PPrChange: &ParaPropsChange{Mark: RevisionMark{ID: 5, Author: "Di", Date: "2026-01-01T00:00:00Z"},
				Previous: ParagraphProps{Justify: JustifyCenter, HasJustify: true}},
		},
			textRun("changed", RunProps{Bold: true, HasBold: true,
				RPrChange: &RunPropsChange{Mark: RevisionMark{ID: 6, Author: "Ed"},
					Previous: RunProps{Italic: true, HasItalic: true}}}),
		)}}
	}},
	{"comments", func() *Document {
		return &Document{
			Comments: map[int]*Comment{
				9: {ID: 9, Author: "Gil", Initials: "G", Date: "2026-02-02T10:00:00Z",
					Body: []Block{paraOf(ParagraphProps{}, textRun("verify this", RunProps{}))}},
			},
			Body: []Block{paraOf(ParagraphProps{},
				ParaChild{CommentStart: &CommentMark{ID: 9}},
				textRun("flagged", RunProps{}),
				ParaChild{CommentEnd: &CommentMark{ID: 9}},
				ParaChild{Run: &Run{CommentRef: 9}},
			)},
		}
	}},
	{"notes", func() *Document {
		fn := NewNotes()
		fn.Add(2, []Block{paraOf(ParagraphProps{}, textRun("the footnote", RunProps{}))})
		en := NewNotes()
		en.Add(3, []Block{paraOf(ParagraphProps{}, textRun("the endnote", RunProps{}))})
		return &Document{
			Footnotes: fn, Endnotes: en,
			Body: []Block{paraOf(ParagraphProps{},
				textRun("claim", RunProps{}),
				ParaChild{Run: &Run{FootnoteRef: 2}},
				ParaChild{Run: &Run{EndnoteRef: 3}},
			)},
		}
	}},
	{"frames-dropcap", func() *Document {
		return &Document{Body: []Block{
			paraOf(ParagraphProps{Frame: &FramePr{DropCap: "drop", Lines: 3, Wrap: "around",
				HAnchor: "text", VAnchor: "text", W: 500, H: 700, HSpace: 100}},
				textRun("D", RunProps{})),
			paraOf(ParagraphProps{}, textRun("rest of the sentence.", RunProps{})),
		}}
	}},
	{"tables", func() *Document {
		border := func(style string, sz int) Border {
			return Border{Style: style, SizeEighthPt: sz, Color: rgba(0x33, 0x44, 0x55), HasColor: true}
		}
		cell := func(text string) TableCell {
			return TableCell{GridSpan: 1, Blocks: []Block{paraOf(ParagraphProps{}, textRun(text, RunProps{}))}}
		}
		spanned := cell("span")
		spanned.GridSpan = 2
		spanned.Props.Borders = BoxBorders{Top: border("dashed", 8), Bottom: border("double", 4),
			Left: Border{None: true}, Right: border("dotted", 6)}
		spanned.Props.Shading = Shading{Fill: rgba(0xF0, 0xF0, 0xF0), HasFill: true}
		spanned.Props.VAlign = VAlignCenter
		merged := cell("restart")
		merged.VMerge = VMergeRestart
		// A continuation cell carries an empty paragraph: OOXML requires at
		// least one block per cell, so that is the parse shape too.
		mergedCont := TableCell{GridSpan: 1, VMerge: VMergeContinue,
			Blocks: []Block{paraOf(ParagraphProps{})}}
		tracked := cell("tracked")
		tracked.Ins = &RevisionMark{ID: 11, Author: "Hu"}
		tracked.Props.TcPrChange = &CellPropsChange{Mark: RevisionMark{ID: 12, Author: "Iv"},
			Previous: CellProps{VAlign: VAlignBottom, WidthDxa: 1000}}
		return &Document{Body: []Block{{Table: &Table{
			Grid:  []Twips{2000, 3000, 3000},
			Props: TableProps{WidthDxa: 8000, Justify: JustifyCenter, Borders: BoxBorders{Top: border("single", 4)}},
			Rows: []TableRow{
				{Props: RowProps{IsHeader: true, HeightDxa: 400}, Cells: []TableCell{cell("a"), spanned}},
				{Cells: []TableCell{merged, cell("b"), tracked}},
				{Cells: []TableCell{mergedCont, cell("c"), cell("d")}},
			},
		}}}}
	}},
	{"lists-numbering", func() *Document {
		num := NewNumbering()
		num.Abstract[0] = map[int]NumLevel{
			0: {Format: NumFmtDecimal, Text: "%1.", Start: 3, HasStart: true},
			1: {Format: NumFmtLowerLetter, Text: "%2)"},
		}
		num.Abstract[1] = map[int]NumLevel{0: {Format: NumFmtBullet, Text: "•"}}
		num.Instances[1] = NumInstance{AbstractID: 0}
		num.Instances[2] = NumInstance{AbstractID: 0, Overrides: map[int]LevelOverride{0: {Start: 7, HasStart: true}}}
		num.Instances[3] = NumInstance{AbstractID: 1}
		li := func(numID, ilvl int, text string) Block {
			return paraOf(ParagraphProps{HasNum: true, NumID: numID, ILvl: ilvl}, textRun(text, RunProps{}))
		}
		return &Document{Numbering: num, Body: []Block{
			li(1, 0, "three"), li(1, 1, "a)"), li(2, 0, "seven"), li(3, 0, "bullet"),
		}}
	}},
	{"drawings", func() *Document {
		doc := &Document{}
		relID := doc.AddImage("chart.png", []byte("\x89PNG\r\n\x1a\nfake")) // bytes are opaque to the writer
		doc.Body = []Block{
			paraOf(ParagraphProps{}, ParaChild{Drawing: &Drawing{
				RelID: relID, WidthEMU: 914400, HeightEMU: 457200, Description: "inline chart"}}),
			paraOf(ParagraphProps{},
				ParaChild{Drawing: &Drawing{RelID: relID, WidthEMU: 914400, HeightEMU: 914400,
					Anchored: true, WrapKind: "square", AlignH: "right", Description: "floated"}},
				textRun("wraps beside", RunProps{})),
		}
		return doc
	}},
	{"styles-table", func() *Document {
		return &Document{
			Styles: DefaultStyles(),
			Body: []Block{
				paraOf(ParagraphProps{StyleID: "Heading1"}, textRun("Title", RunProps{})),
				paraOf(ParagraphProps{StyleID: "Quote"}, textRun("quoted", RunProps{})),
			},
		}
	}},
	{"sections-headers", func() *Document {
		landscape := SectionProps{PageW: 15840, PageH: 12240,
			MarginTop: 720, MarginBottom: 720, MarginLeft: 720, MarginRight: 720,
			Header: 360, Footer: 360}
		body := SectionProps{PageW: 12240, PageH: 15840,
			MarginTop: 1440, MarginBottom: 1440, MarginLeft: 1440, MarginRight: 1440,
			Header: 720, Footer: 720, Gutter: 240,
			HeaderRefDefault: "rId20", FooterRefDefault: "rId21"}
		return &Document{
			Section: body,
			Headers: map[string]*HeaderFooter{"rId20": {Blocks: []Block{
				paraOf(ParagraphProps{Justify: JustifyCenter, HasJustify: true}, textRun("HEADER", RunProps{}))}}},
			Footers: map[string]*HeaderFooter{"rId21": {Blocks: []Block{
				paraOf(ParagraphProps{}, textRun("footer", RunProps{}))}}},
			Body: []Block{
				paraOf(ParagraphProps{SectPr: &landscape}, textRun("landscape section end", RunProps{})),
				paraOf(ParagraphProps{}, textRun("letter section", RunProps{})),
			},
		}
	}},
	{"extra-parts", func() *Document {
		return &Document{
			ExtraParts: map[string][]byte{
				"customXml/item1.xml": []byte(`<app xmlns="urn:x"><keep attr="1">data</keep></app>`),
			},
			Body: []Block{paraOf(ParagraphProps{}, textRun("body", RunProps{}))},
		}
	}},
}

// TestModelRoundTrip pins the writer contract on the constructed corpus:
// Parse(Write(doc)) ≡ doc for every vocabulary feature.
func TestModelRoundTrip(t *testing.T) {
	for _, f := range modelCore {
		t.Run(f.name, func(t *testing.T) {
			assertRoundTrip(t, f.name, f.build())
		})
	}
}

// TestWriteDeterministic pins byte-identical output for repeated writes.
func TestWriteDeterministic(t *testing.T) {
	for _, f := range modelCore {
		doc := f.build()
		a, err := Bytes(doc)
		if err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
		b, err := Bytes(doc)
		if err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s: two writes differ", f.name)
		}
	}
}

// TestWriteInvalidDocument pins the hard-error contract: content the writer
// cannot represent faithfully fails loudly, never silently drops.
func TestWriteInvalidDocument(t *testing.T) {
	danglingDrawing := &Document{Body: []Block{paraOf(ParagraphProps{},
		ParaChild{Drawing: &Drawing{RelID: "rId99", WidthEMU: 1, HeightEMU: 1}})}}
	if _, err := Bytes(danglingDrawing); !errors.Is(err, ErrInvalidDocument) {
		t.Errorf("dangling drawing rel: want ErrInvalidDocument, got %v", err)
	}

	danglingLink := &Document{Body: []Block{paraOf(ParagraphProps{},
		ParaChild{Hyperlink: &Hyperlink{RelID: "rId42", Runs: []Run{{Text: "x"}}}})}}
	if _, err := Bytes(danglingLink); !errors.Is(err, ErrInvalidDocument) {
		t.Errorf("dangling hyperlink rel: want ErrInvalidDocument, got %v", err)
	}

	if _, err := Bytes(nil); !errors.Is(err, ErrInvalidDocument) {
		t.Errorf("nil document: want ErrInvalidDocument, got %v", err)
	}
}

// TestRandomizedRoundTrip drives seeded random documents through the
// round trip — hostile text, deep revision nesting, random property
// combinations — catching escaping/ordering bugs fixture tests miss.
func TestRandomizedRoundTrip(t *testing.T) {
	iterations := 200
	if testing.Short() {
		iterations = 40
	}
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic test corpus
	for i := 0; i < iterations; i++ {
		doc := randomDocument(rng)
		t.Run(fmt.Sprintf("seed1-doc%d", i), func(t *testing.T) {
			assertRoundTrip(t, "random", doc)
		})
	}
}

// hostileTexts are the strings most likely to expose escaping/whitespace bugs.
var hostileTexts = []string{
	"plain words",
	"escape <these> & \"those\" 'too'",
	" leading space",
	"trailing space ",
	"tab\there",
	"\ttab lead",
	"unicode — 標題 ünïcödé",
	"]]> cdata bait",
	"a",
}

func randomText(rng *rand.Rand) string { return hostileTexts[rng.Intn(len(hostileTexts))] }

func randomRunProps(rng *rand.Rand) RunProps {
	var p RunProps
	if rng.Intn(3) == 0 {
		p.Bold, p.HasBold = rng.Intn(2) == 0, true
	}
	if rng.Intn(3) == 0 {
		p.Italic, p.HasItalic = rng.Intn(2) == 0, true
	}
	if rng.Intn(4) == 0 {
		p.Underline, p.HasUnderline = true, true
		p.UnderlineStyle = []string{"single", "double", "wave"}[rng.Intn(3)]
	}
	if rng.Intn(4) == 0 {
		p.SizeHalfPts, p.HasSize = 16+2*rng.Intn(20), true
	}
	if rng.Intn(4) == 0 {
		p.Color, p.HasColor = rgba(uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256))), true
	}
	if rng.Intn(5) == 0 {
		p.Shd = Shading{Fill: rgba(uint8(rng.Intn(256)), 0x80, 0x20), HasFill: true}
	}
	if rng.Intn(5) == 0 {
		p.Family = []string{"Georgia", "Courier New", "Arial"}[rng.Intn(3)]
	}
	return p
}

func randomChildren(rng *rand.Rand, depth int) []ParaChild {
	n := 1 + rng.Intn(4)
	out := make([]ParaChild, 0, n)
	for i := 0; i < n; i++ {
		switch pick := rng.Intn(10); {
		case pick < 6:
			out = append(out, textRun(randomText(rng), randomRunProps(rng)))
		case pick < 7:
			out = append(out, ParaChild{Hyperlink: &Hyperlink{
				Target: fmt.Sprintf("https://example.com/%d?a=b&c=d", rng.Intn(5)),
				Runs:   []Run{{Text: randomText(rng), Props: randomRunProps(rng)}},
			}})
		case pick < 8 && depth < 3:
			kind := RevisionInsert
			if rng.Intn(2) == 0 {
				kind = RevisionDelete
			}
			out = append(out, ParaChild{Revision: &Revision{
				Kind: kind, ID: rng.Intn(100), Author: "Rnd", Date: "2026-03-03T00:00:00Z",
				Content: randomChildren(rng, depth+1),
			}})
		case pick < 9:
			out = append(out, ParaChild{Run: &Run{Break: []BreakKind{BreakLine, BreakPage, BreakColumn}[rng.Intn(3)]}})
		default:
			id := 1 + rng.Intn(9)
			out = append(out,
				ParaChild{CommentStart: &CommentMark{ID: id}},
				textRun(randomText(rng), RunProps{}),
				ParaChild{CommentEnd: &CommentMark{ID: id}},
			)
		}
	}
	return out
}

func randomDocument(rng *rand.Rand) *Document {
	doc := &Document{}
	blocks := 1 + rng.Intn(5)
	for i := 0; i < blocks; i++ {
		var props ParagraphProps
		if rng.Intn(3) == 0 {
			props.Justify = []Justify{JustifyLeft, JustifyCenter, JustifyRight, JustifyBoth}[rng.Intn(4)]
			props.HasJustify = true
		}
		if rng.Intn(4) == 0 {
			props.IndentLeft, props.HasIndentLeft = Twips(360+rng.Intn(1440)), true
		}
		doc.Body = append(doc.Body, paraOf(props, randomChildren(rng, 0)...))
	}
	// Any comment referenced by a marker gets a body (marker ids are 1..9).
	doc.Comments = map[int]*Comment{}
	for i := 1; i <= 9; i++ {
		doc.Comments[i] = &Comment{ID: i, Author: "Rnd",
			Body: []Block{paraOf(ParagraphProps{}, textRun("c", RunProps{}))}}
	}
	return doc
}

// Save-cycle idempotence over the generated package corpus lives in
// write_idempotence_test.go (package docx_test): the corpus builder
// testdata/gen/docx itself imports this package for the model-specimen
// fixture, so an in-package test importing it would form a test import cycle.
