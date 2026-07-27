package heif

import "image"

// Registration with the standard image package. ISOBMFF magic is
// "<4-byte size>ftyp<brand>"; image.RegisterFormat's '?' wildcard covers the
// size field. One registration per HEVC-still brand, plus the structural
// mif1 brand — for mif1 the coded type is verified at decode time, so an
// AVIF file sniffed via mif1 returns ErrUnsupported rather than garbage.
func init() {
	for _, brand := range []string{
		"heic", "heix", "heim", "heis",
		"hevc", "hevx", "hevm", "hevs",
		"mif1",
	} {
		image.RegisterFormat("heif", "????ftyp"+brand, Decode, DecodeConfig)
	}
}
