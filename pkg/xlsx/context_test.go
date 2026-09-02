package xlsx

import (
	"context"
	"errors"
	"testing"
)

// TestContextCancelsOpenEditSave pins that a cancelled context stops the
// reader, the editor's open, and Save with context.Canceled, and that a live
// or nil context leaves the output untouched.
func TestContextCancelsOpenEditSave(t *testing.T) {
	f := New()
	sh, err := f.Sheet("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.SetString(1, 1, "hello"); err != nil {
		t.Fatal(err)
	}
	data, err := f.Save(context.Background())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := OpenBytes(cancelled, data); !errors.Is(err, context.Canceled) {
		t.Errorf("OpenBytes(cancelled): err = %v, want context.Canceled", err)
	}
	if _, err := Edit(cancelled, data); !errors.Is(err, context.Canceled) {
		t.Errorf("Edit(cancelled): err = %v, want context.Canceled", err)
	}
	if _, err := f.Save(cancelled); !errors.Is(err, context.Canceled) {
		t.Errorf("Save(cancelled): err = %v, want context.Canceled", err)
	}

	// nil is tolerated and means Background.
	again, err := f.Save(nil) //nolint:staticcheck // SA1012: nil tolerance is the contract under test
	if err != nil {
		t.Fatalf("Save(nil): %v", err)
	}
	if string(again) != string(data) {
		t.Error("Save(nil) differs from Save(Background)")
	}
	wb, err := OpenBytes(nil, data) //nolint:staticcheck // SA1012
	if err != nil {
		t.Fatalf("OpenBytes(nil): %v", err)
	}
	if got := wb.Sheets[0].Cells[0][0].Text; got != "hello" {
		t.Errorf("cell A1 = %q, want hello", got)
	}
}
