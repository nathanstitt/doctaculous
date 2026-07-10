package docx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidDocument reports a Document the writer cannot serialize faithfully
// — e.g. a Drawing whose relationship resolves to no media part. The writer
// fails hard rather than silently dropping content: a caller's save cycle must
// never lose data quietly.
var ErrInvalidDocument = errors.New("invalid document")

// Write serializes doc to w as a complete WordprocessingML (.docx) package.
// It is the inverse of Open for the modeled vocabulary: Parse(Write(doc)) ≡ doc
// (see the round-trip tests for the one normalization — freshly allocated
// hyperlink relationship ids). Output is deterministic: fixed part order and
// timestamps, sorted iteration of every map — two writes of the same document
// are byte-identical. doc is read-only during Write (safe to share).
//
// Degenerate content the reader cannot represent is normalized rather than
// errored: a run with no text, break, or reference is omitted (the parser
// would drop it anyway).
func Write(w io.Writer, doc *Document) error {
	data, err := Bytes(doc)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("docx: write: %w", err)
	}
	return nil
}

// Bytes is Write into a fresh byte slice.
func Bytes(doc *Document) ([]byte, error) {
	if doc == nil {
		return nil, fmt.Errorf("docx: %w: nil document", ErrInvalidDocument)
	}
	dw := newDocWriter(doc)
	parts, err := dw.assemble()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	stamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range parts {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate, Modified: stamp})
		if err != nil {
			return nil, fmt.Errorf("docx: create part %s: %w", p.name, err)
		}
		if _, err := f.Write(p.data); err != nil {
			return nil, fmt.Errorf("docx: write part %s: %w", p.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("docx: close package: %w", err)
	}
	return buf.Bytes(), nil
}

// The writers build WordprocessingML by direct string assembly with explicit
// escapers — encoding/xml cannot emit the prefixed form (<w:p>) real-world
// OOXML consumers expect (it rewrites elements with default-namespace
// declarations, ballooning output and hurting interop).
var (
	escXMLText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	escXMLAttr = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
)

// wpart is one OPC part ready for the zip.
type wpart struct {
	name string
	data []byte
}

// docWriter accumulates the serialization state for one Write call.
type docWriter struct {
	doc *Document
	// rels is the outgoing relationship set for the main part, keyed by id —
	// the document's own rels plus any the writer allocates (structural parts,
	// Target-only hyperlinks).
	rels map[string]Relationship
	// linkRel memoizes allocated hyperlink rel ids by URL so one URL gets one id.
	linkRel map[string]string
	// nextRel is the next numeric suffix for allocated "rIdN" ids.
	nextRel int
	// drawings counts emitted drawings for unique docPr ids.
	drawings int
}

func newDocWriter(doc *Document) *docWriter {
	dw := &docWriter{doc: doc, rels: map[string]Relationship{}, linkRel: map[string]string{}, nextRel: 1}
	for id, rel := range doc.Rels {
		dw.rels[id] = rel
		if n, ok := relNum(id); ok && n >= dw.nextRel {
			dw.nextRel = n + 1
		}
	}
	return dw
}

// relNum extracts the numeric suffix of an "rIdN" relationship id.
func relNum(id string) (int, bool) {
	if !strings.HasPrefix(id, "rId") {
		return 0, false
	}
	n, err := strconv.Atoi(id[3:])
	if err != nil {
		return 0, false
	}
	return n, true
}

// allocRel adds a fresh relationship and returns its id.
func (dw *docWriter) allocRel(relType, target string, external bool) string {
	id := fmt.Sprintf("rId%d", dw.nextRel)
	dw.nextRel++
	dw.rels[id] = Relationship{ID: id, Target: target, External: external, Type: relType}
	return id
}

// ensureRelOfType returns the id of an existing relationship of relType, or
// allocates one pointing at target (a part name relative to the package root).
func (dw *docWriter) ensureRelOfType(relType, target string) string {
	ids := make([]string, 0, len(dw.rels))
	for id, rel := range dw.rels {
		if rel.Type == relType {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		sort.Strings(ids)
		return ids[0]
	}
	return dw.allocRel(relType, target, false)
}

// hyperlinkRelID resolves a hyperlink to its relationship id: an explicit
// RelID must resolve through the rels (a dangling id is an invalid document);
// a Target-only link gets an allocated external rel, deduped per URL.
func (dw *docWriter) hyperlinkRelID(h *Hyperlink) (string, error) {
	if h.RelID != "" {
		if _, ok := dw.rels[h.RelID]; !ok {
			return "", fmt.Errorf("docx: %w: hyperlink references unknown relationship %q", ErrInvalidDocument, h.RelID)
		}
		return h.RelID, nil
	}
	if h.Target == "" {
		return "", nil // internal anchor link — no relationship
	}
	if id, ok := dw.linkRel[h.Target]; ok {
		return id, nil
	}
	id := dw.allocRel(relTypeHyperlink, h.Target, true)
	dw.linkRel[h.Target] = id
	return id, nil
}

// Relationship type URIs the writer knows.
const (
	relTypeStyles    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	relTypeNumbering = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"
	relTypeFootnotes = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes"
	relTypeEndnotes  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/endnotes"
	relTypeComments  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments"
	relTypeHyperlink = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
	relTypeImage     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
	relTypeHeader    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
	relTypeFooter    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer"
	relTypeCustomXML = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/customXml"
)

// assemble produces every package part in the fixed deterministic order.
func (dw *docWriter) assemble() ([]wpart, error) {
	doc := dw.doc

	// Structural relationships exist before the rels part is emitted.
	if doc.Styles != nil {
		dw.ensureRelOfType(relTypeStyles, "word/styles.xml")
	}
	if doc.Numbering != nil {
		dw.ensureRelOfType(relTypeNumbering, "word/numbering.xml")
	}
	if doc.Footnotes != nil {
		dw.ensureRelOfType(relTypeFootnotes, "word/footnotes.xml")
	}
	if doc.Endnotes != nil {
		dw.ensureRelOfType(relTypeEndnotes, "word/endnotes.xml")
	}
	if len(doc.Comments) > 0 {
		dw.ensureRelOfType(relTypeComments, "word/comments.xml")
	}
	// customXml parts need a relationship for Word to retain them.
	for _, name := range sortedKeys(doc.ExtraParts) {
		found := false
		for _, rel := range dw.rels {
			if rel.Type == relTypeCustomXML && rel.Target == name {
				found = true
				break
			}
		}
		if !found {
			dw.allocRel(relTypeCustomXML, name, false)
		}
	}
	headerParts := dw.headerFooterParts(doc.Headers, relTypeHeader, "header")
	footerParts := dw.headerFooterParts(doc.Footers, relTypeFooter, "footer")

	// The document body last-touches the rel set (hyperlink allocation), so it
	// is emitted before the rels part is rendered.
	body, err := dw.documentXML()
	if err != nil {
		return nil, err
	}

	parts := []wpart{
		{"[Content_Types].xml", []byte(dw.contentTypesXML(headerParts, footerParts))},
		{"_rels/.rels", []byte(rootRelsXML)},
		{"word/document.xml", body},
		{"word/_rels/document.xml.rels", []byte(dw.documentRelsXML())},
	}
	if doc.Styles != nil {
		parts = append(parts, wpart{"word/styles.xml", []byte(dw.stylesXML())})
	}
	if doc.Numbering != nil {
		parts = append(parts, wpart{"word/numbering.xml", []byte(dw.numberingXML())})
	}
	if doc.Footnotes != nil {
		p, err := dw.notesXML(doc.Footnotes, "footnotes", "footnote")
		if err != nil {
			return nil, err
		}
		parts = append(parts, wpart{"word/footnotes.xml", p})
	}
	if doc.Endnotes != nil {
		p, err := dw.notesXML(doc.Endnotes, "endnotes", "endnote")
		if err != nil {
			return nil, err
		}
		parts = append(parts, wpart{"word/endnotes.xml", p})
	}
	if len(doc.Comments) > 0 {
		p, err := dw.commentsXML()
		if err != nil {
			return nil, err
		}
		parts = append(parts, wpart{"word/comments.xml", p})
	}
	for _, hp := range headerParts {
		p, err := dw.hdrFtrXML(doc.Headers[hp.relID], "hdr")
		if err != nil {
			return nil, err
		}
		parts = append(parts, wpart{hp.name, p})
	}
	for _, fp := range footerParts {
		p, err := dw.hdrFtrXML(doc.Footers[fp.relID], "ftr")
		if err != nil {
			return nil, err
		}
		parts = append(parts, wpart{fp.name, p})
	}
	for _, name := range sortedKeys(doc.Media) {
		parts = append(parts, wpart{name, doc.Media[name]})
	}
	for _, name := range sortedKeys(doc.ExtraParts) {
		parts = append(parts, wpart{name, doc.ExtraParts[name]})
	}
	return parts, nil
}

// hfPart pairs a header/footer part name with the relationship id that
// references it.
type hfPart struct {
	name  string
	relID string
}

// headerFooterParts resolves the part name for every header/footer keyed by
// rel id, allocating relationships (and conventional part names) for entries a
// hand-built document added without rels.
func (dw *docWriter) headerFooterParts(m map[string]*HeaderFooter, relType, base string) []hfPart {
	var out []hfPart
	n := 1
	for _, id := range sortedKeys(m) {
		rel, ok := dw.rels[id]
		if !ok || rel.Type != relType {
			target := fmt.Sprintf("word/%s%d.xml", base, n)
			dw.rels[id] = Relationship{ID: id, Target: target, Type: relType}
			rel = dw.rels[id]
		}
		n++
		out = append(out, hfPart{name: rel.Target, relID: id})
	}
	return out
}

// sortedKeys returns a map's keys in sorted order (deterministic iteration).
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedIntKeys returns an int-keyed map's keys ascending.
func sortedIntKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>
`

// documentRelsXML renders the main part's relationships, ids sorted for
// determinism. Internal targets are relativized against word/ (the parser
// resolved them to package-absolute names).
func (dw *docWriter) documentRelsXML() string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + "\n")
	ids := make([]string, 0, len(dw.rels))
	for id := range dw.rels {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		ni, iok := relNum(ids[i])
		nj, jok := relNum(ids[j])
		if iok && jok {
			return ni < nj
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids {
		rel := dw.rels[id]
		target := rel.Target
		if !rel.External {
			target = relativeToWord(target)
		}
		sb.WriteString(`<Relationship Id="` + escXMLAttr.Replace(id) + `" Type="` + escXMLAttr.Replace(rel.Type) + `" Target="` + escXMLAttr.Replace(target) + `"`)
		if rel.External {
			sb.WriteString(` TargetMode="External"`)
		}
		sb.WriteString("/>\n")
	}
	sb.WriteString("</Relationships>\n")
	return sb.String()
}

// relativeToWord expresses a package-absolute part name relative to word/
// (the main part's directory), the form a rels part requires.
func relativeToWord(target string) string {
	if strings.HasPrefix(target, "word/") {
		return target[len("word/"):]
	}
	return "../" + path.Clean(target)
}

// contentTypesXML declares the part content types: defaults for rels/xml and
// each media extension, plus overrides for every WordprocessingML part.
func (dw *docWriter) contentTypesXML(headers, footers []hfPart) string {
	doc := dw.doc
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n" +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` + "\n" +
		`<Default Extension="xml" ContentType="application/xml"/>` + "\n")
	seen := map[string]bool{}
	for _, name := range sortedKeys(doc.Media) {
		ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
		if ext == "" || ext == "xml" || ext == "rels" || seen[ext] {
			continue
		}
		seen[ext] = true
		ct := "image/" + ext
		if ext == "jpg" {
			ct = "image/jpeg"
		}
		sb.WriteString(`<Default Extension="` + ext + `" ContentType="` + ct + `"/>` + "\n")
	}
	over := func(part, ct string) {
		sb.WriteString(`<Override PartName="/` + part + `" ContentType="` + ct + `"/>` + "\n")
	}
	const ctPrefix = "application/vnd.openxmlformats-officedocument.wordprocessingml."
	over("word/document.xml", ctPrefix+"document.main+xml")
	if doc.Styles != nil {
		over("word/styles.xml", ctPrefix+"styles+xml")
	}
	if doc.Numbering != nil {
		over("word/numbering.xml", ctPrefix+"numbering+xml")
	}
	if doc.Footnotes != nil {
		over("word/footnotes.xml", ctPrefix+"footnotes+xml")
	}
	if doc.Endnotes != nil {
		over("word/endnotes.xml", ctPrefix+"endnotes+xml")
	}
	if len(doc.Comments) > 0 {
		over("word/comments.xml", ctPrefix+"comments+xml")
	}
	for _, hp := range headers {
		over(hp.name, ctPrefix+"header+xml")
	}
	for _, fp := range footers {
		over(fp.name, ctPrefix+"footer+xml")
	}
	sb.WriteString("</Types>\n")
	return sb.String()
}

// AddImage embeds image bytes as a media part (word/media/<name>) and
// allocates an image relationship for it, returning the relationship id a
// Drawing references. An existing part of the same name is reused (the
// existing rel id of its target is returned) so repeated adds are idempotent.
// The caller owns choosing distinct names for distinct images.
func (d *Document) AddImage(name string, data []byte) string {
	part := "word/media/" + name
	if d.Media == nil {
		d.Media = map[string][]byte{}
	}
	if _, ok := d.Media[part]; ok {
		for _, id := range relKeysSorted(d.Rels) {
			rel := d.Rels[id]
			if rel.Type == relTypeImage && rel.Target == part {
				return id
			}
		}
	}
	d.Media[part] = data
	if d.Rels == nil {
		d.Rels = map[string]Relationship{}
	}
	next := 1
	for id := range d.Rels {
		if n, ok := relNum(id); ok && n >= next {
			next = n + 1
		}
	}
	id := fmt.Sprintf("rId%d", next)
	d.Rels[id] = Relationship{ID: id, Target: part, Type: relTypeImage}
	return id
}

// relKeysSorted returns relationship ids sorted (small helper for AddImage).
func relKeysSorted(m map[string]Relationship) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
