package resource

import (
	"bytes"
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

func TestDirLoaderHonorsCancellation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.css"), []byte("a{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := DirLoader{Base: dir}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := l.Load(ctx, "s.css"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDirLoaderServesParentRelativeWithinRoot(t *testing.T) {
	root := t.TempDir()
	// Layout mirrors the showcase: <root>/index.html, <root>/css/main.css,
	// <root>/img/tile.png. A stylesheet under css/ references ../img/tile.png.
	if err := os.MkdirAll(filepath.Join(root, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("PNGDATA")
	if err := os.WriteFile(filepath.Join(root, "img", "tile.png"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	d := DirLoader{Base: root}

	// A ref that resolves (via "..") to a file INSIDE the root must be served.
	got, _, err := d.Load(context.Background(), "css/../img/tile.png")
	if err != nil {
		t.Fatalf("css/../img/tile.png: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	// A ref that truly escapes the root must still be refused.
	if _, _, err := d.Load(context.Background(), "../../etc/passwd"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("escaping ref: err = %v, want ErrNotFound", err)
	}
}

func TestDirLoaderServesRawParentRelative(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("TILE")
	if err := os.WriteFile(filepath.Join(root, "img", "tile.png"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	d := DirLoader{Base: root}
	got, _, err := d.Load(context.Background(), "../img/tile.png")
	if err != nil {
		t.Fatalf("../img/tile.png: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDirLoaderRefusesTraversal(t *testing.T) {
	dir := t.TempDir()
	// A file OUTSIDE the base dir.
	outside := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	l := DirLoader{Base: dir}
	if _, _, err := l.Load(context.Background(), "../secret.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound (traversal must be refused)", err)
	}
}
