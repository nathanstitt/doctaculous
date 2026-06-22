package filter

import "fmt"

// asciiHexDecode decodes ASCIIHexDecode data: hex digits, whitespace ignored,
// terminated by '>'. An odd final digit is treated as if followed by '0'.
func asciiHexDecode(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data)/2)
	var hi byte
	haveHi := false
	for _, c := range data {
		if c == '>' {
			break
		}
		if isASCIISpace(c) {
			continue
		}
		v, ok := hexVal(c)
		if !ok {
			return nil, fmt.Errorf("asciihex: bad digit %q", c)
		}
		if !haveHi {
			hi = v
			haveHi = true
		} else {
			out = append(out, hi<<4|v)
			haveHi = false
		}
	}
	if haveHi {
		out = append(out, hi<<4)
	}
	return out, nil
}

// ascii85Decode decodes ASCII85Decode data (Adobe variant). Whitespace is
// ignored, 'z' expands to four zero bytes, and "~>" terminates the data.
func ascii85Decode(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data)*4/5)
	var group [5]byte
	n := 0
	for _, c := range data {
		if c == '~' { // end marker "~>"
			break
		}
		if isASCIISpace(c) {
			continue
		}
		if c == 'z' && n == 0 {
			out = append(out, 0, 0, 0, 0)
			continue
		}
		if c < '!' || c > 'u' {
			return nil, fmt.Errorf("ascii85: byte out of range %q", c)
		}
		group[n] = c - '!'
		n++
		if n == 5 {
			var val uint32
			for _, g := range group {
				val = val*85 + uint32(g)
			}
			out = append(out, byte(val>>24), byte(val>>16), byte(val>>8), byte(val))
			n = 0
		}
	}
	if n == 1 {
		return nil, fmt.Errorf("ascii85: invalid final group of length 1")
	}
	if n > 0 {
		// Pad the final partial group with the max value 'u'-'!' (84).
		for i := n; i < 5; i++ {
			group[i] = 84
		}
		var val uint32
		for _, g := range group {
			val = val*85 + uint32(g)
		}
		// Emit n-1 bytes.
		bytesOut := []byte{byte(val >> 24), byte(val >> 16), byte(val >> 8), byte(val)}
		out = append(out, bytesOut[:n-1]...)
	}
	return out, nil
}

// runLengthDecode decodes RunLengthDecode data. A length byte L: 0..127 copies
// L+1 literal bytes; 129..255 repeats the next byte 257-L times; 128 is EOD.
func runLengthDecode(data []byte) ([]byte, error) {
	var out []byte
	i := 0
	for i < len(data) {
		l := data[i]
		i++
		switch {
		case l == 128:
			return out, nil
		case l < 128:
			n := int(l) + 1
			if i+n > len(data) {
				n = len(data) - i
			}
			out = append(out, data[i:i+n]...)
			i += n
		default:
			n := 257 - int(l)
			if i >= len(data) {
				return out, nil
			}
			b := data[i]
			i++
			for range n {
				out = append(out, b)
			}
		}
	}
	return out, nil
}

func isASCIISpace(c byte) bool {
	switch c {
	case 0x00, 0x09, 0x0A, 0x0C, 0x0D, 0x20:
		return true
	}
	return false
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
