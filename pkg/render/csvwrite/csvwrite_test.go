package csvwrite

import (
	"context"
	"strings"
	"testing"

	"github.com/nathanstitt/doctaculous/pkg/html"
	layoutcss "github.com/nathanstitt/doctaculous/pkg/layout/css"
)

// renderHTML builds a cssbox tree from src (the markdown writer's test
// pattern) and renders it to CSV.
func renderHTML(t *testing.T, src string, opts Options) (string, []string) {
	t.Helper()
	var logs []string
	if opts.Logf == nil {
		opts.Logf = func(f string, a ...any) { logs = append(logs, f) }
	}
	doc, err := html.Parse([]byte(src))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	root, err := layoutcss.Build(context.Background(), doc, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var sb strings.Builder
	if err := Write(root, &sb, opts); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return sb.String(), logs
}

func TestSimpleTable(t *testing.T) {
	got, _ := renderHTML(t, `<html><body><table>
	<tr><th>A</th><th>B</th></tr>
	<tr><td>1</td><td>x, y</td></tr>
	</table></body></html>`, Options{})
	want := "A,B\n1,\"x, y\"\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSpansDuplicate(t *testing.T) {
	got, _ := renderHTML(t, `<html><body><table>
	<tr><td colspan="2">wide</td><td>c</td></tr>
	<tr><td rowspan="2">tall</td><td>m1</td><td>m2</td></tr>
	<tr><td>b1</td><td>b2</td></tr>
	</table></body></html>`, Options{})
	want := "wide,wide,c\ntall,m1,m2\ntall,b1,b2\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMultipleTablesBlankLineSeparated(t *testing.T) {
	got, _ := renderHTML(t, `<html><body>
	<table><tr><td>t1</td></tr></table>
	<p>between</p>
	<table><tr><td>t2</td></tr></table>
	</body></html>`, Options{})
	if got != "t1\n\nt2\n" {
		t.Errorf("got %q", got)
	}
}

func TestTSVDelimiter(t *testing.T) {
	got, _ := renderHTML(t, `<html><body><table>
	<tr><td>a</td><td>with, comma</td></tr>
	</table></body></html>`, Options{Comma: '\t'})
	if got != "a\twith, comma\n" {
		t.Errorf("got %q", got)
	}
}

func TestNoTablesLogsAndEmitsNothing(t *testing.T) {
	got, logs := renderHTML(t, `<html><body><h1>Title</h1><p>prose only</p></body></html>`, Options{})
	if got != "" {
		t.Errorf("table-less document produced output: %q", got)
	}
	found := false
	for _, l := range logs {
		if strings.Contains(l, "no tables") {
			found = true
		}
	}
	if !found {
		t.Errorf("no-tables condition not logged: %v", logs)
	}
}

func TestDroppedProseLogged(t *testing.T) {
	_, logs := renderHTML(t, `<html><body><p>prose</p><table><tr><td>x</td></tr></table></body></html>`, Options{})
	found := false
	for _, l := range logs {
		if strings.Contains(l, "dropped") {
			found = true
		}
	}
	if !found {
		t.Errorf("dropped prose not logged: %v", logs)
	}
}

func TestMultiBlockCell(t *testing.T) {
	got, _ := renderHTML(t, `<html><body><table>
	<tr><td><p>first para</p><p>second para</p></td></tr>
	</table></body></html>`, Options{})
	if got != "first para second para\n" {
		t.Errorf("got %q", got)
	}
}

func TestNilRoot(t *testing.T) {
	var sb strings.Builder
	if err := Write(nil, &sb, Options{}); err != nil {
		t.Fatalf("Write(nil): %v", err)
	}
	if sb.Len() != 0 {
		t.Errorf("nil root produced output: %q", sb.String())
	}
}
