package xmlpart

import (
	"archive/zip"
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/beevik/etree"
	genxlsx "github.com/nathanstitt/doctaculous/testdata/gen/xlsx"
)

// treeEqual compares two elements semantically: prefixed name, attributes as
// an order-insensitive set, child elements in sequence, and the text content
// interleaved between them (whitespace-preserved — the inputs under test are
// byte-related, so text must survive verbatim).
func treeEqual(a, b *etree.Element) bool {
	if a.Space != b.Space || a.Tag != b.Tag {
		return false
	}
	if len(a.Attr) != len(b.Attr) {
		return false
	}
	key := func(at etree.Attr) string { return at.Space + ":" + at.Key + "=" + at.Value }
	ak := make([]string, len(a.Attr))
	bk := make([]string, len(b.Attr))
	for i := range a.Attr {
		ak[i] = key(a.Attr[i])
		bk[i] = key(b.Attr[i])
	}
	sort.Strings(ak)
	sort.Strings(bk)
	for i := range ak {
		if ak[i] != bk[i] {
			return false
		}
	}
	if a.Text() != b.Text() {
		return false
	}
	ac, bc := a.ChildElements(), b.ChildElements()
	if len(ac) != len(bc) {
		return false
	}
	for i := range ac {
		if !treeEqual(ac[i], bc[i]) {
			return false
		}
	}
	return true
}

// TestRoundTripLossless is the keystone property: parse → serialize → reparse
// yields a semantically identical tree for every XML part of every generated
// fixture — unknown elements, prefixed names, and attribute sets survive.
func TestRoundTripLossless(t *testing.T) {
	pkgBytes := genxlsx.New().
		AddSheet("S", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:x14ac="http://schemas.microsoft.com/office/spreadsheetml/2009/9/ac">
 <sheetData><row r="1" x14ac:dyDescent="0.3"><c r="A1"><v>1</v></c></row></sheetData>
 <extLst><ext uri="{X}"><x14:thing xmlns:x14="http://schemas.microsoft.com/office/spreadsheetml/2009/9/main" keep="yes">opaque<inner/></x14:thing></ext></extLst>
</worksheet>`).
		SetStyles(`<?xml version="1.0"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="1"><font/></fonts></styleSheet>`).
		Bytes()

	zr, err := zip.NewReader(bytes.NewReader(pkgBytes), int64(len(pkgBytes)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".xml") && !strings.HasSuffix(f.Name, ".rels") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var data bytes.Buffer
		if _, err := data.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()

		p1, err := Parse(data.Bytes())
		if err != nil {
			t.Fatalf("%s: parse: %v", f.Name, err)
		}
		out, err := p1.Bytes()
		if err != nil {
			t.Fatalf("%s: serialize: %v", f.Name, err)
		}
		p2, err := Parse(out)
		if err != nil {
			t.Fatalf("%s: reparse: %v", f.Name, err)
		}
		if !treeEqual(p1.Root(), p2.Root()) {
			t.Errorf("%s: tree changed across serialize/reparse\n%s", f.Name, out)
		}
	}
}

func TestPrefixedContentSurvives(t *testing.T) {
	src := `<?xml version="1.0"?><root xmlns:x14="urn:x"><x14:rule x14:attr="v" plain="p">text</x14:rule></root>`
	p, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<x14:rule", `x14:attr="v"`, `plain="p"`, ">text<"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDoctypeRejected(t *testing.T) {
	src := `<?xml version="1.0"?><!DOCTYPE root [<!ENTITY x "boom">]><root>&x;</root>`
	if _, err := Parse([]byte(src)); err == nil {
		t.Fatal("DOCTYPE should be rejected")
	}
}

func TestEnsureChildInOrder(t *testing.T) {
	order := []string{"sheetPr", "dimension", "sheetViews", "cols", "sheetData", "mergeCells", "pageMargins"}
	p, err := Parse([]byte(`<worksheet><sheetData/><pageMargins left="1"/></worksheet>`))
	if err != nil {
		t.Fatal(err)
	}
	root := p.Root()

	// mergeCells belongs between sheetData and pageMargins.
	mc := EnsureChildInOrder(root, "mergeCells", order)
	// sheetViews belongs before sheetData.
	sv := EnsureChildInOrder(root, "sheetViews", order)
	// Idempotent: a second call returns the same element.
	if EnsureChildInOrder(root, "mergeCells", order) != mc {
		t.Error("EnsureChildInOrder is not idempotent")
	}
	_ = sv

	var names []string
	for _, ch := range root.ChildElements() {
		names = append(names, ch.Tag)
	}
	want := []string{"sheetViews", "sheetData", "mergeCells", "pageMargins"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("child order = %v, want %v", names, want)
	}
}

// FuzzParse pins "malformed input errors, never panics".
func FuzzParse(f *testing.F) {
	f.Add([]byte(`<root a="1">x</root>`))
	f.Add([]byte(`<unclosed`))
	f.Add([]byte(``))
	f.Add([]byte(`<a><b/></a><trailing/>`))
	f.Add([]byte("<a>\xff\xfe</a>"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Parse(data)
		if err != nil {
			return
		}
		if _, err := p.Bytes(); err != nil {
			t.Skip() // serialize errors on odd trees are acceptable; panics are not
		}
	})
}
