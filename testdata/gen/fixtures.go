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

// VectorPDF returns a single-page PDF that fills a red rectangle and strokes a
// blue diagonal line.
func VectorPDF() []byte {
	content := []byte(
		"1 0 0 rg 100 100 200 150 re f " + // red filled rectangle
			"0 0 1 RG 5 w 100 100 m 300 250 l S", // blue stroked line
	)
	return buildSinglePage(content, `<< >>`)
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
