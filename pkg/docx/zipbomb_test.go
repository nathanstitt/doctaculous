package docx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
	"testing"
)

// buildPackage assembles a minimal OOXML package holding n media parts of
// partSize bytes each. Highly compressible content keeps the ZIP tiny, which is
// the whole point of a zip bomb: a small file that costs a lot to read.
func buildPackage(t *testing.T, n, partSize int) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(name string, body []byte) {
		t.Helper()
		w, err := z.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	add("[Content_Types].xml", []byte(`<?xml version="1.0"?>`+
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`))
	zeros := make([]byte, partSize)
	for i := range n {
		add(fmt.Sprintf("word/media/img%02d.bin", i), zeros)
	}
	if err := z.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// TestMediaPartsTotalBounded covers the aggregate budget across parts.
//
// maxPartSize bounds one part, but partsWithPrefix reads EVERY matching entry,
// so N parts each just under the per-part cap multiply. Measured before the
// budget: a 4 MB .docx with 20 compressible word/media parts decompressed to
// 4.2 GB (a 1,028x amplification) and drove peak RSS to 6 GB — with every
// individual part comfortably inside its 256 MiB limit the whole way.
//
// The budget is lowered for the test rather than building a half-gigabyte
// fixture: the property under test is that the SUM is bounded, which does not
// depend on the constant's production value.
func TestMediaPartsTotalBounded(t *testing.T) {
	const partSize = 64 << 10 // 64 KiB per part
	const parts = 20

	orig := totalPartBudget
	totalPartBudget = 5 * partSize // room for 5 of the 20 parts
	defer func() { totalPartBudget = orig }()

	data := buildPackage(t, parts, partSize)
	p, err := openPackage(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("openPackage: %v", err)
	}

	got := p.mediaParts()
	total := 0
	for _, b := range got {
		total += len(b)
	}
	if total > totalPartBudget {
		t.Errorf("mediaParts returned %d bytes, past the %d-byte budget", total, totalPartBudget)
	}
	if len(got) == 0 {
		t.Error("the budget dropped every part; it should keep what fits")
	}
	if len(got) == parts {
		t.Errorf("all %d parts were returned; the budget did not apply", parts)
	}
}

// TestMediaPartsTruncationIsDeterministic pins which parts survive. Map
// iteration order is random in Go, so without the sort a truncated read would
// return a different subset on every run — turning one hostile document into a
// flaky renderer.
func TestMediaPartsTruncationIsDeterministic(t *testing.T) {
	const partSize = 64 << 10
	orig := totalPartBudget
	totalPartBudget = 3 * partSize
	defer func() { totalPartBudget = orig }()

	data := buildPackage(t, 10, partSize)

	var first []string
	for run := range 5 {
		p, err := openPackage(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("openPackage: %v", err)
		}
		var names []string
		for name := range p.mediaParts() {
			names = append(names, name)
		}
		sort.Strings(names)
		if run == 0 {
			first = names
			continue
		}
		if len(names) != len(first) {
			t.Fatalf("run %d returned %d parts, run 0 returned %d", run, len(names), len(first))
		}
		for i := range names {
			if names[i] != first[i] {
				t.Fatalf("run %d part %d = %q, run 0 had %q", run, i, names[i], first[i])
			}
		}
	}
	// Sorted order means the surviving parts are the FIRST ones by name.
	if len(first) > 0 && first[0] != "word/media/img00.bin" {
		t.Errorf("kept parts start at %q, want the lowest-named part", first[0])
	}
}

// TestOrdinaryPackageUnaffected guards against the budget being too tight: a
// normal document's media must come back whole.
func TestOrdinaryPackageUnaffected(t *testing.T) {
	const partSize = 32 << 10 // 32 KiB, a plausible small image
	data := buildPackage(t, 8, partSize)
	p, err := openPackage(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("openPackage: %v", err)
	}
	got := p.mediaParts()
	if len(got) != 8 {
		t.Errorf("an ordinary 8-image package returned %d parts, want 8", len(got))
	}
	for name, b := range got {
		if len(b) != partSize {
			t.Errorf("part %s is %d bytes, want %d", name, len(b), partSize)
		}
	}
}
