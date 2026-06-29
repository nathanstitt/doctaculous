package font

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// decodeWOFF1 unwraps a WOFF (1.0) container to sfnt bytes. WOFF1 stores the sfnt
// table directory in a compact form and each table either zlib-compressed (when
// compLength < origLength) or stored raw; this rebuilds a standard sfnt via
// buildSFNT. Layout per the W3C WOFF File Format 1.0 spec.
func decodeWOFF1(data []byte) ([]byte, error) {
	// WOFFHeader is 44 bytes: signature(4) flavor(4) length(4) numTables(2)
	// reserved(2) totalSfntSize(4) majorVersion(2) minorVersion(2) metaOffset(4)
	// metaLength(4) metaOrigLength(4) privOffset(4) privLength(4).
	if len(data) < 44 {
		return nil, fmt.Errorf("%w: WOFF header truncated", ErrInvalidWOFF)
	}
	if binary.BigEndian.Uint32(data[0:]) != sigWOFF {
		return nil, fmt.Errorf("%w: bad WOFF signature", ErrInvalidWOFF)
	}
	flavor := binary.BigEndian.Uint32(data[4:])
	numTables := int(binary.BigEndian.Uint16(data[12:]))

	// Table directory: numTables entries of 20 bytes each, starting at offset 44.
	// Each entry: tag(4) offset(4) compLength(4) origLength(4) origChecksum(4).
	const dirStart = 44
	if len(data) < dirStart+20*numTables {
		return nil, fmt.Errorf("%w: WOFF table directory truncated", ErrInvalidWOFF)
	}
	tables := make([]sfntTable, numTables)
	for i := 0; i < numTables; i++ {
		e := dirStart + 20*i
		var t sfntTable
		copy(t.tag[:], data[e:e+4])
		off := binary.BigEndian.Uint32(data[e+4:])
		compLen := binary.BigEndian.Uint32(data[e+8:])
		origLen := binary.BigEndian.Uint32(data[e+12:])
		if int(off)+int(compLen) > len(data) {
			return nil, fmt.Errorf("%w: WOFF table %q out of range", ErrInvalidWOFF, t.tag)
		}
		raw := data[off : off+compLen]
		if compLen == origLen {
			t.data = append([]byte(nil), raw...) // stored uncompressed
		} else {
			zr, err := zlib.NewReader(bytes.NewReader(raw))
			if err != nil {
				return nil, fmt.Errorf("%w: WOFF table %q zlib: %v", ErrInvalidWOFF, t.tag, err)
			}
			out, err := io.ReadAll(io.LimitReader(zr, int64(origLen)+1))
			_ = zr.Close() // read-only inflater; nothing to flush
			if err != nil {
				return nil, fmt.Errorf("%w: WOFF table %q inflate: %v", ErrInvalidWOFF, t.tag, err)
			}
			if uint32(len(out)) != origLen {
				return nil, fmt.Errorf("%w: WOFF table %q length %d != declared %d", ErrInvalidWOFF, t.tag, len(out), origLen)
			}
			t.data = out
		}
		tables[i] = t
	}
	return buildSFNT(flavor, tables), nil
}
