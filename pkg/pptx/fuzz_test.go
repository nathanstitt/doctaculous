package pptx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// FuzzOpenBytes exercises the PPTX reader: the OPC container, the presentation
// part, the slide relationship graph, and each slide's shape tree.
//
// The property under test is that OpenBytes returns. A malformed package may
// fail with any error; what it may not do is panic, recurse without bound, or
// size an allocation from a number it read out of the file.
//
// Slides are walked after opening: shape trees nest (a group shape holds
// shapes), so a defect in that recursion only shows when the tree is consumed.
func FuzzOpenBytes(f *testing.F) {
	for _, s := range pptxSeeds() {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := OpenBytes(data)
		if err != nil || p == nil {
			return
		}
		for _, slide := range p.Slides {
			for range slide.Shapes {
			}
		}
	})
}

// pptxSeeds returns hand-built packages aimed at the container, the slide
// graph, and the numbers a slide can name.
func pptxSeeds() [][]byte {
	pkg := func(parts map[string]string) []byte {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		for name, body := range parts {
			w, err := z.Create(name)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte(body))
		}
		_ = z.Close()
		return buf.Bytes()
	}
	const ctypes = `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`
	const pns = `xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" ` +
		`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

	var out [][]byte
	// A well-formed shell, for the mutator to work from.
	out = append(out, pkg(map[string]string{
		"[Content_Types].xml":              ctypes,
		"ppt/presentation.xml":             `<?xml version="1.0"?><p:presentation ` + pns + `><p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst><p:sldSz cx="9144000" cy="6858000"/></p:presentation>`,
		"ppt/_rels/presentation.xml.rels":  `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":            `<?xml version="1.0"?><p:sld ` + pns + `><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>hi</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/slides/_rels/slide1.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
	}))
	// Slide dimensions taken from the file.
	out = append(out, pkg(map[string]string{
		"[Content_Types].xml":  ctypes,
		"ppt/presentation.xml": `<?xml version="1.0"?><p:presentation ` + pns + `><p:sldSz cx="2000000000" cy="2000000000"/></p:presentation>`,
	}))
	// A deeply nested group shape tree: the shape walk recurses per level.
	out = append(out, pkg(map[string]string{
		"[Content_Types].xml":             ctypes,
		"ppt/presentation.xml":            `<?xml version="1.0"?><p:presentation ` + pns + `><p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst><p:sldSz cx="9144000" cy="6858000"/></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>`,
		"ppt/slides/slide1.xml": `<?xml version="1.0"?><p:sld ` + pns + `><p:cSld><p:spTree>` +
			strings.Repeat(`<p:grpSp>`, 20000) + `<p:sp/>` + strings.Repeat(`</p:grpSp>`, 20000) +
			`</p:spTree></p:cSld></p:sld>`,
	}))
	// A slide relationship pointing at itself.
	out = append(out, pkg(map[string]string{
		"[Content_Types].xml":             ctypes,
		"ppt/presentation.xml":            `<?xml version="1.0"?><p:presentation ` + pns + `><p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="../presentation.xml"/></Relationships>`,
	}))
	// Many slide ids resolving to one part.
	out = append(out, pkg(map[string]string{
		"[Content_Types].xml": ctypes,
		"ppt/presentation.xml": `<?xml version="1.0"?><p:presentation ` + pns + `><p:sldIdLst>` +
			strings.Repeat(`<p:sldId id="256" r:id="rId1"/>`, 50000) + `</p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":           `<?xml version="1.0"?><p:sld ` + pns + `><p:cSld><p:spTree/></p:cSld></p:sld>`,
	}))
	// Structural degenerates.
	out = append(out, pkg(map[string]string{"[Content_Types].xml": ctypes}))
	out = append(out, []byte("PK\x03\x04 nope"), []byte{})
	return out
}
