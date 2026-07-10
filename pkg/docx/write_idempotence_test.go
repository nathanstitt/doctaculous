package docx_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/docx"
	gendocx "github.com/nathanstitt/doctaculous/testdata/gen/docx"
)

// TestWriteIdempotenceOnParsed pins save-cycle stability over every generated
// package fixture: Parse ∘ Write is a fixed point on parsed documents — the
// exact property a save cycle depends on (no compounding loss) — and the
// second write is byte-identical to the first (the package itself reaches a
// fixed point). It lives in an external test package because the corpus
// builder imports pkg/docx (the model-specimen fixture), which would cycle
// with an in-package test.
//
// Parsed documents need no comparison normalization: their hyperlinks carry
// resolved ids the writer preserves, and their sections/cells are already in
// parse shape. Only the relationship table may gain writer-allocated
// structural entries, so Rels is compared as a superset (every parsed rel
// must survive verbatim).
func TestWriteIdempotenceOnParsed(t *testing.T) {
	for _, f := range gendocx.Core {
		t.Run(f.Name, func(t *testing.T) {
			d1, err := docx.OpenBytes(f.Bytes())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			pkg2, err := docx.Bytes(d1)
			if err != nil {
				t.Fatalf("write: %v", err)
			}
			d2, err := docx.OpenBytes(pkg2)
			if err != nil {
				t.Fatalf("reopen written: %v", err)
			}
			pkg3, err := docx.Bytes(d2)
			if err != nil {
				t.Fatalf("write second cycle: %v", err)
			}

			// The write cycle reaches a byte-level fixed point immediately.
			if !bytes.Equal(pkg2, pkg3) {
				t.Errorf("second save cycle is not byte-identical to the first")
			}

			// Every parsed relationship survives verbatim (the writer may add
			// structural rels, never lose or rewrite existing ones).
			for id, rel := range d1.Rels {
				if got, ok := d2.Rels[id]; !ok || got != rel {
					t.Errorf("relationship %s did not survive: had %+v, got %+v (ok=%v)", id, rel, got, ok)
				}
			}

			// Model equality outside the rel table.
			d1.Rels, d2.Rels = nil, nil
			if !reflect.DeepEqual(d1, d2) {
				t.Errorf("Parse∘Write is not a fixed point")
			}
		})
	}
}
