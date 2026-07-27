package heif

import (
	"fmt"
	"slices"
)

// Parsing of the top-level structure: ftyp brand check and the meta box tree
// (hdlr/pitm/iinf/iloc/idat/iref/iprp) into a container of resolved items.

// Hard caps applied before any allocation sized from file-declared counts.
// A malformed header must not be able to force large allocations ("never
// panic" includes "never OOM on header lies").
const (
	maxItems      = 10000 // items per file (iPhone grids use ~50)
	maxExtents    = 4096  // iloc extents per item
	maxProperties = 10000 // ipco entries
	maxRefs       = 10000 // iref references per item

	// maxDim / maxPixels bound the decoded output declared by ispe/grid
	// headers. 32768 per axis and 2^28 total samples comfortably cover
	// real cameras and panoramas while keeping a lying header from
	// demanding gigabytes.
	maxDim    = 32768
	maxPixels = 1 << 28
)

// itemRef is one iref edge: this item references toIDs with the given type
// (e.g. "dimg" grid sources, "auxl" auxiliary-of).
type itemRef struct {
	typ   string
	toIDs []uint32
}

// locExtent is one iloc extent resolved lazily against the file (or idat)
// buffer.
type locExtent struct {
	offset uint64
	length uint64
}

// item is one HEIF item with its location, properties, and references.
type item struct {
	id       uint32
	itemType string // "hvc1", "grid", "Exif", "mime", ...
	hidden   bool

	// location
	constructionMethod uint8 // 0=file offsets, 1=idat, 2=item (unsupported)
	dataRefIndex       uint16
	baseOffset         uint64
	extents            []locExtent

	// properties (resolved from ipco/ipma)
	ispe             *ispeProp
	irot             uint8
	imir             *uint8
	clap             *clapProp
	colr             *colrProp
	pixi             *pixiProp
	auxType          string
	hvcC             []byte
	unknownEssential bool
	protected        bool

	refs []itemRef
}

// container is a parsed HEIF file: brands, the primary item, and the item
// table. The full file buffer is retained because iloc construction 0 extents
// are absolute file offsets.
type container struct {
	file       []byte
	majorBrand string
	compat     []string
	primaryID  uint32
	items      map[uint32]*item
	idat       []byte
}

// heifBrands are the ftyp brands (major or compatible) that identify
// HEVC-coded still HEIF. mif1 is the structural brand shared with other
// codecs (e.g. AVIF), so it is accepted only when the primary item turns out
// to be HEVC-coded.
var heifBrands = map[string]bool{
	"heic": true, "heix": true, "heim": true, "heis": true,
	"hevc": true, "hevx": true, "hevm": true, "hevs": true,
}

// parseContainer parses the top-level boxes of a HEIF file.
func parseContainer(data []byte) (*container, error) {
	boxes, err := scanBoxes(data)
	if err != nil {
		return nil, err
	}
	ftyp, ok := findBox(boxes, "ftyp")
	if !ok {
		return nil, fmt.Errorf("%w: missing ftyp box", ErrInvalid)
	}
	c := &container{file: data, items: make(map[uint32]*item)}
	fc := newCursor(ftyp.payload, "ftyp")
	c.majorBrand = fc.fourcc()
	fc.u32() // minor version
	for fc.remaining() >= 4 {
		c.compat = append(c.compat, fc.fourcc())
	}
	if err := fc.err(); err != nil {
		return nil, err
	}
	if !c.brandSupported() {
		if c.majorBrand == "msf1" || c.brandSeen("msf1") {
			return nil, fmt.Errorf("%w: HEIF image sequence (msf1)", ErrUnsupported)
		}
		return nil, fmt.Errorf("%w: ftyp brand %q is not a HEVC-coded still HEIF", ErrUnsupported, c.majorBrand)
	}

	meta, ok := findBox(boxes, "meta")
	if !ok {
		return nil, fmt.Errorf("%w: missing meta box", ErrInvalid)
	}
	if err := c.parseMeta(meta.payload); err != nil {
		return nil, err
	}
	return c, nil
}

// brandSeen reports whether b is the major brand or in the compatible list.
func (c *container) brandSeen(b string) bool {
	return c.majorBrand == b || slices.Contains(c.compat, b)
}

// brandSupported reports whether the ftyp identifies content this package can
// hold: an heic-family brand anywhere, or the generic mif1 structural brand
// (whose coded type is checked per item at decode time).
func (c *container) brandSupported() bool {
	if heifBrands[c.majorBrand] || c.majorBrand == "mif1" {
		return true
	}
	for _, cb := range c.compat {
		if heifBrands[cb] || cb == "mif1" {
			return true
		}
	}
	return false
}

// parseMeta parses the meta FullBox payload and populates the item table.
func (c *container) parseMeta(payload []byte) error {
	_, _, rest, err := fullBox(payload)
	if err != nil {
		return err
	}
	boxes, err := scanBoxes(rest)
	if err != nil {
		return err
	}
	if hdlr, ok := findBox(boxes, "hdlr"); ok {
		_, _, hrest, err := fullBox(hdlr.payload)
		if err != nil {
			return err
		}
		hc := newCursor(hrest, "hdlr")
		hc.u32() // pre_defined
		if ht := hc.fourcc(); ht != "pict" {
			return fmt.Errorf("%w: meta handler %q is not a still image", ErrUnsupported, ht)
		}
	}

	pitm, ok := findBox(boxes, "pitm")
	if !ok {
		return fmt.Errorf("%w: missing pitm (primary item)", ErrInvalid)
	}
	pv, _, prest, err := fullBox(pitm.payload)
	if err != nil {
		return err
	}
	pc := newCursor(prest, "pitm")
	if pv == 0 {
		c.primaryID = uint32(pc.u16())
	} else {
		c.primaryID = pc.u32()
	}
	if err := pc.err(); err != nil {
		return err
	}

	if idat, ok := findBox(boxes, "idat"); ok {
		c.idat = idat.payload
	}
	if iinf, ok := findBox(boxes, "iinf"); ok {
		if err := c.parseIinf(iinf.payload); err != nil {
			return err
		}
	}
	if iloc, ok := findBox(boxes, "iloc"); ok {
		if err := c.parseIloc(iloc.payload); err != nil {
			return err
		}
	}
	if iref, ok := findBox(boxes, "iref"); ok {
		if err := c.parseIref(iref.payload); err != nil {
			return err
		}
	}
	if iprp, ok := findBox(boxes, "iprp"); ok {
		props, assocs, err := parseIprp(iprp.payload)
		if err != nil {
			return err
		}
		c.applyProps(props, assocs)
	}
	return nil
}

// itemByID returns the item, creating it if iinf has not introduced it yet
// (box order within meta is not guaranteed).
func (c *container) itemByID(id uint32) (*item, error) {
	if it, ok := c.items[id]; ok {
		return it, nil
	}
	if len(c.items) >= maxItems {
		return nil, fmt.Errorf("%w: more than %d items", ErrInvalid, maxItems)
	}
	it := &item{id: id}
	c.items[id] = it
	return it, nil
}

// parseIinf parses the item-information box: one infe per item with its type.
func (c *container) parseIinf(payload []byte) error {
	version, _, rest, err := fullBox(payload)
	if err != nil {
		return err
	}
	cur := newCursor(rest, "iinf")
	var count uint32
	if version == 0 {
		count = uint32(cur.u16())
	} else {
		count = cur.u32()
	}
	if count > maxItems {
		return fmt.Errorf("%w: iinf declares %d items", ErrInvalid, count)
	}
	if err := cur.err(); err != nil {
		return err
	}
	boxes, err := scanBoxes(rest[cur.off:])
	if err != nil {
		return err
	}
	for _, b := range boxes {
		if b.typ != "infe" {
			continue
		}
		iv, iflags, irest, err := fullBox(b.payload)
		if err != nil {
			return err
		}
		if iv < 2 {
			return fmt.Errorf("%w: infe version %d", ErrUnsupported, iv)
		}
		ic := newCursor(irest, "infe")
		var id uint32
		if iv == 2 {
			id = uint32(ic.u16())
		} else {
			id = ic.u32()
		}
		protection := ic.u16()
		itemType := ic.fourcc()
		if err := ic.err(); err != nil {
			return err
		}
		it, err := c.itemByID(id)
		if err != nil {
			return err
		}
		it.itemType = itemType
		it.hidden = iflags&1 != 0
		it.protected = protection != 0
	}
	return nil
}

// parseIloc parses the item-location box (versions 0, 1, 2).
func (c *container) parseIloc(payload []byte) error {
	version, _, rest, err := fullBox(payload)
	if err != nil {
		return err
	}
	if version > 2 {
		return fmt.Errorf("%w: iloc version %d", ErrUnsupported, version)
	}
	cur := newCursor(rest, "iloc")
	sizes := cur.u16()
	offsetSize := int(sizes >> 12 & 0xf)
	lengthSize := int(sizes >> 8 & 0xf)
	baseOffsetSize := int(sizes >> 4 & 0xf)
	indexSize := 0
	if version >= 1 {
		indexSize = int(sizes & 0xf)
	}
	for _, s := range []int{offsetSize, lengthSize, baseOffsetSize, indexSize} {
		if s != 0 && s != 4 && s != 8 {
			return fmt.Errorf("%w: iloc field size %d", ErrInvalid, s)
		}
	}
	var count uint32
	if version < 2 {
		count = uint32(cur.u16())
	} else {
		count = cur.u32()
	}
	if count > maxItems {
		return fmt.Errorf("%w: iloc declares %d items", ErrInvalid, count)
	}
	for i := uint32(0); i < count && !cur.failed; i++ {
		var id uint32
		if version < 2 {
			id = uint32(cur.u16())
		} else {
			id = cur.u32()
		}
		var method uint8
		if version >= 1 {
			method = uint8(cur.u16() & 0xf)
		}
		dataRef := cur.u16()
		base := cur.uint(baseOffsetSize)
		extentCount := int(cur.u16())
		if extentCount > maxExtents {
			return fmt.Errorf("%w: item %d has %d extents", ErrInvalid, id, extentCount)
		}
		extents := make([]locExtent, 0, extentCount)
		for range extentCount {
			cur.uint(indexSize) // extent_index: only meaningful for method 2
			off := cur.uint(offsetSize)
			length := cur.uint(lengthSize)
			extents = append(extents, locExtent{offset: off, length: length})
		}
		if cur.failed {
			break
		}
		it, err := c.itemByID(id)
		if err != nil {
			return err
		}
		it.constructionMethod = method
		it.dataRefIndex = dataRef
		it.baseOffset = base
		it.extents = extents
	}
	return cur.err()
}

// parseIref parses the item-reference box; each child box's type is the
// reference type.
func (c *container) parseIref(payload []byte) error {
	version, _, rest, err := fullBox(payload)
	if err != nil {
		return err
	}
	boxes, err := scanBoxes(rest)
	if err != nil {
		return err
	}
	for _, b := range boxes {
		cur := newCursor(b.payload, "iref")
		var from uint32
		if version == 0 {
			from = uint32(cur.u16())
		} else {
			from = cur.u32()
		}
		n := int(cur.u16())
		if n > maxRefs {
			return fmt.Errorf("%w: iref with %d references", ErrInvalid, n)
		}
		to := make([]uint32, 0, n)
		for range n {
			if version == 0 {
				to = append(to, uint32(cur.u16()))
			} else {
				to = append(to, cur.u32())
			}
		}
		if err := cur.err(); err != nil {
			return err
		}
		it, err := c.itemByID(from)
		if err != nil {
			return err
		}
		it.refs = append(it.refs, itemRef{typ: b.typ, toIDs: to})
	}
	return nil
}

// applyProps attaches ipco properties to items per the ipma associations.
// Index 0 means "no property"; out-of-range indexes make the item unusable
// rather than failing the whole file.
func (c *container) applyProps(props []*prop, assocs map[uint32][]propAssoc) {
	for id, list := range assocs {
		it, err := c.itemByID(id)
		if err != nil {
			return // item table full; parse already capped
		}
		for _, a := range list {
			if a.index == 0 {
				continue
			}
			if int(a.index) > len(props) {
				it.unknownEssential = it.unknownEssential || a.essential
				continue
			}
			p := props[a.index-1]
			if !p.recognized() && a.essential {
				it.unknownEssential = true
			}
			switch {
			case p.ispe != nil:
				it.ispe = p.ispe
			case p.irot != nil:
				it.irot = *p.irot
			case p.imir != nil:
				it.imir = p.imir
			case p.clap != nil:
				it.clap = p.clap
			case p.colr != nil:
				it.colr = p.colr
			case p.pixi != nil:
				it.pixi = p.pixi
			case p.auxC != "":
				it.auxType = p.auxC
			case p.hvcC != nil:
				it.hvcC = p.hvcC
			}
		}
	}
}
