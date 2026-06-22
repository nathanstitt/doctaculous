package pdf

import (
	"bytes"
	"sync"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
)

func TestLexerBasics(t *testing.T) {
	src := []byte(`<< /Type /Page /Count 3 /Flag true /Name (hi) /Hex <48656C> >>`)
	p := newObjParser(src)
	obj, err := p.parseObject()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, ok := obj.(Dict)
	if !ok {
		t.Fatalf("expected Dict, got %T", obj)
	}
	if n, _ := d["Type"].(Name); n != "Page" {
		t.Errorf("Type = %v, want Page", d["Type"])
	}
	if c, _ := d["Count"].(Integer); c != 3 {
		t.Errorf("Count = %v, want 3", d["Count"])
	}
	if f, _ := d["Flag"].(Boolean); !bool(f) {
		t.Errorf("Flag = %v, want true", d["Flag"])
	}
	if s, _ := d["Name"].(String); string(s) != "hi" {
		t.Errorf("Name = %q, want hi", d["Name"])
	}
	if hx, _ := d["Hex"].(String); !bytes.Equal([]byte(hx), []byte{0x48, 0x65, 0x6C}) {
		t.Errorf("Hex = %v, want [48 65 6C]", []byte(d["Hex"].(String)))
	}
}

func TestLexerReference(t *testing.T) {
	p := newObjParser([]byte(`12 0 R`))
	obj, err := p.parseObject()
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := obj.(Reference)
	if !ok {
		t.Fatalf("expected Reference, got %T", obj)
	}
	if ref.Number != 12 || ref.Generation != 0 {
		t.Errorf("ref = %v, want 12 0 R", ref)
	}
}

func TestLexerIntegerNotReference(t *testing.T) {
	// "12 0" with no R should yield Integer 12 then Integer 0 in an array.
	p := newObjParser([]byte(`[12 0 5]`))
	obj, err := p.parseObject()
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := obj.(Array)
	if !ok || len(arr) != 3 {
		t.Fatalf("expected 3-element array, got %T %v", obj, obj)
	}
	for i, want := range []int64{12, 0, 5} {
		if got, _ := arr[i].(Integer); int64(got) != want {
			t.Errorf("arr[%d] = %v, want %d", i, arr[i], want)
		}
	}
}

func TestLiteralStringEscapes(t *testing.T) {
	p := newObjParser([]byte(`(a\nb\(c\)\\d\101)`))
	obj, err := p.parseObject()
	if err != nil {
		t.Fatal(err)
	}
	s, _ := obj.(String)
	want := "a\nb(c)\\dA" // \101 octal = 'A'
	if string(s) != want {
		t.Errorf("string = %q, want %q", s, want)
	}
}

func TestParseTextPDF(t *testing.T) {
	doc, err := Parse(gen.TextPDF())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.PageCount() != 1 {
		t.Fatalf("PageCount = %d, want 1", doc.PageCount())
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatal(err)
	}
	if pg.MediaBox.Width() != 612 || pg.MediaBox.Height() != 792 {
		t.Errorf("MediaBox = %+v, want 612x792", pg.MediaBox)
	}
	content, err := pg.ContentBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("Hello, doctaculous!")) {
		t.Errorf("content missing text: %q", content)
	}
	// Resources should expose the font.
	if pg.Resources["Font"] == nil {
		t.Errorf("expected /Font in resources, got %v", pg.Resources)
	}
}

func TestParseFlateContent(t *testing.T) {
	doc, err := Parse(gen.FlateTextPDF())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pg, _ := doc.Page(0)
	content, err := pg.ContentBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("Flate compressed")) {
		t.Errorf("decompressed content missing text: %q", content)
	}
}

func TestParseMultiPage(t *testing.T) {
	doc, err := Parse(gen.MultiPagePDF(5))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.PageCount() != 5 {
		t.Fatalf("PageCount = %d, want 5", doc.PageCount())
	}
	for i := range 5 {
		pg, err := doc.Page(i)
		if err != nil {
			t.Fatal(err)
		}
		content, err := pg.ContentBytes()
		if err != nil {
			t.Fatal(err)
		}
		want := []byte("Page ")
		if !bytes.Contains(content, want) {
			t.Errorf("page %d content = %q, want to contain %q", i, content, want)
		}
	}
}

// TestConcurrentAccess validates that a parsed Document is safe for concurrent
// page access (the cache locking). Run with -race to catch data races.
func TestConcurrentAccess(t *testing.T) {
	doc, err := Parse(gen.MultiPagePDF(8))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for range 20 {
				idx := (seed * 3) % doc.PageCount()
				pg, err := doc.Page(idx)
				if err != nil {
					t.Errorf("Page(%d): %v", idx, err)
					return
				}
				if _, err := pg.ContentBytes(); err != nil {
					t.Errorf("ContentBytes: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestParseEmptyFails(t *testing.T) {
	_, err := Parse([]byte("not a pdf"))
	if err == nil {
		t.Fatal("expected error parsing non-PDF data")
	}
}

// TestCoreFixtures asserts the canonical contract every layer relies on: each
// fixture in gen.Core parses to a valid Document, reports its declared page
// count, and lets every page's content stream decode without error. Feature-
// specific assertions (rotation values, image resources) live in the dedicated
// tests above; this is the uniform "the core set is sound" gate.
func TestCoreFixtures(t *testing.T) {
	for _, f := range gen.Core {
		t.Run(f.Name, func(t *testing.T) {
			doc, err := Parse(f.Bytes())
			if err != nil {
				t.Fatalf("Parse (%s): %v", f.Desc, err)
			}
			if got := doc.PageCount(); got != f.Pages {
				t.Fatalf("PageCount = %d, want %d", got, f.Pages)
			}
			for i := range f.Pages {
				pg, err := doc.Page(i)
				if err != nil {
					t.Fatalf("Page(%d): %v", i, err)
				}
				if pg.MediaBox.Width() <= 0 || pg.MediaBox.Height() <= 0 {
					t.Errorf("page %d: empty MediaBox %+v", i, pg.MediaBox)
				}
				if _, err := pg.ContentBytes(); err != nil {
					t.Errorf("page %d: ContentBytes: %v", i, err)
				}
			}
		})
	}
}

func TestParseRotated(t *testing.T) {
	for _, in := range []struct{ deg, want int }{
		{90, 90}, {180, 180}, {270, 270}, {360, 0}, {-90, 270},
	} {
		doc, err := Parse(gen.RotatedPDF(in.deg))
		if err != nil {
			t.Fatalf("Parse(rotate %d): %v", in.deg, err)
		}
		pg, err := doc.Page(0)
		if err != nil {
			t.Fatal(err)
		}
		if pg.Rotate != in.want {
			t.Errorf("Rotate for %d = %d, want %d", in.deg, pg.Rotate, in.want)
		}
	}
}

func TestParseXRefStream(t *testing.T) {
	doc, err := Parse(gen.XRefStreamPDF())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.PageCount() != 1 {
		t.Fatalf("PageCount = %d, want 1", doc.PageCount())
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatal(err)
	}
	content, err := pg.ContentBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("XRef stream PDF")) {
		t.Errorf("content missing text: %q", content)
	}
}

func TestParseObjStm(t *testing.T) {
	doc, err := Parse(gen.ObjStmPDF())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.PageCount() != 1 {
		t.Fatalf("PageCount = %d, want 1", doc.PageCount())
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatal(err)
	}
	content, err := pg.ContentBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("Object stream PDF")) {
		t.Errorf("content missing text: %q", content)
	}
}

func TestParseImage(t *testing.T) {
	for name, data := range map[string][]byte{
		"flate": gen.ImagePDF(),
		"jpeg":  gen.JPEGImagePDF(),
	} {
		doc, err := Parse(data)
		if err != nil {
			t.Fatalf("Parse(%s): %v", name, err)
		}
		pg, err := doc.Page(0)
		if err != nil {
			t.Fatalf("Page(%s): %v", name, err)
		}
		if pg.Resources["XObject"] == nil {
			t.Errorf("%s: expected /XObject in resources, got %v", name, pg.Resources)
		}
	}
}

// TestMalformedNoPanic asserts the parser degrades gracefully: each fixture is
// broken in one way, and Parse must return (doc-or-error) without panicking.
func TestMalformedNoPanic(t *testing.T) {
	fixtures := map[string][]byte{
		"truncated":       gen.TruncatedPDF(),
		"bad-xref-offset": gen.BadXrefOffsetPDF(),
		"missing-endobj":  gen.MissingEndobjPDF(),
		"no-header":       gen.NoHeaderPDF(),
		"bad-stream-len":  gen.BadStreamLengthPDF(),
	}
	for name, data := range fixtures {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse panicked on %s fixture: %v", name, r)
				}
			}()
			// We do not assert success or failure here — only that whatever the
			// parser decides, it does so without crashing the batch.
			_, _ = Parse(data)
		})
	}
}
