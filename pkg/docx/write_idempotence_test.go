package docx_test

import (
	"bytes"
	"os"
	"path/filepath"
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
			assertWriteFixedPoint(t, f.Bytes())
		})
	}
}

// TestWriteIdempotenceOnExternalCorpus enforces the same save-cycle contract
// over the committed real-world documents in testdata/external/docx — Word-,
// Mac-Word-, and LibreOffice-authored files carrying tracked changes,
// comments, notes, numbering overrides, headers/footers, and fields (see that
// directory's README for provenance and licensing). Skips if the corpus is
// absent (a sparse checkout).
func TestWriteIdempotenceOnExternalCorpus(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "external", "docx", "*.docx"))
	if err != nil || len(files) == 0 {
		t.Skip("external docx corpus not present")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			assertWriteFixedPoint(t, data)
		})
	}
}

// assertWriteFixedPoint checks the save-cycle contract on one package: parse,
// write, reparse, write again — byte-identical second write, every parsed
// relationship surviving verbatim, and model equality outside the rel table.
func assertWriteFixedPoint(t *testing.T, pkg1 []byte) {
	t.Helper()
	d1, err := docx.OpenBytes(pkg1)
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
}
