package heif

import "fmt"

// Item properties (ISO/IEC 23008-12 §6.5). The ipco box holds a flat,
// 1-indexed list of property boxes; ipma associates properties with items and
// marks each association essential or not. A property we do not recognize is
// kept as its raw box so the essential rule can be enforced: an item carrying
// an unrecognized *essential* property must not be decoded.

// ispeProp is the mandatory image spatial extents property.
type ispeProp struct {
	width, height uint32
}

// clapProp is the clean-aperture crop, all values fractional per ISOBMFF.
// Offsets are signed.
type clapProp struct {
	widthN, widthD   uint32
	heightN, heightD uint32
	horizN           int32
	horizD           uint32
	vertN            int32
	vertD            uint32
}

// colrProp is the colour information property. For colourType "nclx" the CICP
// triplet and range flag are populated; for "rICC"/"prof" the raw ICC profile
// bytes are kept.
type colrProp struct {
	colourType string
	primaries  uint16
	transfer   uint16
	matrix     uint16
	fullRange  bool
	icc        []byte
}

// pixiProp reports bits per channel.
type pixiProp struct {
	bits []uint8
}

// prop is one entry of the ipco list. Exactly one of the typed fields is set
// for recognized types; raw always holds the payload.
type prop struct {
	typ  string
	raw  []byte
	ispe *ispeProp
	irot *uint8 // rotation, anti-clockwise: 0,1,2,3 => 0/90/180/270 degrees
	imir *uint8 // mirror axis: 0 = about a vertical axis, 1 = horizontal
	clap *clapProp
	colr *colrProp
	pixi *pixiProp
	auxC string // auxiliary type URN (auxC box)
	hvcC []byte // raw HEVCDecoderConfigurationRecord (parsed by pkg/heif/hevc)
}

// recognized reports whether the property type is one this decoder
// understands (used for the essential-property rule).
func (p *prop) recognized() bool {
	switch p.typ {
	case "ispe", "irot", "imir", "clap", "colr", "pixi", "auxC", "hvcC", "pasp", "rloc", "lsel":
		// pasp (pixel aspect), rloc (relative location) and lsel (layer
		// selector) are recognized-and-ignored: they never change how a
		// still primary image decodes here.
		return true
	}
	return false
}

// parseProp decodes a single ipco child box.
func parseProp(b boxSpan) (*prop, error) {
	p := &prop{typ: b.typ, raw: b.payload}
	switch b.typ {
	case "ispe":
		_, _, rest, err := fullBox(b.payload)
		if err != nil {
			return nil, err
		}
		c := newCursor(rest, "ispe")
		p.ispe = &ispeProp{width: c.u32(), height: c.u32()}
		if err := c.err(); err != nil {
			return nil, err
		}
	case "irot":
		c := newCursor(b.payload, "irot")
		v := c.u8() & 3
		if err := c.err(); err != nil {
			return nil, err
		}
		p.irot = &v
	case "imir":
		c := newCursor(b.payload, "imir")
		v := c.u8() & 1
		if err := c.err(); err != nil {
			return nil, err
		}
		p.imir = &v
	case "clap":
		c := newCursor(b.payload, "clap")
		cl := &clapProp{
			widthN: c.u32(), widthD: c.u32(),
			heightN: c.u32(), heightD: c.u32(),
		}
		cl.horizN = int32(c.u32())
		cl.horizD = c.u32()
		cl.vertN = int32(c.u32())
		cl.vertD = c.u32()
		if err := c.err(); err != nil {
			return nil, err
		}
		p.clap = cl
	case "colr":
		c := newCursor(b.payload, "colr")
		ct := c.fourcc()
		col := &colrProp{colourType: ct}
		switch ct {
		case "nclx":
			col.primaries = c.u16()
			col.transfer = c.u16()
			col.matrix = c.u16()
			col.fullRange = c.u8()>>7 == 1
		case "rICC", "prof":
			col.icc = c.take(c.remaining())
		}
		if err := c.err(); err != nil {
			return nil, err
		}
		p.colr = col
	case "pixi":
		_, _, rest, err := fullBox(b.payload)
		if err != nil {
			return nil, err
		}
		c := newCursor(rest, "pixi")
		n := int(c.u8())
		px := &pixiProp{}
		for range n {
			px.bits = append(px.bits, c.u8())
		}
		if err := c.err(); err != nil {
			return nil, err
		}
		p.pixi = px
	case "auxC":
		_, _, rest, err := fullBox(b.payload)
		if err != nil {
			return nil, err
		}
		c := newCursor(rest, "auxC")
		p.auxC = c.cstring()
		if err := c.err(); err != nil {
			return nil, err
		}
	case "hvcC":
		p.hvcC = b.payload
	}
	return p, nil
}

// propAssoc is one ipma association: a 1-based index into the ipco list plus
// the essential bit.
type propAssoc struct {
	index     uint16
	essential bool
}

// parseIprp parses the item-properties box: the ipco property list (in order)
// and the per-item association lists.
func parseIprp(payload []byte) (props []*prop, assocs map[uint32][]propAssoc, err error) {
	boxes, err := scanBoxes(payload)
	if err != nil {
		return nil, nil, err
	}
	ipco, ok := findBox(boxes, "ipco")
	if !ok {
		return nil, nil, fmt.Errorf("%w: iprp without ipco", ErrInvalid)
	}
	children, err := scanBoxes(ipco.payload)
	if err != nil {
		return nil, nil, err
	}
	if len(children) > maxProperties {
		return nil, nil, fmt.Errorf("%w: %d properties exceeds limit", ErrInvalid, len(children))
	}
	for _, ch := range children {
		p, err := parseProp(ch)
		if err != nil {
			return nil, nil, err
		}
		props = append(props, p)
	}

	assocs = make(map[uint32][]propAssoc)
	for _, b := range boxes {
		if b.typ != "ipma" {
			continue
		}
		version, flags, rest, err := fullBox(b.payload)
		if err != nil {
			return nil, nil, err
		}
		c := newCursor(rest, "ipma")
		entryCount := c.u32()
		if entryCount > maxItems {
			return nil, nil, fmt.Errorf("%w: ipma entry count %d exceeds limit", ErrInvalid, entryCount)
		}
		for i := uint32(0); i < entryCount && !c.failed; i++ {
			var itemID uint32
			if version < 1 {
				itemID = uint32(c.u16())
			} else {
				itemID = c.u32()
			}
			n := int(c.u8())
			list := make([]propAssoc, 0, n)
			for range n {
				var idx uint16
				var essential bool
				if flags&1 != 0 {
					v := c.u16()
					essential = v&0x8000 != 0
					idx = v & 0x7fff
				} else {
					v := c.u8()
					essential = v&0x80 != 0
					idx = uint16(v & 0x7f)
				}
				list = append(list, propAssoc{index: idx, essential: essential})
			}
			assocs[itemID] = list
		}
		if err := c.err(); err != nil {
			return nil, nil, err
		}
	}
	return props, assocs, nil
}
