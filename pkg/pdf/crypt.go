package pdf

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rc4"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrEncryptedNeedsPassword is returned when a document uses the Standard
// Security Handler but the empty user password does not authenticate, i.e. a
// non-empty user password would be required to read it. Only empty-password
// decryption is supported.
var ErrEncryptedNeedsPassword = errors.New("pdf: document requires a password")

// passwordPad is the 32-byte padding string from the PDF spec (Algorithm 2),
// used to pad/truncate passwords for R2–R4.
var passwordPad = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41,
	0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80,
	0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

// cryptMethod is the per-object encryption algorithm selected by the security
// handler (and, for V4/V5, by the chosen crypt filter).
type cryptMethod int

const (
	cryptNone cryptMethod = iota // Identity / no encryption
	cryptRC4
	cryptAESV2 // AES-128 CBC
	cryptAESV3 // AES-256 CBC
)

// encrypter holds the resolved Standard Security Handler state for a document.
// It is computed once at Open time and is read-only afterwards, so it is safe
// for concurrent use by the parallel render path.
type encrypter struct {
	key    []byte      // file encryption key
	method cryptMethod // method for strings and streams
	v, r   int
}

// setupEncryption inspects the trailer's /Encrypt entry. It returns:
//   - (nil, nil) if the document is not encrypted;
//   - (enc, nil) if it is encrypted with the Standard handler and the empty
//     user password authenticates;
//   - (nil, ErrEncryptedNeedsPassword) if Standard but the empty password fails;
//   - (nil, ErrEncrypted) for any unsupported handler/algorithm.
//
// It must be called before any string/stream object (other than the /Encrypt
// dict and the xref stream) is decrypted.
func (d *Document) setupEncryption() (*encrypter, error) {
	encObj, ok := d.trailer["Encrypt"]
	if !ok {
		return nil, nil
	}
	// The /Encrypt dictionary itself is never encrypted; resolving it here is
	// safe because no encrypter is installed yet.
	encDict := d.GetDict(encObj)
	if encDict == nil {
		return nil, fmt.Errorf("pdf: /Encrypt is not a dictionary: %w", ErrEncrypted)
	}

	if filt, _ := d.GetName(encDict["Filter"]); filt != "Standard" {
		return nil, fmt.Errorf("pdf: unsupported security handler /%s: %w", filt, ErrEncrypted)
	}

	v, _ := d.GetInt(encDict["V"])
	r, _ := d.GetInt(encDict["R"])

	// Document ID (first element of trailer /ID). It is not encrypted.
	var id0 []byte
	if idArr := d.GetArray(d.trailer["ID"]); len(idArr) > 0 {
		if s, ok := d.Resolve(idArr[0]).(String); ok {
			id0 = []byte(s)
		}
	}

	oStr, _ := d.Resolve(encDict["O"]).(String)
	uStr, _ := d.Resolve(encDict["U"]).(String)
	pInt, _ := d.GetInt(encDict["P"])
	o := []byte(oStr)
	u := []byte(uStr)

	switch r {
	case 2, 3, 4:
		return d.setupRC4orAES(encDict, v, r, o, u, int32(pInt), id0)
	case 5, 6:
		return d.setupAESV3(encDict, v, r, o, u)
	default:
		return nil, fmt.Errorf("pdf: unsupported /Encrypt R=%d: %w", r, ErrEncrypted)
	}
}

// setupRC4orAES handles R2/R3 (RC4) and R4 (RC4 or AESV2 via crypt filters).
func (d *Document) setupRC4orAES(encDict Dict, v, r int, o, u []byte, p int32, id0 []byte) (*encrypter, error) {
	keyLen := 5 // R2 default: 40 bits
	if r >= 3 {
		if l, ok := d.GetInt(encDict["Length"]); ok && l > 0 {
			keyLen = l / 8
		}
	}
	if keyLen < 5 || keyLen > 16 {
		keyLen = 5
	}

	encryptMetadata := true
	if v, ok := boolValue(d.Resolve(encDict["EncryptMetadata"])); ok {
		encryptMetadata = v
	}

	method := cryptRC4
	if r >= 4 {
		m, err := d.streamCryptMethod(encDict)
		if err != nil {
			return nil, err
		}
		method = m
		// AESV2 fixes the key length at 16 bytes regardless of /Length.
		if method == cryptAESV2 {
			keyLen = 16
		}
	}

	key := computeKeyR234(o, p, id0, r, keyLen, encryptMetadata)

	// Algorithm 6: verify the key reproduces /U.
	if !verifyUserKeyR234(key, u, r, id0) {
		return nil, ErrEncryptedNeedsPassword
	}

	return &encrypter{key: key, method: method, v: v, r: r}, nil
}

// streamCryptMethod resolves the per-stream crypt filter for V4 (R4): it reads
// /StmF, looks it up in /CF, and maps /CFM to a method. /Crypt-filter
// overrides on individual streams are not supported.
func (d *Document) streamCryptMethod(encDict Dict) (cryptMethod, error) {
	stmF, _ := d.GetName(encDict["StmF"])
	if stmF == "" || stmF == "Identity" {
		// No default stream filter named, or explicitly Identity. Most R4 files
		// name a real filter; if not, assume RC4 for safety is wrong, so treat
		// Identity as no encryption for streams.
		if stmF == "Identity" {
			return cryptNone, nil
		}
		return cryptRC4, nil
	}
	cf := d.GetDict(encDict["CF"])
	if cf == nil {
		return cryptNone, fmt.Errorf("pdf: /CF missing for crypt filter %q: %w", stmF, ErrEncrypted)
	}
	fd := d.GetDict(cf[stmF])
	if fd == nil {
		return cryptNone, fmt.Errorf("pdf: crypt filter %q not found in /CF: %w", stmF, ErrEncrypted)
	}
	cfm, _ := d.GetName(fd["CFM"])
	switch cfm {
	case "V2":
		return cryptRC4, nil
	case "AESV2":
		return cryptAESV2, nil
	case "AESV3":
		return cryptAESV3, nil
	case "Identity", "":
		return cryptNone, nil
	default:
		return cryptNone, fmt.Errorf("pdf: unsupported crypt filter method /%s: %w", cfm, ErrEncrypted)
	}
}

// computeKeyR234 implements Algorithm 2 with the empty user password.
func computeKeyR234(o []byte, p int32, id0 []byte, r, keyLen int, encryptMetadata bool) []byte {
	h := md5.New()
	// a) padded (empty) password.
	h.Write(passwordPad)
	// b) /O (first 32 bytes if longer; spec stores exactly 32).
	if len(o) >= 32 {
		h.Write(o[:32])
	} else {
		h.Write(o)
	}
	// c) /P as 4-byte little-endian signed int.
	var pb [4]byte
	binary.LittleEndian.PutUint32(pb[:], uint32(p))
	h.Write(pb[:])
	// d) first element of /ID.
	h.Write(id0)
	// e) R4+ with EncryptMetadata false: append 0xFFFFFFFF.
	if r >= 4 && !encryptMetadata {
		h.Write([]byte{0xff, 0xff, 0xff, 0xff})
	}
	sum := h.Sum(nil)

	// f) R3+: rehash the first keyLen bytes 50 times.
	if r >= 3 {
		for i := 0; i < 50; i++ {
			s := md5.Sum(sum[:keyLen])
			sum = s[:]
		}
	}
	key := make([]byte, keyLen)
	copy(key, sum[:keyLen])
	return key
}

// verifyUserKeyR234 implements Algorithm 6: recompute /U from the file key and
// compare against the stored value.
func verifyUserKeyR234(key, u []byte, r int, id0 []byte) bool {
	switch r {
	case 2:
		// Algorithm 4: RC4(key) over the padding string.
		want := rc4Crypt(key, passwordPad)
		return bytes.Equal(want, u[:min(len(u), 32)])
	default: // r 3,4
		// Algorithm 5: MD5(pad || id0), then RC4 with key, then 19 rounds with
		// XOR-modified keys. Compare the first 16 bytes.
		h := md5.New()
		h.Write(passwordPad)
		h.Write(id0)
		digest := h.Sum(nil) // 16 bytes
		out := rc4Crypt(key, digest)
		for i := 1; i <= 19; i++ {
			k := make([]byte, len(key))
			for j := range key {
				k[j] = key[j] ^ byte(i)
			}
			out = rc4Crypt(k, out)
		}
		if len(u) < 16 {
			return false
		}
		return bytes.Equal(out[:16], u[:16])
	}
}

// setupAESV3 handles V5/R6 (AES-256) authentication with the empty user
// password, per ISO 32000-2 Algorithm 2.A/2.B.
func (d *Document) setupAESV3(encDict Dict, v, r int, o, u []byte) (*encrypter, error) {
	ueStr, _ := d.Resolve(encDict["UE"]).(String)
	ue := []byte(ueStr)
	if len(u) < 48 || len(ue) < 32 {
		return nil, fmt.Errorf("pdf: malformed AESV3 /U or /UE: %w", ErrEncrypted)
	}
	// /U = 48 bytes: 32-byte hash || 8-byte validation salt || 8-byte key salt.
	hash := u[0:32]
	validationSalt := u[32:40]
	keySalt := u[40:48]

	pwd := []byte{} // empty user password

	// Validate: hash2B(pwd || validationSalt) must equal the stored hash.
	if !bytes.Equal(hash2B(pwd, validationSalt, nil, r), hash) {
		return nil, ErrEncryptedNeedsPassword
	}

	// Derive the intermediate key, then AES-256 (no padding, zero IV, CBC) the
	// /UE to recover the file encryption key.
	ik := hash2B(pwd, keySalt, nil, r)
	block, err := aes.NewCipher(ik)
	if err != nil {
		return nil, fmt.Errorf("pdf: AESV3 key derivation: %w", ErrEncrypted)
	}
	fileKey := make([]byte, 32)
	iv := make([]byte, 16) // zero IV
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(fileKey, ue[:32])

	return &encrypter{key: fileKey, method: cryptAESV3, v: v, r: r}, nil
}

// hash2B implements the password hashing used by R6 (ISO 32000-2 Algorithm
// 2.B). For R5 (the deprecated draft) it is a single SHA-256; R6 adds the
// iterated AES/SHA mixing rounds. udata is the 48-byte /U for owner-password
// validation; pass nil for user-password use.
func hash2B(password, salt, udata []byte, r int) []byte {
	h := sha256.New()
	h.Write(password)
	h.Write(salt)
	h.Write(udata)
	k := h.Sum(nil)
	if r < 6 {
		return k // R5: single SHA-256
	}

	for round := 0; ; round++ {
		// K1 = (password || K || udata) repeated 64 times.
		seq := make([]byte, 0, (len(password)+len(k)+len(udata))*64)
		base := make([]byte, 0, len(password)+len(k)+len(udata))
		base = append(base, password...)
		base = append(base, k...)
		base = append(base, udata...)
		for i := 0; i < 64; i++ {
			seq = append(seq, base...)
		}

		// E = AES-128-CBC(key=K[0:16], iv=K[16:32]) over K1, no padding.
		block, err := aes.NewCipher(k[0:16])
		if err != nil {
			return k
		}
		e := make([]byte, len(seq))
		cipher.NewCBCEncrypter(block, k[16:32]).CryptBlocks(e, seq)

		// mod = sum of first 16 bytes of E, mod 3, selects the digest.
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

		// Stop after at least 64 rounds once the last byte of E is <= round-32.
		if round >= 63 && int(e[len(e)-1]) <= round-32 {
			break
		}
	}
	return k[:32]
}

// --- per-object decryption ---

// decryptString returns the decrypted bytes for a string object belonging to
// (objNum, gen). It never panics; on a malformed/short input it returns the
// input unchanged so parsing can proceed.
func (e *encrypter) decryptString(num, gen int, in []byte) []byte {
	return e.decrypt(num, gen, in)
}

// decryptStream returns the decrypted raw bytes of a stream belonging to
// (objNum, gen). Filter decoding happens afterwards on the returned bytes.
func (e *encrypter) decryptStream(num, gen int, in []byte) []byte {
	return e.decrypt(num, gen, in)
}

func (e *encrypter) decrypt(num, gen int, in []byte) []byte {
	if e == nil || e.method == cryptNone || len(in) == 0 {
		return in
	}
	switch e.method {
	case cryptRC4:
		return rc4Crypt(e.objectKey(num, gen, false), in)
	case cryptAESV2:
		return aesCBCDecrypt(e.objectKey(num, gen, true), in)
	case cryptAESV3:
		return aesCBCDecrypt(e.key, in)
	default:
		return in
	}
}

// objectKey derives the per-object key for RC4 and AESV2 (Algorithm 1). aesSalt
// appends the "sAlT" extension required for AESV2.
func (e *encrypter) objectKey(num, gen int, aesSalt bool) []byte {
	h := md5.New()
	h.Write(e.key)
	var b [5]byte
	b[0] = byte(num)
	b[1] = byte(num >> 8)
	b[2] = byte(num >> 16)
	b[3] = byte(gen)
	b[4] = byte(gen >> 8)
	h.Write(b[:])
	if aesSalt {
		h.Write([]byte{0x73, 0x41, 0x6c, 0x54}) // "sAlT"
	}
	sum := h.Sum(nil)
	n := len(e.key) + 5
	if n > 16 {
		n = 16
	}
	return sum[:n]
}

func rc4Crypt(key, data []byte) []byte {
	c, err := rc4.NewCipher(key)
	if err != nil {
		return data
	}
	out := make([]byte, len(data))
	c.XORKeyStream(out, data)
	return out
}

// aesCBCDecrypt decrypts AES-CBC ciphertext whose first 16 bytes are the IV. It
// strips PKCS#7 padding. On any structural problem it returns the input bytes
// after the IV (or the input) rather than panicking.
func aesCBCDecrypt(key, in []byte) []byte {
	if len(in) < aes.BlockSize {
		return in
	}
	iv := in[:aes.BlockSize]
	ct := in[aes.BlockSize:]
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return ct
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return ct
	}
	out := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ct)
	return stripPKCS7(out)
}

func stripPKCS7(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	pad := int(b[len(b)-1])
	if pad <= 0 || pad > aes.BlockSize || pad > len(b) {
		return b // not valid padding; return as-is
	}
	for _, c := range b[len(b)-pad:] {
		if int(c) != pad {
			return b
		}
	}
	return b[:len(b)-pad]
}
