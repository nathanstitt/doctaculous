package omnidoc

import (
	"errors"
	"strings"
	"testing"
)

// TestErrTooDeeplyNestedIsPublic pins that the nesting guards in the HTML and
// Markdown parsers surface as the ONE exported sentinel. Each internal package
// has its own, which an importer cannot name; without this mapping the
// CHANGELOG's promise that callers can errors.Is on it would be false.
func TestErrTooDeeplyNestedIsPublic(t *testing.T) {
	deepHTML := []byte(strings.Repeat("<div>", 600) + "x" + strings.Repeat("</div>", 600))
	if _, err := OpenHTMLBytes(deepHTML); !errors.Is(err, ErrTooDeeplyNested) {
		t.Errorf("HTML nesting: err = %v, want ErrTooDeeplyNested", err)
	}
	deepMD := []byte(strings.Repeat("- ", 2000) + "x\n")
	if _, err := OpenMarkdownBytes(deepMD); !errors.Is(err, ErrTooDeeplyNested) {
		t.Errorf("Markdown nesting: err = %v, want ErrTooDeeplyNested", err)
	}
	// The generic opener routes through the same frontends.
	if _, err := OpenBytesAs(FormatMarkdown, deepMD); !errors.Is(err, ErrTooDeeplyNested) {
		t.Errorf("OpenBytesAs(markdown): err = %v, want ErrTooDeeplyNested", err)
	}
	// An ordinary document is untouched by the mapping.
	if _, err := OpenMarkdownBytes([]byte("- one\n- two\n")); err != nil {
		t.Errorf("shallow list: %v", err)
	}
}
