package omnidoc

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestWithContextCancelsOpen covers the exported cancellation option.
//
// Before it, context.Context was reachable only through a parallel *Context
// naming family (OpenHTMLBytesContext, OpenURLContext, OpenHTMLFileContext)
// covering HTML and URLs; every other Open* had no way to cancel, and
// OpenHTMLBytesContext's own doc admitted the plumbing existed but that "there
// is no exported way to" reach it. One option covers them all.
func TestWithContextCancelsOpen(t *testing.T) {
	// Big enough that layout does real work, so a cancellation has something to
	// interrupt rather than racing an instant return.
	src := []byte("<html><body>" + strings.Repeat("<p>hello world</p>", 20000) + "</body></html>")

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := OpenHTMLBytes(src, WithContext(cancelled)); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context: err = %v, want context.Canceled", err)
	}
	if _, err := OpenHTMLBytes(src, WithContext(context.Background())); err != nil {
		t.Errorf("live context: %v", err)
	}
	// A nil context is ignored, matching the *Context functions' contract. The
	// linter is right that callers should not pass nil; this asserts the option
	// tolerates it rather than panicking, which is the documented behaviour.
	if _, err := OpenHTMLBytes(src, WithContext(nil)); err != nil { //nolint:staticcheck // SA1012: nil tolerance is the contract under test
		t.Errorf("nil context: %v", err)
	}
}

// TestWithContextReachesEveryFrontend is the point of exporting it: the option
// applies to frontends the *Context family never covered, because they all
// resolve options through the same config.
func TestWithContextReachesEveryFrontend(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name   string
		format Format
		data   []byte
	}{
		{"markdown", FormatMarkdown, []byte("# heading\n\ntext\n")},
		{"text", FormatText, []byte("plain text\n")},
		{"csv", FormatCSV, []byte("a,b\n1,2\n")},
		{"html", FormatHTML, []byte("<p>x</p>")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := OpenBytesAs(c.format, c.data, WithContext(cancelled))
			if !errors.Is(err, context.Canceled) {
				t.Errorf("err = %v, want context.Canceled", err)
			}
			// The same input opens fine without a dead context, so the failure
			// above is the cancellation and not the fixture.
			if _, err := OpenBytesAs(c.format, c.data); err != nil {
				t.Errorf("uncancelled open failed: %v", err)
			}
		})
	}
}

// TestWithContextCallerWins pins the ordering the *Context functions promise:
// they prepend the option internally, so a caller passing their own still wins.
func TestWithContextCallerWins(t *testing.T) {
	src := []byte("<p>x</p>")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	// The entry point prepends context.Background(); the caller's cancelled
	// context comes later and must override it.
	if _, err := OpenHTMLBytesContext(context.Background(), src, WithContext(cancelled)); !errors.Is(err, context.Canceled) {
		t.Errorf("caller's WithContext did not win: err = %v, want context.Canceled", err)
	}
}
