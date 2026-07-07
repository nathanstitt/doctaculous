package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// Footnotes is the parsed word/footnotes.xml: note id -> block content.
type Footnotes struct {
	byID map[int]*HeaderFooter // reuse HeaderFooter's Blocks container
}

// Note returns a footnote's content by id.
func (f *Footnotes) Note(id int) (*HeaderFooter, bool) {
	if f == nil {
		return nil, false
	}
	n, ok := f.byID[id]
	return n, ok
}

// parseFootnotes parses a word/footnotes.xml part. Separator/continuation notes
// (negative or special ids) are parsed like any other; the lowering ignores ids
// it has no reference for.
func parseFootnotes(data []byte) (*Footnotes, error) {
	f := &Footnotes{byID: map[int]*HeaderFooter{}}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: footnotes: %v", ErrMalformedXML, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != wNS || se.Name.Local != "footnote" {
			continue
		}
		id, _ := wAttrInt(se, "id")
		note := &HeaderFooter{}
		if err := fillBlocksUntil(dec, "footnote", &note.Blocks); err != nil {
			return nil, err
		}
		f.byID[id] = note
	}
	return f, nil
}
