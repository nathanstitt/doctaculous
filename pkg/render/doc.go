// Package render defines the device-independent graphics layer: the Device
// interface that the content interpreter drives, along with the geometry types
// (paths, matrices, paint) shared between the interpreter and concrete backends.
//
// Keeping paint operations abstract here is the seam that lets doctaculous target
// multiple backends (a raster bitmap today; SVG or others later) from a single
// content interpreter.
package render
