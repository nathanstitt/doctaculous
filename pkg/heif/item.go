package heif

import (
	"fmt"
	"slices"
)

// Item payload resolution and derived-item helpers.

// payload concatenates the item's iloc extents into its coded data.
// Construction method 0 resolves offsets against the whole file; method 1
// against the idat box payload. Method 2 (extents inside another item) and
// external data references are unsupported.
func (c *container) payload(it *item) ([]byte, error) {
	if it.dataRefIndex != 0 {
		return nil, fmt.Errorf("%w: item %d stored in external data reference", ErrUnsupported, it.id)
	}
	var src []byte
	switch it.constructionMethod {
	case 0:
		src = c.file
	case 1:
		src = c.idat
	default:
		return nil, fmt.Errorf("%w: iloc construction method %d", ErrUnsupported, it.constructionMethod)
	}
	if len(it.extents) == 0 {
		return nil, fmt.Errorf("%w: item %d has no location", ErrInvalid, it.id)
	}
	var total uint64
	for _, e := range it.extents {
		total += e.length
		if total > uint64(len(src)) {
			return nil, fmt.Errorf("%w: item %d extents exceed data size", ErrInvalid, it.id)
		}
	}
	if len(it.extents) == 1 {
		return c.extentBytes(src, it, it.extents[0])
	}
	out := make([]byte, 0, total)
	for _, e := range it.extents {
		b, err := c.extentBytes(src, it, e)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}

// extentBytes bounds-checks one extent against src and returns its slice.
// An extent length of 0 means "to the end of the data" per ISOBMFF.
func (c *container) extentBytes(src []byte, it *item, e locExtent) ([]byte, error) {
	start := it.baseOffset + e.offset
	if start < it.baseOffset { // overflow
		return nil, fmt.Errorf("%w: item %d extent offset overflow", ErrInvalid, it.id)
	}
	if start > uint64(len(src)) {
		return nil, fmt.Errorf("%w: item %d extent start %d beyond data", ErrInvalid, it.id, start)
	}
	length := e.length
	if length == 0 {
		length = uint64(len(src)) - start
	}
	end := start + length
	if end < start || end > uint64(len(src)) {
		return nil, fmt.Errorf("%w: item %d extent [%d,%d) beyond data", ErrInvalid, it.id, start, end)
	}
	return src[start:end], nil
}

// primary returns the primary (pitm) item after basic usability checks.
func (c *container) primary() (*item, error) {
	it, ok := c.items[c.primaryID]
	if !ok || it.itemType == "" {
		return nil, fmt.Errorf("%w: primary item %d not found", ErrInvalid, c.primaryID)
	}
	return it, nil
}

// checkDecodable rejects items this package must not attempt to decode.
func checkDecodable(it *item) error {
	if it.protected {
		return fmt.Errorf("%w: item %d is protected", ErrUnsupported, it.id)
	}
	if it.unknownEssential {
		return fmt.Errorf("%w: item %d carries an unrecognized essential property", ErrUnsupported, it.id)
	}
	switch it.itemType {
	case "hvc1", "grid":
		return nil
	case "av01":
		return fmt.Errorf("%w: AV1-coded item (AVIF)", ErrUnsupported)
	default:
		return fmt.Errorf("%w: item type %q", ErrUnsupported, it.itemType)
	}
}

// gridBody is the payload of a 'grid' derived item (ISO/IEC 23008-12 §6.6.2):
// a rows×cols matrix of tile items (the item's dimg references, row-major)
// cropped to output width×height.
type gridBody struct {
	rows, cols    int
	width, height uint32
}

// parseGridBody decodes the ImageGrid structure.
func parseGridBody(payload []byte) (*gridBody, error) {
	c := newCursor(payload, "grid")
	if v := c.u8(); v != 0 {
		if c.failed {
			return nil, c.err()
		}
		return nil, fmt.Errorf("%w: grid version %d", ErrUnsupported, v)
	}
	flags := c.u8()
	g := &gridBody{}
	g.rows = int(c.u8()) + 1
	g.cols = int(c.u8()) + 1
	if flags&1 != 0 {
		g.width = c.u32()
		g.height = c.u32()
	} else {
		g.width = uint32(c.u16())
		g.height = uint32(c.u16())
	}
	if err := c.err(); err != nil {
		return nil, err
	}
	return g, nil
}

// dimgSources returns the tile item IDs of a derived item in reference order.
func (it *item) dimgSources() []uint32 {
	for _, r := range it.refs {
		if r.typ == "dimg" {
			return r.toIDs
		}
	}
	return nil
}

// alphaAuxFor returns the alpha auxiliary item targeting the given item ID,
// if any. Both the HEVC-specific and the generic CICP alpha URNs are
// recognized.
func (c *container) alphaAuxFor(id uint32) *item {
	for _, it := range c.items {
		if !isAlphaAux(it.auxType) {
			continue
		}
		for _, r := range it.refs {
			if r.typ == "auxl" && slices.Contains(r.toIDs, id) {
				return it
			}
		}
	}
	return nil
}

// validateGrid checks a derived grid item's structure: a parsable ImageGrid
// body and one decodable hvc1 tile per rows×cols cell.
func (c *container) validateGrid(it *item) (*gridBody, []*item, error) {
	payload, err := c.payload(it)
	if err != nil {
		return nil, nil, err
	}
	g, err := parseGridBody(payload)
	if err != nil {
		return nil, nil, err
	}
	sources := it.dimgSources()
	if len(sources) != g.rows*g.cols {
		return nil, nil, fmt.Errorf("%w: grid %dx%d with %d tile references",
			ErrInvalid, g.cols, g.rows, len(sources))
	}
	tiles := make([]*item, 0, len(sources))
	for _, id := range sources {
		tile, ok := c.items[id]
		if !ok {
			return nil, nil, fmt.Errorf("%w: grid tile item %d not found", ErrInvalid, id)
		}
		if err := checkDecodable(tile); err != nil {
			return nil, nil, err
		}
		if tile.itemType != "hvc1" {
			return nil, nil, fmt.Errorf("%w: grid tile item type %q", ErrUnsupported, tile.itemType)
		}
		tiles = append(tiles, tile)
	}
	return g, tiles, nil
}

func isAlphaAux(auxType string) bool {
	return auxType == "urn:mpeg:hevc:2015:auxid:1" ||
		auxType == "urn:mpeg:mpegB:cicp:systems:auxiliary:alpha"
}
