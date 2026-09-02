package docx

import (
	"context"
	"errors"
	"testing"
)

// TestContextCancelsOpenAndWrite pins that a cancelled context stops both the
// parse and the serialization with context.Canceled, and that a live context
// changes nothing — the same bytes come out either way.
func TestContextCancelsOpenAndWrite(t *testing.T) {
	doc := &Document{Styles: DefaultStyles(), Body: []Block{{Paragraph: &Paragraph{Content: []ParaChild{{Run: &Run{Text: "hello"}}}}}}}
	data, err := Bytes(context.Background(), doc)
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := OpenBytes(cancelled, data); !errors.Is(err, context.Canceled) {
		t.Errorf("OpenBytes(cancelled): err = %v, want context.Canceled", err)
	}
	if _, err := Bytes(cancelled, doc); !errors.Is(err, context.Canceled) {
		t.Errorf("Bytes(cancelled): err = %v, want context.Canceled", err)
	}
	// nil is tolerated and means Background.
	again, err := Bytes(nil, doc) //nolint:staticcheck // SA1012: nil tolerance is the contract under test
	if err != nil {
		t.Fatalf("Bytes(nil): %v", err)
	}
	if string(again) != string(data) {
		t.Error("Bytes(nil) differs from Bytes(Background)")
	}
	if _, err := OpenBytes(nil, data); err != nil { //nolint:staticcheck // SA1012
		t.Errorf("OpenBytes(nil): %v", err)
	}
}
