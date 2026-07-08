package gen

import (
	"bytes"
	"fmt"
)

// TextPDF returns a single-page PDF that draws a short text string using the
// standard Helvetica font. MediaBox is US Letter.
func TextPDF() []byte {
	content := []byte("BT /F1 24 Tf 72 700 Td (Hello, doctaculous!) Tj ET")
	return buildSinglePage(content, `<< >>`)
}

// WeightedFontsPDF returns a single-page PDF that draws three lines with three
// non-embedded standard-14 base fonts — Helvetica (regular), Helvetica-Bold, and
// Times-Italic — so a renderer must substitute the matching WEIGHTED/SLANTED bundled
// face (not collapse them all to a regular face). None embed a program, exercising the
// standardSubstituteProgram path; the golden shows the bold line heavier and the italic
// line slanted relative to the regular line.
func WeightedFontsPDF() []byte {
	resources := `<< /Font <<
		/F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>
		/F2 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>
		/F3 << /Type /Font /Subtype /Type1 /BaseFont /Times-Italic >>
	>> >>`
	content := []byte(
		"BT /F1 28 Tf 72 700 Td (Regular Helvetica) Tj ET\n" +
			"BT /F2 28 Tf 72 640 Td (Bold Helvetica) Tj ET\n" +
			"BT /F3 28 Tf 72 580 Td (Italic Times) Tj ET")
	return buildSinglePage(content, resources)
}

// SeparationColorPDF returns a single-page PDF that fills a rectangle with a
// Separation ("spot") color at full ink (scn 1). The /Separation color space's
// tint-transform /Function (an inline Type 2 exponential) maps tint t to CMYK
// (0,1,1,0)·t — so full ink (tint 1) is RED. A conformant renderer evaluates the tint
// transform; the bug treated the single tint component as gray, rendering full ink as
// WHITE. The rect is at user (100,100)-(300,250). This locks down Separation tint
// transforms from the content stream through the rasterizer.
func SeparationColorPDF() []byte {
	// /CS1 = [/Separation /Spot /DeviceCMYK <tint fn>]; tint fn: C0=no ink, C1=CMYK red.
	cs := "/CS1 [ /Separation /Spot /DeviceCMYK " +
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 1 0] /N 1 >> ]"
	resources := fmt.Sprintf("<< /ColorSpace << %s >> >>", cs)
	content := []byte("/CS1 cs 1 scn 100 100 200 150 re f")
	return buildSinglePage(content, resources)
}

// VectorPDF returns a single-page PDF that fills a red rectangle and strokes a
// blue diagonal line.
func VectorPDF() []byte {
	content := []byte(
		"1 0 0 rg 100 100 200 150 re f " + // red filled rectangle
			"0 0 1 RG 5 w 100 100 m 300 250 l S", // blue stroked line
	)
	return buildSinglePage(content, `<< >>`)
}

// StrokeJoinsPDF returns a single-page PDF that strokes a thick open polyline
// three times, once per line join (miter, round, bevel), each row also varying
// the line cap (butt, round, square). It locks down the stroker's join and cap
// fidelity through the content-stream interpreter and rasterizer. The polyline
// is a shallow "V" so the joins form visible corners.
func StrokeJoinsPDF() []byte {
	var b bytes.Buffer
	// Three rows; each uses a distinct join (j) and cap (J) operator.
	// PDF: "J" = line cap, "j" = line join, "M" = miter limit, "w" = width.
	rows := []struct {
		y          int
		join, capN int // 0,1,2 maps to miter/round/bevel and butt/round/square
		r, g, bl   string
	}{
		{600, 0, 0, "1", "0", "0"}, // miter join, butt cap, red
		{450, 1, 1, "0", "1", "0"}, // round join, round cap, green
		{300, 2, 2, "0", "0", "1"}, // bevel join, square cap, blue
	}
	for _, row := range rows {
		// A "V": left-down to a vertex, then up-right — a sharp join in the middle.
		fmt.Fprintf(&b, "%s %s %s RG 16 w 10 M %d J %d j ",
			row.r, row.g, row.bl, row.capN, row.join)
		fmt.Fprintf(&b, "100 %d m 250 %d l 400 %d l S ",
			row.y, row.y-100, row.y)
	}
	return buildSinglePage(b.Bytes(), `<< >>`)
}

// EvenOddPDF returns a single-page PDF that fills a square-with-a-square-hole
// ("donut") using the even-odd rule (f*). Both subpaths wind the same direction,
// so the nonzero rule would fill the hole solid; even-odd must leave it empty.
// This locks down even-odd winding from content stream through rasterization.
func EvenOddPDF() []byte {
	content := []byte(
		"0 0.4 0.8 rg " + // blue-ish fill
			"100 100 400 400 re " + // outer square
			"200 200 200 200 re " + // inner square (same winding)
			"f*", // even-odd fill: inner square is a hole
	)
	return buildSinglePage(content, `<< >>`)
}

// FormXObjectPDF returns a single-page PDF whose content invokes a form XObject
// (Do) that draws a green rectangle. The form carries a /Matrix that translates
// its drawing, so a correct renderer must concatenate that matrix; the page also
// nests a q/cm around the Do to prove the form runs inside the caller's state.
// This locks down form-XObject recursion, /Matrix composition, and resource
// scoping (the form has its own /Resources) from content stream to raster.
func FormXObjectPDF() []byte {
	b := newBuilder()

	// The form's content: fill a 100x100 green square at the form's origin.
	formContent := []byte("0 1 0 rg 0 0 100 100 re f")
	form := b.addStreamForm(formContent)

	// Page content: translate by (200,200) via cm, then invoke the form, which
	// additionally translates by (50,50) via its own /Matrix.
	pageContent := []byte("q 1 0 0 1 200 200 cm /Fm0 Do Q")
	content := b.addStream("", pageContent)

	// Resources reference the form under /XObject /Fm0.
	resources := fmt.Sprintf("<< /XObject << /Fm0 %d 0 R >> >>", form)

	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	page := b.addObject(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Resources %s /Contents %d 0 R >>",
		pagesNum, resources, content))
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}

// addStreamForm appends a form XObject stream (Subtype /Form) with a /Matrix that
// translates by (50,50) and its own empty /Resources, returning its object number.
func (b *builder) addStreamForm(content []byte) int {
	dictExtra := " /Type /XObject /Subtype /Form /BBox [0 0 200 200] " +
		"/Matrix [1 0 0 1 50 50] /Resources << >>"
	return b.addStream(dictExtra, content)
}

// FormBBoxClipPDF returns a single-page PDF whose form XObject declares a small
// /BBox [0 0 50 50] but whose content fills a much larger 200x200 green square. A
// conformant renderer clips the form's painting to its BBox (ISO 32000 §8.10.1), so
// only the 50x50 region paints; the rest of the would-be 200x200 square stays white.
// The form sits at the page origin (identity /Matrix, no page cm), so in user space the
// BBox is (0,0)-(50,50) and the unclipped square would be (0,0)-(200,200). This locks
// down the /BBox clip: a point inside the BBox is green, a point outside it (but inside
// the drawn square) is white.
func FormBBoxClipPDF() []byte {
	b := newBuilder()

	// The form fills a 200x200 green square — larger than its 50x50 BBox.
	formContent := []byte("0 1 0 rg 0 0 200 200 re f")
	dictExtra := " /Type /XObject /Subtype /Form /BBox [0 0 50 50] " +
		"/Matrix [1 0 0 1 0 0] /Resources << >>"
	form := b.addStream(dictExtra, formContent)

	pageContent := []byte("/Fm0 Do")
	content := b.addStream("", pageContent)
	resources := fmt.Sprintf("<< /XObject << /Fm0 %d 0 R >> >>", form)

	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	page := b.addObject(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Resources %s /Contents %d 0 R >>",
		pagesNum, resources, content))
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}

// AlphaPDF returns a single-page PDF exercising ExtGState constant alpha. It
// draws an opaque red rectangle, then overlaps it with a blue rectangle painted
// under /ca 0.5 (50% fill opacity) and strokes a line at /CA 0.5. The overlap
// must show a blended (semi-transparent) blue over red rather than solid blue,
// locking down /ca and /CA from the gs operator through compositing.
func AlphaPDF() []byte {
	b := newBuilder()

	content := []byte(
		"1 0 0 rg 100 500 250 200 re f " + // opaque red rectangle
			"/GS0 gs " + // set fill+stroke alpha to 0.5
			"0 0 1 rg 200 450 250 200 re f " + // 50%-alpha blue rectangle (overlaps red)
			"0 0 1 RG 8 w 100 450 m 450 450 l S", // 50%-alpha blue stroke
	)
	contentNum := b.addStream("", content)

	// Resources: an ExtGState dict named GS0 with 50% fill and stroke alpha.
	resources := "<< /ExtGState << /GS0 << /ca 0.5 /CA 0.5 >> >> >>"

	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	page := b.addObject(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Resources %s /Contents %d 0 R >>",
		pagesNum, resources, contentNum))
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}

// BlendModePDF returns a single-page PDF exercising an ExtGState /BM blend mode.
// It fills an opaque red rectangle, sets the named blend mode via /GS0 gs, then
// fills an overlapping opaque blue rectangle. In the overlap the blue blends with
// the red backdrop per the mode (e.g. Multiply red×blue → black; Screen →
// magenta), locking down /BM from the gs operator through compositing.
func BlendModePDF(mode string) []byte {
	b := newBuilder()

	content := []byte(
		"1 0 0 rg 100 400 250 250 re f " + // opaque red rectangle
			"/GS0 gs " + // activate the blend mode
			"0 0 1 rg 225 300 250 250 re f", // blue rectangle overlapping the red
	)
	contentNum := b.addStream("", content)

	resources := fmt.Sprintf("<< /ExtGState << /GS0 << /BM /%s >> >> >>", mode)

	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	page := b.addObject(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Resources %s /Contents %d 0 R >>",
		pagesNum, resources, contentNum))
	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}

// MultiPagePDF returns an n-page PDF where each page draws its 1-based number.
func MultiPagePDF(n int) []byte {
	if n < 1 {
		n = 1
	}
	b := newBuilder()
	font := b.addObject(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`)

	// Reserve the Pages object number by allocating placeholders in order.
	// We compute object numbers manually for correct references.
	// Layout: font(=1), pages(=2), then for each page: content, page.
	pagesNum := font + 1
	pageObjNums := make([]int, n)
	contentNums := make([]int, n)

	// We can't know offsets until written, so write content+page objects after
	// the Pages object. Build the Pages object body referencing kids first.
	// To keep ordering simple, pre-assign numbers:
	next := pagesNum + 1
	for i := range n {
		contentNums[i] = next
		next++
		pageObjNums[i] = next
		next++
	}

	kids := &bytes.Buffer{}
	for i := range n {
		fmt.Fprintf(kids, "%d 0 R ", pageObjNums[i])
	}
	pagesBody := fmt.Sprintf(
		"<< /Type /Pages /Kids [ %s] /Count %d /MediaBox [0 0 612 792] /Resources << /Font << /F1 %d 0 R >> >> >>",
		kids.String(), n, font)
	gotPages := b.addObject(pagesBody)
	if gotPages != pagesNum {
		panic("gen: pages object number mismatch")
	}

	for i := range n {
		content := []byte(fmt.Sprintf("BT /F1 36 Tf 72 700 Td (Page %d) Tj ET", i+1))
		gotC := b.addStream("", content)
		if gotC != contentNums[i] {
			panic("gen: content object number mismatch")
		}
		pageBody := fmt.Sprintf("<< /Type /Page /Parent %d 0 R /Contents %d 0 R >>", pagesNum, gotC)
		gotP := b.addObject(pageBody)
		if gotP != pageObjNums[i] {
			panic("gen: page object number mismatch")
		}
	}

	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesNum))
	return b.finish(catalog)
}

// FlateTextPDF is like TextPDF but its content stream is FlateDecode-compressed,
// exercising the flate filter path.
func FlateTextPDF() []byte {
	raw := []byte("BT /F1 24 Tf 72 700 Td (Flate compressed) Tj ET")
	return buildSinglePageStream(raw, `<< >>`, true)
}

// buildSinglePage builds a one-page PDF with the given (uncompressed) content
// and resources dict body (e.g. "<< /Font << /F1 4 0 R >> >>").
func buildSinglePage(content []byte, resources string) []byte {
	return buildSinglePageStream(content, resources, false)
}

func buildSinglePageStream(content []byte, resources string, compress bool) []byte {
	b := newBuilder()
	font := b.addObject(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`)

	// If resources is the empty dict, still attach the font so text fixtures work.
	resBody := resources
	if resources == "<< >>" || resources == "<<>>" {
		resBody = fmt.Sprintf("<< /Font << /F1 %d 0 R >> >>", font)
	}

	var contentNum int
	if compress {
		contentNum = b.addStream(" /Filter /FlateDecode", zlibCompress(content))
	} else {
		contentNum = b.addStream("", content)
	}

	// The page object is added next; the Pages object follows it. Compute both
	// numbers up front so the cross-references are correct.
	pageNum := len(b.offsets)
	pagesNum := pageNum + 1
	pageBody := fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Resources %s /Contents %d 0 R >>",
		pagesNum, resBody, contentNum)
	page := b.addObject(pageBody)
	if page != pageNum {
		panic("gen: page object number mismatch in buildSinglePage")
	}

	pages := b.addObject(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	if pages != pagesNum {
		panic("gen: pages object number mismatch in buildSinglePage")
	}
	catalog := b.addObject(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages))
	return b.finish(catalog)
}
