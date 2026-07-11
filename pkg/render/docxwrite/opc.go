package docxwrite

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math"
	"strings"
	"time"
)

// assemblePackage serializes the writer's accumulated state into the OPC (zip)
// container. Part order and timestamps are fixed so output is deterministic —
// two writes of the same tree are byte-identical (mirroring pdfwrite's
// reproducibility guarantee).
func assemblePackage(w *writer, opts Options) ([]byte, error) {
	parts := []struct {
		name string
		data []byte
	}{
		{"[Content_Types].xml", []byte(contentTypesXML(w.mediaParts))},
		{"_rels/.rels", []byte(rootRelsXML)},
		{"word/document.xml", []byte(documentXML(w, opts))},
		{"word/_rels/document.xml.rels", []byte(documentRelsXML(w.rels))},
		{"word/styles.xml", []byte(stylesXML())},
		{"word/numbering.xml", []byte(numberingXML(w.orderedLists))},
	}
	for _, m := range w.mediaParts {
		parts = append(parts, struct {
			name string
			data []byte
		}{m.name, m.data})
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	stamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range parts {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate, Modified: stamp})
		if err != nil {
			return nil, fmt.Errorf("create part %s: %w", p.name, err)
		}
		if _, err := f.Write(p.data); err != nil {
			return nil, fmt.Errorf("write part %s: %w", p.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close package: %w", err)
	}
	return buf.Bytes(), nil
}

// documentXML wraps the accumulated body in the document root plus the final
// section properties (page size and margins).
func documentXML(w *writer, opts Options) string {
	pageW, pageH := opts.PageWidthPt, opts.PageHeightPt
	if pageW <= 0 {
		pageW = 612 // US Letter
	}
	if pageH <= 0 {
		pageH = 792
	}
	margin := opts.MarginPt
	switch {
	case margin < 0:
		margin = 0
	case margin == 0:
		margin = 72 // Word's 1in default
	}
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`)
	sb.WriteString(w.body.String())
	fmt.Fprintf(&sb, `<w:sectPr><w:pgSz w:w="%d" w:h="%d"/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="720" w:footer="720"/></w:sectPr>`,
		twips(pageW), twips(pageH), twips(margin), twips(margin), twips(margin), twips(margin))
	sb.WriteString("</w:body></w:document>\n")
	return sb.String()
}

// twips converts points to twentieths of a point.
func twips(pt float64) int { return int(math.Round(pt * 20)) }

// contentTypesXML declares the package's part content types. The repo's reader
// only checks the part exists, but real consumers (Word, LibreOffice) validate
// it, so the declarations are complete — including a Default per embedded image
// extension.
func contentTypesXML(media []mediaPart) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
`)
	seen := map[string]bool{}
	for _, m := range media {
		if seen[m.ext] {
			continue
		}
		seen[m.ext] = true
		sb.WriteString(`<Default Extension="` + m.ext + `" ContentType="image/` + m.ext + `"/>` + "\n")
	}
	sb.WriteString(`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>
</Types>
`)
	return sb.String()
}

// rootRelsXML points the package at the main document part.
const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>
`

// documentRelsXML lists the main part's relationships: styles and numbering at
// their reserved ids, then the writer-allocated rels (external hyperlinks).
func documentRelsXML(rels []docRel) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
`)
	for _, r := range rels {
		sb.WriteString(`<Relationship Id="` + escAttr.Replace(r.id) + `" Type="` + escAttr.Replace(r.relType) + `" Target="` + escAttr.Replace(r.target) + `"`)
		if r.external {
			sb.WriteString(` TargetMode="External"`)
		}
		sb.WriteString("/>\n")
	}
	sb.WriteString("</Relationships>\n")
	return sb.String()
}
