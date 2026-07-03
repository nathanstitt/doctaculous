package docx

import (
	"archive/zip"
	"bytes"
	"errors"
	"image/color"
	"testing"
)

// buildDocx assembles a minimal valid .docx in memory from the given
// document.xml and (optional) styles.xml bodies. It is a test helper kept local
// to the parser tests; the shared fixture generator lives in testdata/gen/docx.
func buildDocx(t *testing.T, documentXML, stylesXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	write := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
</Types>`
	rootRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`
	docRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

	write("[Content_Types].xml", contentTypes)
	write("_rels/.rels", rootRels)
	write("word/_rels/document.xml.rels", docRels)
	write("word/document.xml", documentXML)
	if stylesXML != "" {
		write("word/styles.xml", stylesXML)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

const docHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`

func TestParseParagraphsAndRuns(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:p><w:r><w:rPr><w:b/><w:sz w:val="28"/><w:color w:val="FF0000"/></w:rPr><w:t>Hello </w:t></w:r><w:r><w:t xml:space="preserve">world</w:t></w:r></w:p>
  <w:p><w:r><w:t>Second</w:t></w:r></w:p>
  <w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:bottom="1440" w:left="1440" w:right="1440"/></w:sectPr>
</w:body></w:document>`

	d, err := OpenBytes(buildDocx(t, doc, ""))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if len(d.Body) != 2 {
		t.Fatalf("Body blocks = %d, want 2", len(d.Body))
	}
	p0 := d.Body[0].Paragraph
	if p0 == nil {
		t.Fatalf("paragraph 0 = nil, want a paragraph")
	}
	runs0 := runsOf(p0)
	if len(runs0) != 2 {
		t.Fatalf("paragraph 0 runs = %d, want 2", len(runs0))
	}
	if got := runs0[0].Text; got != "Hello " {
		t.Errorf("run 0 text = %q, want %q", got, "Hello ")
	}
	if got := runs0[1].Text; got != "world" {
		t.Errorf("run 1 text = %q, want %q (xml:space preserve)", got, "world")
	}
	if !runs0[0].Props.Bold || !runs0[0].Props.HasBold {
		t.Error("run 0 should be bold")
	}
	if runs0[0].Props.SizeHalfPts != 28 {
		t.Errorf("run 0 size = %d half-points, want 28", runs0[0].Props.SizeHalfPts)
	}
	if runs0[0].Props.Color != (color.RGBA{R: 0xff, A: 0xff}) {
		t.Errorf("run 0 color = %v, want red", runs0[0].Props.Color)
	}
}

func TestParseSectionGeometry(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:p><w:r><w:t>x</w:t></w:r></w:p>
  <w:sectPr><w:pgSz w:w="15840" w:h="12240"/><w:pgMar w:top="720" w:bottom="720" w:left="1080" w:right="1080"/></w:sectPr>
</w:body></w:document>`
	d, err := OpenBytes(buildDocx(t, doc, ""))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	s := d.Section
	if s.PageW != 15840 || s.PageH != 12240 {
		t.Errorf("page = %dx%d twips, want 15840x12240", s.PageW, s.PageH)
	}
	if s.MarginTop != 720 || s.MarginLeft != 1080 {
		t.Errorf("margins top=%d left=%d, want 720/1080", s.MarginTop, s.MarginLeft)
	}
}

func TestParseDefaultSectionWhenAbsent(t *testing.T) {
	doc := docHeader + `<w:body><w:p><w:r><w:t>x</w:t></w:r></w:p></w:body></w:document>`
	d, err := OpenBytes(buildDocx(t, doc, ""))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if d.Section.PageW != 12240 || d.Section.PageH != 15840 {
		t.Errorf("default page = %dx%d, want Letter 12240x15840", d.Section.PageW, d.Section.PageH)
	}
}

func TestParseBreakAndStyleRef(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:p><w:pPr><w:pStyle w:val="Heading1"/><w:jc w:val="center"/></w:pPr><w:r><w:t>Title</w:t></w:r></w:p>
  <w:p><w:r><w:t>before</w:t><w:br w:type="page"/><w:t>after</w:t></w:r></w:p>
</w:body></w:document>`
	d, err := OpenBytes(buildDocx(t, doc, ""))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if d.Body[0].Paragraph.Props.StyleID != "Heading1" {
		t.Errorf("style ref = %q, want Heading1", d.Body[0].Paragraph.Props.StyleID)
	}
	if d.Body[0].Paragraph.Props.Justify != JustifyCenter {
		t.Error("paragraph 0 should be centered")
	}
	// "before" / page break / "after" → three runs in order.
	runs := runsOf(d.Body[1].Paragraph)
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3 (before, break, after)", len(runs))
	}
	if runs[1].Break != BreakPage {
		t.Errorf("run 1 break = %v, want BreakPage", runs[1].Break)
	}
}

func TestParseNotDocx(t *testing.T) {
	if _, err := OpenBytes([]byte("not a zip")); err == nil {
		t.Error("OpenBytes(garbage): want error")
	} else if !errors.Is(err, ErrNotDocx) {
		t.Errorf("OpenBytes(garbage): err = %v, want ErrNotDocx", err)
	}
}

// TestParseDegradesGracefully proves malformed-but-zippy inputs return a typed
// error instead of panicking, per the project's degradation policy.
func TestParseDegradesGracefully(t *testing.T) {
	t.Run("malformed document XML", func(t *testing.T) {
		data := buildDocx(t, docHeader+`<w:body><w:p><w:r><w:t>oops`, "") // truncated, unclosed tags
		if _, err := OpenBytes(data); err == nil {
			t.Fatal("OpenBytes(malformed XML): want error, got nil")
		} else if !errors.Is(err, ErrMalformedXML) {
			t.Errorf("err = %v, want ErrMalformedXML", err)
		}
	})

	t.Run("missing main document part", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, err := zw.Create("[Content_Types].xml")
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write([]byte(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("zip close: %v", err)
		}
		if _, err := OpenBytes(buf.Bytes()); err == nil {
			t.Fatal("OpenBytes(no document.xml): want error, got nil")
		} else if !errors.Is(err, ErrMissingPart) {
			t.Errorf("err = %v, want ErrMissingPart", err)
		}
	})
}

// TestParseTextPreservesWhitespace confirms a <w:t>'s character data is kept
// verbatim — leading/trailing spaces are significant for inter-run spacing —
// whether or not xml:space="preserve" is present.
func TestParseTextPreservesWhitespace(t *testing.T) {
	doc := docHeader + `<w:body>
  <w:p><w:r><w:t xml:space="preserve"> spaced </w:t></w:r><w:r><w:t>untrimmed </w:t></w:r></w:p>
</w:body></w:document>`
	d, err := OpenBytes(buildDocx(t, doc, ""))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	runs := runsOf(d.Body[0].Paragraph)
	if got := runs[0].Text; got != " spaced " {
		t.Errorf("run 0 text = %q, want %q (whitespace preserved)", got, " spaced ")
	}
	if got := runs[1].Text; got != "untrimmed " {
		t.Errorf("run 1 text = %q, want %q (trailing space preserved without preserve attr)", got, "untrimmed ")
	}
}

// TestParseParagraphFillsContent verifies runs land in Paragraph.Content (not the
// removed Runs field) after the refactor.
func TestParseParagraphFillsContent(t *testing.T) {
	doc := mustParse(t, `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>alpha</w:t></w:r><w:r><w:t> beta</w:t></w:r></w:p>
</w:body></w:document>`)
	if len(doc.Body) != 1 || doc.Body[0].Paragraph == nil {
		t.Fatalf("body = %+v, want 1 paragraph block", doc.Body)
	}
	c := doc.Body[0].Paragraph.Content
	if len(c) != 2 {
		t.Fatalf("Content len = %d, want 2", len(c))
	}
	if c[0].Run == nil || c[0].Run.Text != "alpha" {
		t.Fatalf("Content[0] = %+v, want Run{alpha}", c[0])
	}
	if c[1].Run == nil || c[1].Run.Text != " beta" {
		t.Fatalf("Content[1] = %+v, want Run{ beta}", c[1])
	}
}

// mustParse parses a document.xml body string into a Document, failing the test
// on error. It parses the document part directly (no OPC package needed).
func mustParse(t *testing.T, documentXML string) *Document {
	t.Helper()
	doc := &Document{Section: defaultSection()}
	if err := parseDocument([]byte(documentXML), doc); err != nil {
		t.Fatalf("parseDocument: %v", err)
	}
	return doc
}

// runsOf collects the bare runs from a paragraph's Content, ignoring hyperlink
// groups and drawings. It keeps the existing parser tests (which assert on the
// pre-refactor Runs slice) readable after runs moved into Content.
func runsOf(p *Paragraph) []Run {
	var out []Run
	for _, c := range p.Content {
		if c.Run != nil {
			out = append(out, *c.Run)
		}
	}
	return out
}
