package gen

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
)

// This file builds encrypted PDF fixtures with the Standard Security Handler and
// an EMPTY user password. It mirrors the decryption logic under test in
// pkg/pdf/crypt.go so the fixtures are reproducible and need no external files.
//
// Supported variants: RC4 (V2/R3), AES-128 (V4/R4, AESV2), AES-256 (V5/R6,
// AESV3). A "needs password" variant (non-empty user password) is also produced
// to exercise the ErrEncryptedNeedsPassword path.

// encPad is the 32-byte PDF password padding string (Algorithm 2).
var encPad = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41,
	0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80,
	0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

// encMethod selects the per-object cipher used to write an encrypted fixture.
type encMethod int

const (
	encRC4   encMethod = iota // V2/R3
	encAESV2                  // V4/R4
	encAESV3                  // V5/R6
)

// encBuilder writes a one-page encrypted PDF. Strings and stream bodies are
// encrypted with the file key as objects are added.
type encBuilder struct {
	buf     bytes.Buffer
	offsets []int
	method  encMethod
	fileKey []byte
	id0     []byte
}

// EncryptedRC4PDF returns a single-page RC4-encrypted (V2/R3, 128-bit) PDF with
// an empty user password. The content stream draws a red rectangle and the page
// carries a known /Title string in its catalog for string-decryption coverage.
func EncryptedRC4PDF() []byte { return buildEncrypted(encRC4, "") }

// EncryptedAESV2PDF returns a single-page AES-128 (V4/R4) encrypted PDF with an
// empty user password.
func EncryptedAESV2PDF() []byte { return buildEncrypted(encAESV2, "") }

// EncryptedAESV3PDF returns a single-page AES-256 (V5/R6) encrypted PDF with an
// empty user password.
func EncryptedAESV3PDF() []byte { return buildEncrypted(encAESV3, "") }

// EncryptedNeedsPasswordPDF returns an RC4-encrypted PDF whose user password is
// non-empty ("secret"), so empty-password authentication must fail.
func EncryptedNeedsPasswordPDF() []byte { return buildEncrypted(encRC4, "secret") }

// EncryptedTitle is the known /Title string embedded (encrypted) in the catalog
// of the encrypted fixtures; tests assert it round-trips after decryption.
const EncryptedTitle = "Doctaculous Secret"

// EncryptedContent is the known (decrypted) content stream of the fixtures.
var EncryptedContent = []byte("1 0 0 rg 100 100 200 150 re f")

func buildEncrypted(method encMethod, userPwd string) []byte {
	eb := &encBuilder{method: method, offsets: []int{0}}
	eb.buf.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")

	// Fixed ID for determinism.
	eb.id0 = bytes.Repeat([]byte{0xAB}, 16)

	// Permissions: -44 is a common "allow nothing but print/copy off" mask; the
	// exact value only matters in that it must match the key computation.
	const perms int32 = -44

	var encDict string
	switch method {
	case encRC4:
		o := computeOwnerR234([]byte(userPwd), nil, 3, 16)
		eb.fileKey = computeFileKeyR234([]byte(userPwd), o, perms, eb.id0, 3, 16, true)
		u := computeUserR3([]byte(userPwd), eb.fileKey, eb.id0)
		encDict = fmt.Sprintf("<< /Filter /Standard /V 2 /R 3 /Length 128 /P %d /O %s /U %s >>",
			perms, pdfHexString(o), pdfHexString(u))
	case encAESV2:
		o := computeOwnerR234([]byte(userPwd), nil, 4, 16)
		eb.fileKey = computeFileKeyR234([]byte(userPwd), o, perms, eb.id0, 4, 16, true)
		u := computeUserR3([]byte(userPwd), eb.fileKey, eb.id0)
		encDict = fmt.Sprintf("<< /Filter /Standard /V 4 /R 4 /Length 128 /P %d /O %s /U %s "+
			"/CF << /StdCF << /CFM /AESV2 /Length 16 >> >> /StmF /StdCF /StrF /StdCF >>",
			perms, pdfHexString(o), pdfHexString(u))
	case encAESV3:
		var fileKey [32]byte
		_, _ = rand.Read(fileKey[:])
		eb.fileKey = fileKey[:]
		u, ue := computeUserUE_R6([]byte(userPwd), eb.fileKey)
		o, oe := computeOwnerOE_R6([]byte(userPwd), eb.fileKey, u)
		perms64 := encodePermsV5(perms, true)
		encDict = fmt.Sprintf("<< /Filter /Standard /V 5 /R 6 /Length 256 /P %d "+
			"/O %s /U %s /OE %s /UE %s /Perms %s "+
			"/CF << /StdCF << /CFM /AESV3 /Length 32 >> >> /StmF /StdCF /StrF /StdCF >>",
			perms, pdfHexString(o), pdfHexString(u), pdfHexString(oe), pdfHexString(ue),
			pdfHexString(perms64))
	}

	// Object layout (numbers are 1-based after the free object 0):
	// 1: font, 2: content stream, 3: page, 4: pages, 5: catalog, 6: encrypt dict.
	font := eb.addObjectRaw(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`)
	content := eb.addStream(EncryptedContent)
	pageNum := len(eb.offsets)
	pagesNum := pageNum + 1
	pageBody := fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>",
		pagesNum, font, content)
	page := eb.addObjectRaw(pageBody)
	pages := eb.addObjectRaw(fmt.Sprintf("<< /Type /Pages /Kids [ %d 0 R ] /Count 1 >>", page))
	// Catalog carries an (encrypted) string entry to exercise string decryption.
	// encString must be evaluated for the catalog's object number, so compute it
	// inline against len(offsets) at the moment of the call.
	title := eb.encString(EncryptedTitle)
	catalog := eb.addObjectRaw(
		fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R /Title %s >>", pages, title))

	// The /Encrypt dict is itself NOT encrypted. addObject with encrypt=false.
	encNum := eb.addObjectRaw(encDict)

	return eb.finish(catalog, encNum, perms)
}

// addObjectRaw appends an indirect object with a verbatim body. Any string in
// the body must already have been encrypted (via encString) for this object's
// number.
func (eb *encBuilder) addObjectRaw(body string) int {
	num := len(eb.offsets)
	eb.offsets = append(eb.offsets, eb.buf.Len())
	fmt.Fprintf(&eb.buf, "%d 0 obj\n%s\nendobj\n", num, body)
	return num
}

// addStream appends an encrypted stream object (the data is encrypted with the
// object's key) and returns its number.
func (eb *encBuilder) addStream(data []byte) int {
	num := len(eb.offsets)
	enc := eb.encryptBytes(num, 0, data)
	eb.offsets = append(eb.offsets, eb.buf.Len())
	fmt.Fprintf(&eb.buf, "%d 0 obj\n<< /Length %d >>\nstream\n", num, len(enc))
	eb.buf.Write(enc)
	eb.buf.WriteString("\nendstream\nendobj\n")
	return num
}

// encString encrypts s for the NEXT object number (the object currently being
// assembled). Because strings are embedded in the object body, we must know the
// object's number; the caller adds the object immediately after, so the number
// is len(offsets).
func (eb *encBuilder) encString(s string) string {
	num := len(eb.offsets)
	enc := eb.encryptBytes(num, 0, []byte(s))
	return pdfHexString(enc)
}

func (eb *encBuilder) encryptBytes(num, gen int, data []byte) []byte {
	switch eb.method {
	case encRC4:
		return rc4Bytes(objKey(eb.fileKey, num, gen, false), data)
	case encAESV2:
		return aesCBCEncryptRandIV(objKey(eb.fileKey, num, gen, true), data)
	case encAESV3:
		return aesCBCEncryptRandIV(eb.fileKey, data)
	}
	return data
}

func (eb *encBuilder) finish(rootNum, encNum int, perms int32) []byte {
	xrefOff := eb.buf.Len()
	n := len(eb.offsets)
	fmt.Fprintf(&eb.buf, "xref\n0 %d\n", n)
	eb.buf.WriteString("0000000000 65535 f \n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&eb.buf, "%010d 00000 n \n", eb.offsets[i])
	}
	fmt.Fprintf(&eb.buf,
		"trailer\n<< /Size %d /Root %d 0 R /Encrypt %d 0 R /ID [ %s %s ] >>\n",
		n, rootNum, encNum, pdfHexString(eb.id0), pdfHexString(eb.id0))
	fmt.Fprintf(&eb.buf, "startxref\n%d\n%%%%EOF\n", xrefOff)
	return eb.buf.Bytes()
}

// --- crypto helpers (mirror of pkg/pdf/crypt.go, for fixture generation) ---

func objKey(fileKey []byte, num, gen int, aesSalt bool) []byte {
	h := md5.New()
	h.Write(fileKey)
	var b [5]byte
	b[0] = byte(num)
	b[1] = byte(num >> 8)
	b[2] = byte(num >> 16)
	b[3] = byte(gen)
	b[4] = byte(gen >> 8)
	h.Write(b[:])
	if aesSalt {
		h.Write([]byte{0x73, 0x41, 0x6c, 0x54})
	}
	sum := h.Sum(nil)
	nl := len(fileKey) + 5
	if nl > 16 {
		nl = 16
	}
	return sum[:nl]
}

func rc4Bytes(key, data []byte) []byte {
	c, _ := rc4.NewCipher(key)
	out := make([]byte, len(data))
	c.XORKeyStream(out, data)
	return out
}

func aesCBCEncryptRandIV(key, data []byte) []byte {
	block, _ := aes.NewCipher(key)
	iv := make([]byte, aes.BlockSize)
	_, _ = rand.Read(iv)
	// PKCS#7 pad.
	pad := aes.BlockSize - len(data)%aes.BlockSize
	padded := append(append([]byte{}, data...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)
	return append(append([]byte{}, iv...), ct...)
}

// pad32 pads/truncates a password to 32 bytes per Algorithm 2.
func pad32(pwd []byte) []byte {
	out := make([]byte, 32)
	n := copy(out, pwd)
	copy(out[n:], encPad)
	return out
}

// computeFileKeyR234 is Algorithm 2.
func computeFileKeyR234(userPwd, o []byte, p int32, id0 []byte, r, keyLen int, encryptMeta bool) []byte {
	h := md5.New()
	h.Write(pad32(userPwd))
	h.Write(o[:min(len(o), 32)])
	var pb [4]byte
	binary.LittleEndian.PutUint32(pb[:], uint32(p))
	h.Write(pb[:])
	h.Write(id0)
	if r >= 4 && !encryptMeta {
		h.Write([]byte{0xff, 0xff, 0xff, 0xff})
	}
	sum := h.Sum(nil)
	if r >= 3 {
		for i := 0; i < 50; i++ {
			s := md5.Sum(sum[:keyLen])
			sum = s[:]
		}
	}
	return append([]byte{}, sum[:keyLen]...)
}

// computeOwnerR234 is Algorithm 3 (with an empty owner password, so the owner
// key derives from the user password padding).
func computeOwnerR234(userPwd, ownerPwd []byte, r, keyLen int) []byte {
	if len(ownerPwd) == 0 {
		ownerPwd = userPwd
	}
	h := md5.Sum(pad32(ownerPwd))
	digest := h[:]
	if r >= 3 {
		for i := 0; i < 50; i++ {
			s := md5.Sum(digest[:keyLen])
			digest = s[:]
		}
	}
	rc4Key := digest[:keyLen]
	out := rc4Bytes(rc4Key, pad32(userPwd))
	if r >= 3 {
		for i := 1; i <= 19; i++ {
			k := make([]byte, keyLen)
			for j := 0; j < keyLen; j++ {
				k[j] = rc4Key[j] ^ byte(i)
			}
			out = rc4Bytes(k, out)
		}
	}
	return out
}

// computeUserR3 is Algorithm 5 (R3/R4): produces the 32-byte /U.
func computeUserR3(userPwd, fileKey, id0 []byte) []byte {
	h := md5.New()
	h.Write(encPad)
	h.Write(id0)
	digest := h.Sum(nil)
	out := rc4Bytes(fileKey, digest)
	for i := 1; i <= 19; i++ {
		k := make([]byte, len(fileKey))
		for j := range fileKey {
			k[j] = fileKey[j] ^ byte(i)
		}
		out = rc4Bytes(k, out)
	}
	// Pad to 32 bytes with arbitrary (here zero) bytes.
	return append(out[:16], make([]byte, 16)...)
}

// --- R6 (AES-256) fixture helpers ---

func hash2B(password, salt, udata []byte) []byte {
	h := sha256.New()
	h.Write(password)
	h.Write(salt)
	h.Write(udata)
	k := h.Sum(nil)
	for round := 0; ; round++ {
		base := append(append(append([]byte{}, password...), k...), udata...)
		seq := bytes.Repeat(base, 64)
		block, _ := aes.NewCipher(k[0:16])
		e := make([]byte, len(seq))
		cipher.NewCBCEncrypter(block, k[16:32]).CryptBlocks(e, seq)
		mod := 0
		for i := 0; i < 16; i++ {
			mod += int(e[i])
		}
		switch mod % 3 {
		case 0:
			s := sha256.Sum256(e)
			k = s[:]
		case 1:
			s := sha512.Sum384(e)
			k = s[:]
		case 2:
			s := sha512.Sum512(e)
			k = s[:]
		}
		if round >= 63 && int(e[len(e)-1]) <= round-32 {
			break
		}
	}
	return k[:32]
}

// computeUserUE_R6 builds /U (48 bytes) and /UE (32 bytes) for an empty (or
// given) user password.
func computeUserUE_R6(userPwd, fileKey []byte) (u, ue []byte) {
	valSalt := make([]byte, 8)
	keySalt := make([]byte, 8)
	_, _ = rand.Read(valSalt)
	_, _ = rand.Read(keySalt)
	hash := hash2B(userPwd, valSalt, nil)
	u = append(append(append([]byte{}, hash...), valSalt...), keySalt...) // 48 bytes

	ik := hash2B(userPwd, keySalt, nil)
	block, _ := aes.NewCipher(ik)
	ue = make([]byte, 32)
	iv := make([]byte, 16)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ue, fileKey)
	return u, ue
}

// computeOwnerOE_R6 builds /O (48 bytes) and /OE (32 bytes).
func computeOwnerOE_R6(ownerPwd, fileKey, u []byte) (o, oe []byte) {
	valSalt := make([]byte, 8)
	keySalt := make([]byte, 8)
	_, _ = rand.Read(valSalt)
	_, _ = rand.Read(keySalt)
	hash := hash2B(ownerPwd, valSalt, u[:48])
	o = append(append(append([]byte{}, hash...), valSalt...), keySalt...)

	ik := hash2B(ownerPwd, keySalt, u[:48])
	block, _ := aes.NewCipher(ik)
	oe = make([]byte, 32)
	iv := make([]byte, 16)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(oe, fileKey)
	return o, oe
}

// encodePermsV5 builds a plausible 16-byte /Perms entry. The reader under test
// authenticates via /U's validation salt and does NOT validate /Perms, so this
// value need not be the real AES-256-ECB(fileKey) block — it exists only so the
// /Encrypt dict is well-formed.
func encodePermsV5(p int32, encryptMeta bool) []byte {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint32(b[0:4], uint32(p))
	b[4], b[5], b[6], b[7] = 0xff, 0xff, 0xff, 0xff
	if encryptMeta {
		b[8] = 'T'
	} else {
		b[8] = 'F'
	}
	b[9], b[10], b[11] = 'a', 'd', 'b'
	_, _ = rand.Read(b[12:16])
	return b
}

func pdfHexString(b []byte) string {
	const hexdig = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2+2)
	out = append(out, '<')
	for _, c := range b {
		out = append(out, hexdig[c>>4], hexdig[c&0xf])
	}
	out = append(out, '>')
	return string(out)
}
