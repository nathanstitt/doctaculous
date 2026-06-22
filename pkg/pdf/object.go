package pdf

import (
	"fmt"
	"strings"
)

// Object is any PDF object value. Concrete types are: Null, Boolean, Integer,
// Real, String, Name, Array, Dict, Stream, and Reference.
type Object interface {
	isObject()
}

// Null is the PDF null object.
type Null struct{}

// Boolean is a PDF boolean (true/false).
type Boolean bool

// Integer is a PDF integer.
type Integer int64

// Real is a PDF real (floating-point) number.
type Real float64

// String is a PDF string. The bytes are the decoded value (after resolving
// literal escapes or hex encoding); they are not necessarily valid UTF-8.
type String string

// Name is a PDF name object (e.g. /Type). The leading slash is not stored and
// name escapes (#xx) are already decoded.
type Name string

// Array is a PDF array.
type Array []Object

// Dict is a PDF dictionary. Keys are name objects without the leading slash.
type Dict map[Name]Object

// Reference is an indirect object reference (e.g. "12 0 R").
type Reference struct {
	Number     int // object number
	Generation int // generation number
}

// Stream is a PDF stream object: a dictionary plus raw (still-encoded) bytes.
// Use the filter package together with the dictionary's Filter entry to decode
// the contents.
type Stream struct {
	Dict Dict
	Raw  []byte
}

func (Null) isObject()      {}
func (Boolean) isObject()   {}
func (Integer) isObject()   {}
func (Real) isObject()      {}
func (String) isObject()    {}
func (Name) isObject()      {}
func (Array) isObject()     {}
func (Dict) isObject()      {}
func (Reference) isObject() {}
func (*Stream) isObject()   {}

func (r Reference) String() string {
	return fmt.Sprintf("%d %d R", r.Number, r.Generation)
}

// Number returns the float64 value of an Integer or Real, and reports whether
// the object was numeric.
func Number(o Object) (float64, bool) {
	switch v := o.(type) {
	case Integer:
		return float64(v), true
	case Real:
		return float64(v), true
	default:
		return 0, false
	}
}

// IntValue returns the int value of an Integer (or a Real truncated to int) and
// reports whether the object was numeric.
func IntValue(o Object) (int, bool) {
	switch v := o.(type) {
	case Integer:
		return int(v), true
	case Real:
		return int(v), true
	default:
		return 0, false
	}
}

// Debug returns a human-readable representation of an object. It is not the PDF
// serialization; it is intended for logging and test output.
func Debug(o Object) string {
	switch v := o.(type) {
	case nil:
		return "<nil>"
	case Null:
		return "null"
	case Boolean:
		if v {
			return "true"
		}
		return "false"
	case Integer:
		return fmt.Sprintf("%d", int64(v))
	case Real:
		return fmt.Sprintf("%g", float64(v))
	case String:
		return fmt.Sprintf("(%s)", string(v))
	case Name:
		return "/" + string(v)
	case Reference:
		return v.String()
	case Array:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = Debug(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case Dict:
		return dictDebugString(v)
	case *Stream:
		return fmt.Sprintf("stream(%s, %d bytes)", dictDebugString(v.Dict), len(v.Raw))
	default:
		return fmt.Sprintf("%v", o)
	}
}

func dictDebugString(d Dict) string {
	parts := make([]string, 0, len(d))
	for k, val := range d {
		parts = append(parts, "/"+string(k)+" "+Debug(val))
	}
	return "<<" + strings.Join(parts, " ") + ">>"
}
