package docxwrite

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	"github.com/nathanstitt/doctaculous/pkg/resource"
)

// firstTable returns the body's first table.
func firstTable(t *testing.T, d *docx.Document) *docx.Table {
	t.Helper()
	for _, b := range d.Body {
		if b.Table != nil {
			return b.Table
		}
	}
	t.Fatalf("no table in body")
	return nil
}

// cellText concatenates a cell's paragraph text.
func cellText(c docx.TableCell) string {
	var sb strings.Builder
	for _, blk := range c.Blocks {
		if blk.Paragraph != nil {
			sb.WriteString(textOf(blk.Paragraph))
		}
	}
	return sb.String()
}

func TestTableSimple(t *testing.T) {
	d := reopen(t, writeHTML(t, `<html><body><table>
	<tr><th>A</th><th>B</th></tr>
	<tr><td>1</td><td>2</td></tr>
	</table></body></html>`, Options{}))
	tb := firstTable(t, d)
	if len(tb.Grid) != 2 {
		t.Errorf("tblGrid columns = %d, want 2", len(tb.Grid))
	}
	if len(tb.Rows) != 2 || len(tb.Rows[0].Cells) != 2 {
		t.Fatalf("table shape = %d rows / %d cells", len(tb.Rows), len(tb.Rows[0].Cells))
	}
	if !tb.Rows[0].Props.IsHeader {
		t.Errorf("header row lost tblHeader")
	}
	if got := cellText(tb.Rows[1].Cells[0]); got != "1" {
		t.Errorf("cell text = %q", got)
	}
}

func TestTableColspan(t *testing.T) {
	d := reopen(t, writeHTML(t, `<html><body><table>
	<tr><td colspan="2">wide</td></tr>
	<tr><td>a</td><td>b</td></tr>
	</table></body></html>`, Options{}))
	tb := firstTable(t, d)
	if tb.Rows[0].Cells[0].GridSpan != 2 {
		t.Errorf("gridSpan = %d, want 2", tb.Rows[0].Cells[0].GridSpan)
	}
}

func TestTableRowspan(t *testing.T) {
	d := reopen(t, writeHTML(t, `<html><body><table>
	<tr><td rowspan="2">tall</td><td>r1</td></tr>
	<tr><td>r2</td></tr>
	</table></body></html>`, Options{}))
	tb := firstTable(t, d)
	if got := tb.Rows[0].Cells[0].VMerge; got != docx.VMergeRestart {
		t.Errorf("row 0 vMerge = %v, want restart", got)
	}
	// The continuation cell must be explicit (a bare vMerge would mean restart).
	if got := tb.Rows[1].Cells[0].VMerge; got != docx.VMergeContinue {
		t.Errorf("row 1 vMerge = %v, want continue", got)
	}
	if got := cellText(tb.Rows[1].Cells[1]); got != "r2" {
		t.Errorf("row 1 content cell = %q", got)
	}
}

func TestTableCombinedSpans(t *testing.T) {
	// A 3x3 with a 2x2 block: the continuation row must repeat the origin's
	// gridSpan so the grid stays aligned.
	d := reopen(t, writeHTML(t, `<html><body><table>
	<tr><td colspan="2" rowspan="2">block</td><td>c3</td></tr>
	<tr><td>x</td></tr>
	<tr><td>a</td><td>b</td><td>c</td></tr>
	</table></body></html>`, Options{}))
	tb := firstTable(t, d)
	cont := tb.Rows[1].Cells[0]
	if cont.VMerge != docx.VMergeContinue || cont.GridSpan != 2 {
		t.Errorf("continuation cell = vMerge %v gridSpan %d, want continue/2", cont.VMerge, cont.GridSpan)
	}
	if len(tb.Rows[2].Cells) != 3 {
		t.Errorf("final row cells = %d, want 3", len(tb.Rows[2].Cells))
	}
}

func TestTableCellStyling(t *testing.T) {
	d := reopen(t, writeHTML(t, `<html><body><table>
	<tr><td style="border: 2px solid #336699; background-color: #ffeecc">styled</td></tr>
	</table></body></html>`, Options{}))
	cell := firstTable(t, d).Rows[0].Cells[0]
	if cell.Props.Borders.Top.SizeEighthPt != 16 {
		t.Errorf("border sz = %d eighth-pt, want 16 (2pt)", cell.Props.Borders.Top.SizeEighthPt)
	}
	if c := cell.Props.Borders.Top.Color; !cell.Props.Borders.Top.HasColor || c.R != 0x33 || c.G != 0x66 || c.B != 0x99 {
		t.Errorf("border color = %+v, want 336699", c)
	}
	if f := cell.Props.Shading.Fill; !cell.Props.Shading.HasFill || f.R != 0xFF || f.G != 0xEE || f.B != 0xCC {
		t.Errorf("shading = %+v, want FFEECC", cell.Props.Shading)
	}
}

func TestTableCaption(t *testing.T) {
	d := reopen(t, writeHTML(t, `<html><body><table>
	<caption>Quarterly</caption><tr><td>x</td></tr>
	</table></body></html>`, Options{}))
	ps := paragraphs(d)
	if len(ps) == 0 || ps[0].Props.StyleID != "Caption" || textOf(ps[0]) != "Quarterly" {
		t.Fatalf("caption paragraph missing or wrong: %+v", ps)
	}
}

// tinyPNG encodes a small PNG for embedding tests.
func tinyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageEmbedding(t *testing.T) {
	pngData := tinyPNG(t, 40, 20)
	loader := resource.MapLoader{"pic.png": {Data: pngData, ContentType: "image/png"}}
	src := `<html><body><p>before <img src="pic.png" alt="a pic"> after</p>
	<p><img src="pic.png" alt="again"></p></body></html>`
	d := reopen(t, writeHTML(t, src, Options{Loader: loader}))

	var drawings []*docx.Drawing
	for _, b := range d.Body {
		if b.Paragraph == nil {
			continue
		}
		for _, c := range b.Paragraph.Content {
			if c.Drawing != nil {
				drawings = append(drawings, c.Drawing)
			}
		}
	}
	if len(drawings) != 2 {
		t.Fatalf("drawings = %d, want 2", len(drawings))
	}
	// Both reference the SAME deduped media part, byte-identical to the source.
	if drawings[0].RelID != drawings[1].RelID {
		t.Errorf("same src produced two rels: %q vs %q", drawings[0].RelID, drawings[1].RelID)
	}
	rel, ok := d.Rels[drawings[0].RelID]
	if !ok {
		t.Fatalf("image rel %q missing", drawings[0].RelID)
	}
	data, ok := d.Media[rel.Target]
	if !ok {
		t.Fatalf("media part %q missing (have %v)", rel.Target, len(d.Media))
	}
	if !bytes.Equal(data, pngData) {
		t.Errorf("media bytes differ from the source image")
	}
	// Extent from the intrinsic 40x20 px size.
	if drawings[0].WidthEMU != 40*emuPerPx || drawings[0].HeightEMU != 20*emuPerPx {
		t.Errorf("extent = %dx%d EMU, want %dx%d", drawings[0].WidthEMU, drawings[0].HeightEMU, 40*emuPerPx, 20*emuPerPx)
	}
}

func TestImageExplicitSizeWins(t *testing.T) {
	loader := resource.MapLoader{"pic.png": {Data: tinyPNG(t, 40, 20)}}
	d := reopen(t, writeHTML(t, `<html><body><p><img src="pic.png" width="100" height="50"></p></body></html>`,
		Options{Loader: loader}))
	for _, b := range d.Body {
		if b.Paragraph == nil {
			continue
		}
		for _, c := range b.Paragraph.Content {
			if c.Drawing != nil {
				if c.Drawing.WidthEMU != 100*emuPerPx || c.Drawing.HeightEMU != 50*emuPerPx {
					t.Errorf("extent = %dx%d EMU, want attribute-driven %dx%d",
						c.Drawing.WidthEMU, c.Drawing.HeightEMU, 100*emuPerPx, 50*emuPerPx)
				}
				return
			}
		}
	}
	t.Fatalf("no drawing found")
}

func TestLinkedImageKeepsImage(t *testing.T) {
	// A drawing cannot live inside the model's Hyperlink (runs only), so a
	// linked image is emitted BESIDE its link group: the image survives — the
	// reader dropped drawings inside w:hyperlink entirely — and the link text
	// keeps its target on both sides of the split.
	loader := resource.MapLoader{"pic.png": {Data: tinyPNG(t, 8, 8), ContentType: "image/png"}}
	d := reopen(t, writeHTML(t,
		`<html><body><p><a href="https://x.test/">before <img src="pic.png" alt="icon"> after</a></p></body></html>`,
		Options{Loader: loader}))
	ps := paragraphs(d)
	var links []*docx.Hyperlink
	var drawing *docx.Drawing
	for _, c := range ps[0].Content {
		if c.Hyperlink != nil {
			links = append(links, c.Hyperlink)
		}
		if c.Drawing != nil {
			drawing = c.Drawing
		}
	}
	if drawing == nil {
		t.Fatalf("linked image lost")
	}
	if len(links) != 2 {
		t.Fatalf("link groups = %d, want 2 (split around the image)", len(links))
	}
	for i, l := range links {
		if rel := d.Rels[l.RelID]; rel.Target != "https://x.test/" {
			t.Errorf("link group %d target = %q", i, rel.Target)
		}
	}
	if textOf(ps[0]) != "before  after" {
		t.Errorf("link text = %q", textOf(ps[0]))
	}
}

func TestImageDegradesWithoutLoader(t *testing.T) {
	var logged bool
	d := reopen(t, writeHTML(t, `<html><body><p>x <img src="pic.png" alt="fallback alt"> y</p></body></html>`,
		Options{Logf: func(string, ...any) { logged = true }}))
	ps := paragraphs(d)
	if got := textOf(ps[0]); !strings.Contains(got, "fallback alt") {
		t.Errorf("alt text lost: %q", got)
	}
	if !logged {
		t.Errorf("degradation not logged")
	}
	if len(d.Media) != 0 {
		t.Errorf("unexpected media parts: %v", len(d.Media))
	}
}
