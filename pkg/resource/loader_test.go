package resource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMapLoaderLoadsRegistered(t *testing.T) {
	l := MapLoader{"theme.css": {Data: []byte("p{color:red}"), ContentType: "text/css"}}
	data, ct, err := l.Load(context.Background(), "theme.css")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "p{color:red}" || ct != "text/css" {
		t.Errorf("got (%q,%q)", data, ct)
	}
}

func TestMapLoaderNotFound(t *testing.T) {
	l := MapLoader{}
	_, _, err := l.Load(context.Background(), "missing.css")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestLoaderHonorsCancellation(t *testing.T) {
	l := MapLoader{"a.css": {Data: []byte("x"), ContentType: "text/css"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := l.Load(ctx, "a.css"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDirLoaderServesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.css"), []byte("a{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := DirLoader{Base: dir}
	data, ct, err := l.Load(context.Background(), "s.css")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "a{}" || ct != "text/css" {
		t.Errorf("got (%q,%q)", data, ct)
	}
}

func TestDirLoaderMissingIsNotFound(t *testing.T) {
	l := DirLoader{Base: t.TempDir()}
	if _, _, err := l.Load(context.Background(), "nope.css"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
