package font

import "testing"

func TestParseToUnicodeCMapBFChar(t *testing.T) {
	cmap := []byte(`/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CMapName /Adobe-Identity-UCS def
/CMapType 2 def
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
3 beginbfchar
<0029> <0046>
<0048> <0065>
<0050> <D83DDE00>
endbfchar
endcmap
CMapName currentdict /CMap defineresource pop
end
end
`)
	m := parseToUnicodeCMap(cmap)
	if m == nil {
		t.Fatal("parseToUnicodeCMap returned nil")
	}
	want := map[int]rune{0x29: 'F', 0x48: 'e', 0x50: 0x1F600} // incl. a surrogate pair
	for code, r := range want {
		if m[code] != r {
			t.Errorf("m[%#x] = %q; want %q", code, m[code], r)
		}
	}
}

func TestParseToUnicodeCMapBFRange(t *testing.T) {
	cmap := []byte(`begincmap
2 beginbfrange
<0041> <0043> <0061>
<0060> <0062> [<0058> <0059> <005A>]
endbfrange
endcmap
`)
	m := parseToUnicodeCMap(cmap)
	if m == nil {
		t.Fatal("parseToUnicodeCMap returned nil")
	}
	want := map[int]rune{
		0x41: 'a', 0x42: 'b', 0x43: 'c', // incrementing form
		0x60: 'X', 0x61: 'Y', 0x62: 'Z', // array form
	}
	for code, r := range want {
		if m[code] != r {
			t.Errorf("m[%#x] = %q; want %q", code, m[code], r)
		}
	}
}

func TestParseToUnicodeCMapMalformed(t *testing.T) {
	// Garbage and empty inputs must not panic; they yield no mapping.
	for _, src := range [][]byte{nil, []byte("not a cmap"), []byte("beginbfchar <00")} {
		if m := parseToUnicodeCMap(src); m != nil {
			t.Errorf("parseToUnicodeCMap(%q) = %v; want nil", src, m)
		}
	}
	// A malformed tail keeps entries parsed before it.
	partial := []byte("1 beginbfchar <0041> <0061> endbfchar 1 beginbfchar <00")
	m := parseToUnicodeCMap(partial)
	if m[0x41] != 'a' {
		t.Errorf("m[0x41] = %q; want 'a'", m[0x41])
	}
}
