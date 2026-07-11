package docx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// Notes is a parsed notes part — word/footnotes.xml or word/endnotes.xml, which
// share their grammar — mapping a note id to its block content.
type Notes struct {
	// ByID maps a note id to its blocks. Separator/continuation notes (the
	// special ids Word reserves, typically <= 0) are included; consumers ignore
	// ids they hold no reference for.
	ByID map[int][]Block
}

// NewNotes returns an empty Notes container, for callers constructing a
// document for the writer.
func NewNotes() *Notes { return &Notes{ByID: map[int][]Block{}} }

// Add sets a note's content by id.
func (n *Notes) Add(id int, blocks []Block) { n.ByID[id] = blocks }

// Note returns a note's content by id.
func (n *Notes) Note(id int) ([]Block, bool) {
	if n == nil {
		return nil, false
	}
	blocks, ok := n.ByID[id]
	return blocks, ok
}

// parseNotes parses a footnotes/endnotes part; element is the per-note element
// local name ("footnote" or "endnote").
func parseNotes(data []byte, element string) (*Notes, error) {
	n := NewNotes()
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: %ss: %v", ErrMalformedXML, element, err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != wNS || se.Name.Local != element {
			continue
		}
		id, _ := wAttrInt(se, "id")
		var blocks []Block
		if err := fillBlocksUntil(dec, element, &blocks); err != nil {
			return nil, err
		}
		n.ByID[id] = blocks
	}
	return n, nil
}
