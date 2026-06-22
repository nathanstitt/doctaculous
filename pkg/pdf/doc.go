// Package pdf parses PDF file structure: the tokenizer, indirect objects,
// cross-reference tables and streams, object streams, and the page tree.
//
// It exposes a read-only [Document] that, once opened, is safe to share across
// goroutines without locking. Higher layers (filters, the content interpreter,
// and the rasterizer) build on the objects this package returns.
package pdf
