package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The four defects below were all found by FuzzParse rather than by reading the
// code, and each is checked here against the specific shape that triggered it.
// Three of the four are unrecoverable at runtime -- a stack overflow and an OOM
// are raised through runtime.throw, which recover() cannot catch -- so "the
// parser returns an error" is the only acceptable outcome, not "the caller
// handles the panic".

// TestObjectNestingBounded covers arrays and dictionaries nested deeply enough
// to exhaust the stack. Measured before the depth cap: ~1.2 MB of "[" survived,
// ~1.5 MB raised `fatal error: stack overflow`.
func TestObjectNestingBounded(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"balanced arrays", strings.Repeat("[", 2000000) + strings.Repeat("]", 2000000)},
		{"unterminated arrays", strings.Repeat("[", 2000000)},
		{"nested dictionaries", strings.Repeat("<</K ", 2000000) + "null" + strings.Repeat(">>", 2000000)},
		{"unterminated dictionaries", strings.Repeat("<</K ", 2000000)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "%PDF-1.7\n1 0 obj " + c.body + " endobj\ntrailer <</Root 1 0 R>>\n"
			// A stack overflow here kills the process; reaching the assertion at
			// all is most of the test.
			if _, err := Parse([]byte(src)); err == nil {
				t.Log("parsed without error (acceptable: the requirement is that it returns)")
			}
		})
	}
}

// TestNestingUnderCapStillParses checks the cap did not break ordinary nesting.
// Real PDFs nest a handful of levels; maxObjectDepth is 256.
func TestNestingUnderCapStillParses(t *testing.T) {
	const depth = 200
	body := strings.Repeat("[", depth) + "42" + strings.Repeat("]", depth)
	p := newObjParser([]byte(body))
	obj, err := p.parseObject()
	if err != nil {
		t.Fatalf("depth %d should parse: %v", depth, err)
	}
	// Unwrap to the integer to prove the structure survived intact.
	for range depth {
		arr, ok := obj.(Array)
		if !ok || len(arr) != 1 {
			t.Fatalf("expected a 1-element array at each level, got %T", obj)
		}
		obj = arr[0]
	}
	if n, ok := obj.(Integer); !ok || n != 42 {
		t.Errorf("innermost value = %v, want Integer(42)", obj)
	}
}

// TestPageTreeCycleBounded covers a page tree whose Kids point back at earlier
// nodes. The depth cap alone never fires: the fan-out is exponential BELOW it,
// so the walk completes -- with 4.2 million pages from a 1,427-byte file, and 67
// million from 240 bytes more.
func TestPageTreeCycleBounded(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"self-referencing node", `%PDF-1.7
1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj
2 0 obj <</Type/Pages/Kids[2 0 R 2 0 R]/Count 2>> endobj
trailer <</Root 1 0 R>>
`},
		{"mutually-referencing nodes", `%PDF-1.7
1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj
2 0 obj <</Type/Pages/Kids[3 0 R 3 0 R]/Count 2>> endobj
3 0 obj <</Type/Pages/Kids[2 0 R 2 0 R]/Count 2>> endobj
trailer <</Root 1 0 R>>
`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			done := make(chan int, 1)
			go func() {
				doc, err := Parse([]byte(c.src))
				if err != nil || doc == nil {
					done <- 0
					return
				}
				done <- doc.PageCount()
			}()
			select {
			case n := <-done:
				// The bound is what matters, not the exact count: each node is
				// visited once, so this cannot exceed the objects in the file.
				if n > 100 {
					t.Errorf("page count %d: the cycle is still expanding", n)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("Parse did not return: the page tree walk is still expanding")
			}
		})
	}
}

// TestExponentialFanoutBounded is the shape the audit measured: a chain of nodes
// each pointing twice at the next. Without the visited set this produced 2^n
// pages from a file that fits in a tweet.
func TestExponentialFanoutBounded(t *testing.T) {
	const levels = 26
	var b strings.Builder
	b.WriteString("%PDF-1.7\n1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj\n")
	for i := 2; i < 2+levels; i++ {
		fmt.Fprintf(&b, "%d 0 obj <</Type/Pages/Kids[%d 0 R %d 0 R]/Count 2>> endobj\n", i, i+1, i+1)
	}
	fmt.Fprintf(&b, "%d 0 obj <</Type/Page/MediaBox[0 0 10 10]>> endobj\n", 2+levels)
	b.WriteString("trailer <</Root 1 0 R>>\n")

	done := make(chan int, 1)
	go func() {
		doc, err := Parse([]byte(b.String()))
		if err != nil || doc == nil {
			done <- 0
			return
		}
		done <- doc.PageCount()
	}()
	select {
	case n := <-done:
		if n > 100 {
			t.Errorf("page count %d from a %d-byte file: fan-out is unbounded", n, b.Len())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Parse did not return")
	}
}

// TestObjStreamCountBounded covers /N taken straight from the file and used to
// size a slice. Found by fuzzing: a 504-byte input declaring /N 40000000020
// asked for a 320 GB allocation, which killed the process under the fuzzer's
// parallel workers.
//
// The assertion is that parseObjStmData REJECTS the count rather than that the
// process survives: a single-threaded test with plenty of free memory is not a
// reliable witness here. Go's allocator refuses an obviously impossible make()
// with a catchable panic, so without the bound this returns rather than dying —
// it is the concurrent case, with real memory pressure, that kills. Asserting
// the rejection makes the test independent of how much memory the machine
// running it happens to have.
func TestObjStreamCountBounded(t *testing.T) {
	data := []byte("0 0") // 3 bytes: room for no entries at all
	for _, n := range []int{40000000020, 2000000000, 1 << 40} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			_, err := parseObjStmData(data, n, 6)
			if err == nil {
				t.Fatalf("N=%d against %d bytes of data was accepted; it must be refused before allocating", n, len(data))
			}
			if !strings.Contains(err.Error(), "can hold") {
				t.Errorf("error = %v, want it to name the data-size bound", err)
			}
		})
	}
	// End to end, through the public entry point.
	src := "%PDF-1.7\n1 0 obj <</Type/ObjStm/N 40000000020/First 6/Length 4>> stream\n0 0\nendstream endobj\n" +
		"trailer <</Root 1 0 R>>\n"
	if _, err := Parse([]byte(src)); err == nil {
		t.Log("Parse returned no error (acceptable: the requirement is that it does not allocate from N)")
	}
	// The bound must not reject a legitimate stream.
	if _, err := parseObjStmData([]byte("1 0 2 6 <</A 1>> <</B 2>>"), 2, 8); err != nil {
		t.Errorf("a well-formed 2-object stream was rejected: %v", err)
	}
}

// TestObjStreamSelfReference covers an object stream whose own /N is an indirect
// reference living inside that same stream. The cycle runs through the Document
// (loadObjStream -> GetInt -> Resolve -> loadObject -> parseObjectFromStream ->
// loadObjStream), so the parser's depth cap cannot see it: each level builds a
// fresh parser. Found by fuzzing, as a stack overflow.
func TestObjStreamSelfReference(t *testing.T) {
	src := `%PDF-1.7
1 0 obj <</Type/ObjStm/N 2 0 R/First 6/Length 4>> stream
0 0
endstream endobj
2 0 obj 5 endobj
trailer <</Root 1 0 R/Size 3>>
`
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = Parse([]byte(src))
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Parse did not return: the object-stream cycle is unbounded")
	}
}

// TestStreamLengthOverflow covers a /Length large enough that start+n overflows
// to a negative number, which passed the old "start+n <= len(src)" bound and
// then indexed the source slice out of range. Found by fuzzing, reported as
// `index out of range [-9223372036854775764]`.
func TestStreamLengthOverflow(t *testing.T) {
	for _, length := range []string{
		"9223372036854775807", // MaxInt64
		"9223372036854775000",
		"-1",
	} {
		t.Run(length, func(t *testing.T) {
			src := "%PDF-1.7\n1 0 obj <</Length " + length + ">> stream\nabc\nendstream endobj\ntrailer <</Root 1 0 R>>\n"
			// A panic here fails the test rather than the process, since it is
			// an ordinary index panic rather than a runtime.throw.
			if _, err := Parse([]byte(src)); err == nil {
				t.Log("parsed without error (acceptable: it must not index out of range)")
			}
		})
	}
}

// TestDecodedStreamSizeBounded covers a compression bomb. Measured before the
// cap: a 2.9 MB PDF whose content stream decoded to 2 GB drove peak RSS to
// 4.5 GB in about a second, and the ratio holds for larger inputs.
func TestDecodedStreamSizeBounded(t *testing.T) {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	chunk := bytes.Repeat([]byte("0 0 m "), 1000)
	for range 100000 { // ~600 MB decoded, past the 512 MB ceiling
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("compress: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("compress close: %v", err)
	}
	comp := z.Bytes()

	src := fmt.Sprintf("%%PDF-1.7\n"+
		"1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj\n"+
		"2 0 obj <</Type/Pages/Kids[3 0 R]/Count 1>> endobj\n"+
		"3 0 obj <</Type/Page/Parent 2 0 R/MediaBox[0 0 10 10]/Contents 4 0 R>> endobj\n"+
		"4 0 obj <</Length %d/Filter/FlateDecode>> stream\n", len(comp))
	full := append([]byte(src), comp...)
	full = append(full, []byte("\nendstream endobj\ntrailer <</Root 1 0 R>>\n")...)

	doc, err := Parse(full)
	if err != nil {
		return // refusing the document outright is acceptable
	}
	pg, err := doc.Page(0)
	if err != nil {
		return
	}
	content, err := pg.ContentBytes()
	if err == nil {
		t.Fatalf("a %d-byte input decoded to %d bytes; it should have been refused",
			len(full), len(content))
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %v, want it to name the size limit", err)
	}
}

// TestOrdinaryStreamStillDecodes checks the size ceiling does not disturb a
// normal compressed stream.
func TestOrdinaryStreamStillDecodes(t *testing.T) {
	payload := bytes.Repeat([]byte("0 0 m 10 10 l S "), 100)
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("compress: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("compress close: %v", err)
	}
	comp := z.Bytes()

	src := fmt.Sprintf("%%PDF-1.7\n"+
		"1 0 obj <</Type/Catalog/Pages 2 0 R>> endobj\n"+
		"2 0 obj <</Type/Pages/Kids[3 0 R]/Count 1>> endobj\n"+
		"3 0 obj <</Type/Page/Parent 2 0 R/MediaBox[0 0 10 10]/Contents 4 0 R>> endobj\n"+
		"4 0 obj <</Length %d/Filter/FlateDecode>> stream\n", len(comp))
	full := append([]byte(src), comp...)
	full = append(full, []byte("\nendstream endobj\ntrailer <</Root 1 0 R>>\n")...)

	doc, err := Parse(full)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pg, err := doc.Page(0)
	if err != nil {
		t.Fatalf("Page(0): %v", err)
	}
	content, err := pg.ContentBytes()
	if err != nil {
		t.Fatalf("ContentBytes: %v", err)
	}
	if !bytes.Equal(content, payload) {
		t.Errorf("content = %d bytes, want the original %d", len(content), len(payload))
	}
}
