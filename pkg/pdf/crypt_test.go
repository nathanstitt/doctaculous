package pdf

import (
	"bytes"
	"errors"
	"testing"

	"github.com/nathanstitt/doctaculous/testdata/gen"
)

// catalogTitle resolves the document catalog's /Title string, exercising
// string decryption on a non-stream object.
func catalogTitle(t *testing.T, doc *Document) string {
	t.Helper()
	cat := doc.GetDict(doc.Trailer()["Root"])
	if cat == nil {
		t.Fatal("no catalog")
	}
	s, ok := doc.Resolve(cat["Title"]).(String)
	if !ok {
		t.Fatalf("/Title not a string: %T", doc.Resolve(cat["Title"]))
	}
	return string(s)
}

func TestDecryptStandard(t *testing.T) {
	cases := []struct {
		name  string
		build func() []byte
	}{
		{"rc4", gen.EncryptedRC4PDF},
		{"aesv2", gen.EncryptedAESV2PDF},
		{"aesv3", gen.EncryptedAESV3PDF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Parse(tc.build())
			if err != nil {
				t.Fatalf("Parse: %v (must NOT be ErrEncrypted for empty-password docs)", err)
			}
			if doc.PageCount() != 1 {
				t.Fatalf("PageCount = %d, want 1", doc.PageCount())
			}

			// Stream decryption: the content stream must decrypt to the known
			// plaintext content.
			pg, err := doc.Page(0)
			if err != nil {
				t.Fatal(err)
			}
			content, err := pg.ContentBytes()
			if err != nil {
				t.Fatalf("ContentBytes: %v", err)
			}
			if !bytes.Equal(content, gen.EncryptedContent) {
				t.Errorf("decrypted content = %q, want %q", content, gen.EncryptedContent)
			}

			// String decryption: the catalog /Title must decrypt to the known
			// plaintext.
			if got := catalogTitle(t, doc); got != gen.EncryptedTitle {
				t.Errorf("decrypted /Title = %q, want %q", got, gen.EncryptedTitle)
			}
		})
	}
}

// TestDecryptNeedsPassword verifies that a document whose user password is
// non-empty cleanly reports ErrEncryptedNeedsPassword rather than panicking or
// returning garbage.
func TestDecryptNeedsPassword(t *testing.T) {
	_, err := Parse(gen.EncryptedNeedsPasswordPDF())
	if !errors.Is(err, ErrEncryptedNeedsPassword) {
		t.Fatalf("Parse error = %v, want ErrEncryptedNeedsPassword", err)
	}
}

// TestDecryptUnsupportedHandler verifies a non-Standard /Filter yields
// ErrEncrypted (genuinely unsupported), not ErrEncryptedNeedsPassword.
func TestDecryptUnsupportedHandler(t *testing.T) {
	// Reuse an RC4 fixture but rewrite /Standard to a bogus handler name. Both
	// are 8 bytes so xref offsets stay valid.
	data := gen.EncryptedRC4PDF()
	patched := bytes.Replace(data, []byte("/Filter /Standard"), []byte("/Filter /Bogusxxx"), 1)
	if bytes.Equal(data, patched) {
		t.Fatal("failed to patch handler name")
	}
	_, err := Parse(patched)
	if !errors.Is(err, ErrEncrypted) {
		t.Fatalf("Parse error = %v, want ErrEncrypted", err)
	}
	if errors.Is(err, ErrEncryptedNeedsPassword) {
		t.Fatal("unsupported handler must not report ErrEncryptedNeedsPassword")
	}
}

// TestDecryptTruncatedNoPanic feeds truncated/garbled encrypted data and
// asserts Parse never panics. It may error or partially succeed; it must not
// crash.
func TestDecryptTruncatedNoPanic(t *testing.T) {
	full := gen.EncryptedRC4PDF()
	for _, n := range []int{0, 10, 50, len(full) / 3, len(full) / 2, len(full) - 5} {
		if n < 0 || n > len(full) {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on truncated[:%d]: %v", n, r)
				}
			}()
			_, _ = Parse(full[:n])
		}()
	}

	// Also corrupt the encrypted stream/string bytes in the middle and ensure no
	// panic when the content is decoded.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic on corrupted body: %v", r)
			}
		}()
		corrupt := append([]byte{}, full...)
		for i := len(corrupt) / 2; i < len(corrupt)/2+20 && i < len(corrupt); i++ {
			corrupt[i] ^= 0xFF
		}
		if doc, err := Parse(corrupt); err == nil {
			if pg, e := doc.Page(0); e == nil {
				_, _ = pg.ContentBytes()
			}
		}
	}()
}
