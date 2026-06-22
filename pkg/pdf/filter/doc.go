// Package filter decodes PDF stream data. Streams may be encoded with one or
// more filters (FlateDecode, ASCIIHexDecode, ASCII85Decode, DCTDecode, …),
// optionally followed by a predictor; this package applies a filter chain in
// order to recover the raw bytes.
package filter
