package docx

import (
	"archive/zip"
	"bytes"
	"testing"
)

// TestAllRelsResolvesInternalAndExternal covers the rel-resolution rule: an
// internal (package) target is joined to the source part's directory, while an
// external (TargetMode="External") target is kept verbatim with External set. It
// drives allRels directly on a pkgReader built from an in-memory .rels part.
func TestAllRelsResolvesInternalAndExternal(t *testing.T) {
	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>
  <Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/" TargetMode="External"/>
</Relationships>`

	pkg := pkgWithParts(t, map[string]string{
		"word/_rels/document.xml.rels": rels,
	})

	got := pkg.allRels("word/document.xml")
	if got == nil {
		t.Fatal("allRels returned nil, want two relationships")
	}

	internal, ok := got["rId7"]
	if !ok {
		t.Fatalf("allRels missing rId7; got %+v", got)
	}
	if internal.External {
		t.Errorf("rId7 External = true, want false")
	}
	// The internal target is joined to the word/ directory of the source part.
	if internal.Target != "word/media/image1.png" {
		t.Errorf("rId7 Target = %q, want word/media/image1.png", internal.Target)
	}

	external, ok := got["rId5"]
	if !ok {
		t.Fatalf("allRels missing rId5; got %+v", got)
	}
	if !external.External {
		t.Errorf("rId5 External = false, want true")
	}
	// The external target is kept verbatim (not joined to the part directory).
	if external.Target != "https://example.com/" {
		t.Errorf("rId5 Target = %q, want https://example.com/ verbatim", external.Target)
	}
}

// pkgWithParts builds a minimal valid OPC package (a [Content_Types].xml plus the
// given extra parts) in memory and opens it into a pkgReader, so unexported
// package-relative methods can be exercised directly.
func pkgWithParts(t *testing.T, parts map[string]string) *pkgReader {
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
	write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
</Types>`)
	for name, content := range parts {
		write(name, content)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	pkg, err := openPackage(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("openPackage: %v", err)
	}
	return pkg
}
