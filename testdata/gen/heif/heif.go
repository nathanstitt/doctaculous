// Package heif builds deterministic HEIF containers in memory for tests,
// mirroring the other format generators. It assembles the ISOBMFF box
// structure (ftyp/meta/mdat) around caller-supplied item payloads; it does
// not encode HEVC — codec bitstreams used by tests are committed payloads
// (see testdata/gen/heif payloads README once codec milestones land) or
// opaque placeholder bytes for container-only tests.
package heif

import "encoding/binary"

// Prop is one item property to place in ipco and associate via ipma.
type Prop struct {
	Type      string
	Payload   []byte
	Essential bool
}

// Ref is one item reference (iref edge), e.g. {"dimg", tileIDs} on a grid
// item or {"auxl", [primary]} on an alpha item.
type Ref struct {
	Type string
	To   []uint32
}

// Item is one HEIF item.
type Item struct {
	ID        uint32
	Type      string   // 4CC: "hvc1", "grid", ...
	Data      []byte   // coded payload; stored in mdat (or idat when InIdat)
	Parts     [][]byte // optional: multi-extent payload; overrides Data
	InIdat    bool
	Props     []Prop
	Refs      []Ref
	Protected bool // non-zero protection index in infe
	NoLoc     bool // omit the iloc entry entirely
	Hidden    bool
}

// parts returns the item payload as its extent list.
func (it *Item) parts() [][]byte {
	if it.Parts != nil {
		return it.Parts
	}
	return [][]byte{it.Data}
}

// File describes a HEIF file to build.
type File struct {
	Major   string // default "heic"
	Compat  []string
	Primary uint32
	Items   []Item
}

// Build assembles the file: ftyp, meta (hdlr/pitm/iinf/iloc/iref/iprp/idat),
// mdat. iloc uses version 1 with 4-byte offset/length fields and absolute
// file offsets (construction method 0) or idat-relative offsets (method 1).
func (f *File) Build() []byte {
	major := f.Major
	if major == "" {
		major = "heic"
	}
	compat := f.Compat
	if compat == nil {
		compat = []string{major, "mif1"}
	}
	var ftypParts [][]byte
	ftypParts = append(ftypParts, []byte(major), u32(0))
	for _, c := range compat {
		ftypParts = append(ftypParts, []byte(c))
	}
	ftyp := Box("ftyp", ftypParts...)

	// Two passes: the meta box's size does not depend on the offset
	// *values* (fixed 4-byte fields), so build once with zero offsets to
	// learn the mdat data start, then rebuild with real offsets.
	meta := f.buildMeta(0)
	mdatDataStart := len(ftyp) + len(meta) + 8 // 8 = mdat box header
	meta = f.buildMeta(mdatDataStart)

	var mdat []byte
	for _, it := range f.Items {
		if !it.InIdat && !it.NoLoc {
			for _, p := range it.parts() {
				mdat = append(mdat, p...)
			}
		}
	}
	out := append([]byte{}, ftyp...)
	out = append(out, meta...)
	out = append(out, Box("mdat", mdat)...)
	return out
}

func (f *File) buildMeta(mdatDataStart int) []byte {
	hdlr := FullBox("hdlr", 0, 0, u32(0), []byte("pict"), make([]byte, 12))
	pitm := FullBox("pitm", 0, 0, u16(uint16(f.Primary)))

	// iinf
	var infes [][]byte
	for _, it := range f.Items {
		protection := uint16(0)
		if it.Protected {
			protection = 1
		}
		var flags uint32
		if it.Hidden {
			flags = 1
		}
		infes = append(infes, FullBox("infe", 2, flags,
			u16(uint16(it.ID)), u16(protection), []byte(it.Type), []byte{0}))
	}
	iinf := FullBox("iinf", 0, 0, append([][]byte{u16(uint16(len(infes)))}, infes...)...)

	// idat + iloc
	var idat []byte
	type extent struct{ offset, length uint32 }
	type loc struct {
		id      uint16
		method  uint16
		extents []extent
	}
	var locs []loc
	mdatOff := uint32(mdatDataStart)
	for _, it := range f.Items {
		if it.NoLoc {
			continue
		}
		l := loc{id: uint16(it.ID)}
		for _, p := range it.parts() {
			if it.InIdat {
				l.method = 1
				l.extents = append(l.extents, extent{uint32(len(idat)), uint32(len(p))})
				idat = append(idat, p...)
			} else {
				l.extents = append(l.extents, extent{mdatOff, uint32(len(p))})
				mdatOff += uint32(len(p))
			}
		}
		locs = append(locs, l)
	}
	var ilocBody [][]byte
	ilocBody = append(ilocBody,
		u16(0x4400), // offset_size=4, length_size=4, base_offset_size=0, index_size=0
		u16(uint16(len(locs))))
	for _, l := range locs {
		ilocBody = append(ilocBody,
			u16(l.id), u16(l.method), u16(0), // ID, construction method, data_reference_index
			u16(uint16(len(l.extents))))
		for _, e := range l.extents {
			ilocBody = append(ilocBody, u32(e.offset), u32(e.length))
		}
	}
	iloc := FullBox("iloc", 1, 0, ilocBody...)

	// iref
	var refBoxes [][]byte
	for _, it := range f.Items {
		for _, r := range it.Refs {
			parts := [][]byte{u16(uint16(it.ID)), u16(uint16(len(r.To)))}
			for _, to := range r.To {
				parts = append(parts, u16(uint16(to)))
			}
			refBoxes = append(refBoxes, Box(r.Type, parts...))
		}
	}
	var iref []byte
	if len(refBoxes) > 0 {
		iref = FullBox("iref", 0, 0, refBoxes...)
	}

	// iprp: ipco entries in item order, ipma associating them.
	var ipcoChildren [][]byte
	var ipmaEntries [][]byte
	propIndex := uint16(0)
	for _, it := range f.Items {
		if len(it.Props) == 0 {
			continue
		}
		entry := [][]byte{u16(uint16(it.ID)), {byte(len(it.Props))}}
		for _, p := range it.Props {
			ipcoChildren = append(ipcoChildren, Box(p.Type, p.Payload))
			propIndex++
			assoc := propIndex & 0x7fff
			if p.Essential {
				assoc |= 0x8000
			}
			entry = append(entry, u16(assoc))
		}
		ipmaEntries = append(ipmaEntries, flatten(entry))
	}
	ipco := Box("ipco", ipcoChildren...)
	ipma := FullBox("ipma", 0, 1, // flags&1: 15-bit property indexes
		append([][]byte{u32(uint32(len(ipmaEntries)))}, ipmaEntries...)...)
	iprp := Box("iprp", ipco, ipma)

	parts := [][]byte{hdlr, pitm, iinf, iloc}
	if iref != nil {
		parts = append(parts, iref)
	}
	parts = append(parts, iprp)
	if len(idat) > 0 {
		parts = append(parts, Box("idat", idat))
	}
	return FullBox("meta", 0, 0, parts...)
}

// Box assembles an ISOBMFF box with a 32-bit size header.
func Box(typ string, parts ...[]byte) []byte {
	body := flatten(parts)
	out := make([]byte, 0, 8+len(body))
	out = append(out, u32(uint32(8+len(body)))...)
	out = append(out, typ...)
	return append(out, body...)
}

// FullBox assembles a box with a version/flags word.
func FullBox(typ string, version byte, flags uint32, parts ...[]byte) []byte {
	vf := u32(uint32(version)<<24 | flags&0x00ffffff)
	return Box(typ, append([][]byte{vf}, parts...)...)
}

// Ispe builds an image spatial extents property payload.
func Ispe(w, h uint32) Prop {
	return Prop{Type: "ispe", Payload: flatten([][]byte{u32(0), u32(w), u32(h)})}
}

// Irot builds a rotation property (anti-clockwise quarter turns 0-3).
func Irot(angle uint8) Prop { return Prop{Type: "irot", Payload: []byte{angle & 3}} }

// Imir builds a mirror property (axis 0 = vertical, 1 = horizontal).
func Imir(axis uint8) Prop { return Prop{Type: "imir", Payload: []byte{axis & 1}} }

// Clap builds a clean-aperture property from whole-number values.
func Clap(w, h uint32, hoff, voff int32) Prop {
	return Prop{Type: "clap", Payload: flatten([][]byte{
		u32(w), u32(1), u32(h), u32(1),
		u32(uint32(hoff)), u32(1), u32(uint32(voff)), u32(1),
	})}
}

// Pixi builds a bits-per-channel property.
func Pixi(bits ...uint8) Prop {
	return Prop{Type: "pixi", Payload: flatten([][]byte{u32(0), {byte(len(bits))}, bits})}
}

// AuxC builds an auxiliary-type property.
func AuxC(urn string) Prop {
	return Prop{Type: "auxC", Payload: flatten([][]byte{u32(0), []byte(urn), {0}})}
}

// ColrNCLX builds an nclx colour property.
func ColrNCLX(primaries, transfer, matrix uint16, fullRange bool) Prop {
	fr := byte(0)
	if fullRange {
		fr = 0x80
	}
	return Prop{Type: "colr", Payload: flatten([][]byte{
		[]byte("nclx"), u16(primaries), u16(transfer), u16(matrix), {fr},
	})}
}

// HvcC wraps raw HEVCDecoderConfigurationRecord bytes; container-only tests
// pass placeholder bytes.
func HvcC(raw []byte) Prop { return Prop{Type: "hvcC", Payload: raw, Essential: true} }

// GridPayload builds an ImageGrid derived-item body.
func GridPayload(rows, cols int, w, h uint32) []byte {
	if w > 0xffff || h > 0xffff {
		return flatten([][]byte{
			{0, 1, byte(rows - 1), byte(cols - 1)}, u32(w), u32(h),
		})
	}
	return flatten([][]byte{
		{0, 0, byte(rows - 1), byte(cols - 1)}, u16(uint16(w)), u16(uint16(h)),
	})
}

// HvcCFromAnnexB builds an HEVCDecoderConfigurationRecord (4-byte NAL
// lengths) plus the matching length-prefixed hvc1 item payload from an
// Annex-B intra stream, so tests can wrap real encoder output in containers.
func HvcCFromAnnexB(stream []byte) (hvcc, payload []byte) {
	var vps, sps, pps, slices [][]byte
	for _, nal := range splitAnnexB(stream) {
		if len(nal) < 2 {
			continue
		}
		switch nal[0] >> 1 & 0x3f {
		case 32:
			vps = append(vps, nal)
		case 33:
			sps = append(sps, nal)
		case 34:
			pps = append(pps, nal)
		default:
			if nal[0]>>1&0x3f < 32 {
				slices = append(slices, nal)
			}
		}
	}
	hvcc = append(hvcc, 1)                                     // configurationVersion
	hvcc = append(hvcc, make([]byte, 12)...)                   // profile/compat/constraints/level
	hvcc = append(hvcc, 0xf0, 0, 0xfc, 0xfd, 0xf8, 0xf8, 0, 0) // reserved fields + avgFrameRate
	hvcc = append(hvcc, 0x03|0x0c)                             // lengthSizeMinusOne=3
	arrays := [][2]any{{byte(32), vps}, {byte(33), sps}, {byte(34), pps}}
	hvcc = append(hvcc, 3)
	for _, a := range arrays {
		hvcc = append(hvcc, a[0].(byte)|0x80)
		nals := a[1].([][]byte)
		hvcc = append(hvcc, u16(uint16(len(nals)))...)
		for _, n := range nals {
			hvcc = append(hvcc, u16(uint16(len(n)))...)
			hvcc = append(hvcc, n...)
		}
	}
	for _, n := range slices {
		payload = append(payload, u32(uint32(len(n)))...)
		payload = append(payload, n...)
	}
	return hvcc, payload
}

// splitAnnexB splits a start-code-delimited stream (00 00 01 / 00 00 00 01).
func splitAnnexB(data []byte) [][]byte {
	var out [][]byte
	start := -1
	for i := 0; i+2 < len(data); i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if start >= 0 {
				end := i
				for end > start && data[end-1] == 0 {
					end--
				}
				out = append(out, data[start:end])
			}
			start = i + 3
			i += 2
		}
	}
	if start >= 0 && start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

func flatten(parts [][]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func u16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
