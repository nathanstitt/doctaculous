package main

import (
	"reflect"
	"testing"
)

func TestResolvePagesSingle(t *testing.T) {
	got, err := resolvePages("", 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{1}) { // 1-based 2 -> 0-based 1
		t.Errorf("got %v, want [1]", got)
	}
}

func TestResolvePagesRange(t *testing.T) {
	got, err := resolvePages("1-3,5", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 1, 2, 4}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolvePagesReversedRange(t *testing.T) {
	got, err := resolvePages("3-1", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Errorf("got %v, want [0 1 2]", got)
	}
}

func TestResolvePagesOutOfRange(t *testing.T) {
	for _, spec := range []struct {
		rng  string
		page int
	}{
		{"", 9},
		{"0", 1},
		{"4-7", 1},
		{"abc", 1},
	} {
		if _, err := resolvePages(spec.rng, spec.page, 5); err == nil {
			t.Errorf("resolvePages(%q,%d,5) expected error", spec.rng, spec.page)
		}
	}
}

func TestResolvePagesDedup(t *testing.T) {
	got, err := resolvePages("1-3,2-4", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 1, 2, 3} // pages 1,2,3,4 deduped, first-seen order
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestOutputPath(t *testing.T) {
	if got := outputPath("page-%d.png", 4, true); got != "page-5.png" {
		t.Errorf("multi output = %q, want page-5.png", got)
	}
	if got := outputPath("out.png", 0, false); got != "out.png" {
		t.Errorf("single output = %q, want out.png", got)
	}
}

func TestReorderArgs(t *testing.T) {
	// Input before flags should be moved to the end.
	got := reorderArgs([]string{"in.pdf", "--out", "o.png", "--dpi", "150"})
	want := []string{"--out", "o.png", "--dpi", "150", "in.pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reorderArgs = %v, want %v", got, want)
	}
}
