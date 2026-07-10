package docx

import (
	"image/color"

	"github.com/nathanstitt/doctaculous/pkg/docx"
)

// modelSpecimenDocx builds a document through the public pkg/docx MODEL and
// serializes it with docx.Bytes — the writer-first path a library consumer
// takes (construct → Write), as opposed to every other fixture's raw-XML path.
// Rendering it locks the full chain: model → Write → Parse → cascade → lower →
// raster. The content samples the writer vocabulary: styled headings from
// DefaultStyles, emphasis + shading runs, a hyperlink, a bordered table with a
// spanned shaded header, an ordered list starting at 4, a footnote marker, and
// a tracked change rendered final-state.
func modelSpecimenDocx() []byte {
	rgb := func(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 0xFF} }
	text := func(s string, props docx.RunProps) docx.ParaChild {
		return docx.ParaChild{Run: &docx.Run{Text: s, Props: props}}
	}
	para := func(props docx.ParagraphProps, children ...docx.ParaChild) docx.Block {
		return docx.Block{Paragraph: &docx.Paragraph{Props: props, Content: children}}
	}
	cell := func(blocks ...docx.Block) docx.TableCell {
		return docx.TableCell{GridSpan: 1, Blocks: blocks}
	}
	edge := docx.Border{Style: "single", SizeEighthPt: 4, Color: rgb(0x99, 0xA0, 0xA8), HasColor: true}
	allEdges := docx.BoxBorders{Top: edge, Bottom: edge, Left: edge, Right: edge}

	num := docx.NewNumbering()
	num.Abstract[0] = map[int]docx.NumLevel{0: {Format: docx.NumFmtDecimal, Text: "%1."}}
	num.Instances[1] = docx.NumInstance{AbstractID: 0,
		Overrides: map[int]docx.LevelOverride{0: {Start: 4, HasStart: true}}}

	notes := docx.NewNotes()
	notes.Add(1, []docx.Block{para(docx.ParagraphProps{}, text("A supporting note.", docx.RunProps{}))})

	header := cell(para(docx.ParagraphProps{}, text("Metric", docx.RunProps{Bold: true, HasBold: true})))
	header.GridSpan = 2
	header.Props.Shading = docx.Shading{Fill: rgb(0xEF, 0xF3, 0xF8), HasFill: true}
	header.Props.Borders = allEdges
	c1 := cell(para(docx.ParagraphProps{}, text("Latency", docx.RunProps{})))
	c1.Props.Borders = allEdges
	c2 := cell(para(docx.ParagraphProps{}, text("12ms", docx.RunProps{})))
	c2.Props.Borders = allEdges

	doc := &docx.Document{
		Styles:    docx.DefaultStyles(),
		Numbering: num,
		Footnotes: notes,
		Body: []docx.Block{
			para(docx.ParagraphProps{StyleID: "Heading1"}, text("Model Specimen", docx.RunProps{})),
			para(docx.ParagraphProps{},
				text("Constructed through the public model with ", docx.RunProps{}),
				text("bold", docx.RunProps{Bold: true, HasBold: true}),
				text(", ", docx.RunProps{}),
				text("shaded", docx.RunProps{Shd: docx.Shading{Fill: rgb(0xFF, 0xF2, 0xA8), HasFill: true}}),
				text(", and a ", docx.RunProps{}),
				docx.ParaChild{Hyperlink: &docx.Hyperlink{Target: "https://example.com/",
					Runs: []docx.Run{{Text: "hyperlink"}}}},
				text(".", docx.RunProps{}),
				docx.ParaChild{Run: &docx.Run{FootnoteRef: 1}},
			),
			para(docx.ParagraphProps{},
				text("Tracked: kept ", docx.RunProps{}),
				docx.ParaChild{Revision: &docx.Revision{Kind: docx.RevisionInsert, ID: 1, Author: "Spec",
					Content: []docx.ParaChild{text("inserted ", docx.RunProps{})}}},
				docx.ParaChild{Revision: &docx.Revision{Kind: docx.RevisionDelete, ID: 2, Author: "Spec",
					Content: []docx.ParaChild{text("GONE ", docx.RunProps{})}}},
				text("final.", docx.RunProps{}),
			),
			para(docx.ParagraphProps{HasNum: true, NumID: 1}, text("List starts at four", docx.RunProps{})),
			para(docx.ParagraphProps{HasNum: true, NumID: 1}, text("then five", docx.RunProps{})),
			{Table: &docx.Table{
				Grid:  []docx.Twips{4000, 4000},
				Props: docx.TableProps{Borders: docx.BoxBorders{Top: edge}},
				Rows: []docx.TableRow{
					{Props: docx.RowProps{IsHeader: true}, Cells: []docx.TableCell{header}},
					{Cells: []docx.TableCell{c1, c2}},
				},
			}},
		},
	}
	data, err := docx.Bytes(doc)
	if err != nil {
		panic("model specimen: " + err.Error()) // a fixture builder failure is a programming error
	}
	return data
}
