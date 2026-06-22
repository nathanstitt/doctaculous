package pdf

import (
	"fmt"

	"github.com/nathanstitt/doctaculous/pkg/pdf/filter"
)

// Resolve follows indirect references until it reaches a direct object. Direct
// objects are returned unchanged. A missing object resolves to Null.
func (d *Document) Resolve(o Object) Object {
	for range 32 { // guard against reference cycles
		ref, ok := o.(Reference)
		if !ok {
			return o
		}
		o = d.loadObject(ref.Number)
	}
	return Null{}
}

// loadObject returns the top-level object with the given object number, parsing
// and caching it on first access.
func (d *Document) loadObject(num int) Object {
	d.cacheMu.Lock()
	if obj, ok := d.objCache[num]; ok {
		d.cacheMu.Unlock()
		return obj
	}
	d.cacheMu.Unlock()

	entry, ok := d.xref[num]
	if !ok {
		return Null{}
	}
	obj := d.parseXrefObject(num, entry)

	d.cacheMu.Lock()
	d.objCache[num] = obj
	d.cacheMu.Unlock()
	return obj
}

func (d *Document) parseXrefObject(num int, entry xrefEntry) Object {
	if entry.inStream {
		obj, err := d.parseObjectFromStream(entry.streamObj, entry.indexInStrm)
		if err != nil {
			return Null{}
		}
		return obj
	}
	return d.parseObjectAt(entry.offset, num)
}

// parseObjectAt parses an indirect object located at a byte offset. The form is
// "N G obj <object> endobj".
func (d *Document) parseObjectAt(offset int64, wantNum int) Object {
	if offset < 0 || int(offset) >= len(d.data) {
		return Null{}
	}
	p := newObjParser(d.data[offset:])
	// Expect "N G obj".
	t1, err := p.take()
	if err != nil || t1.kind != tokInteger {
		return Null{}
	}
	t2, err := p.take()
	if err != nil || t2.kind != tokInteger {
		return Null{}
	}
	t3, err := p.take()
	if err != nil || t3.kind != tokKeyword || string(t3.val) != "obj" {
		return Null{}
	}
	// If a specific object number was requested, verify the object at this offset
	// actually has that number. A stale/corrupt xref offset must resolve to Null
	// (so the caller can fall back) rather than silently returning a neighboring
	// object. wantNum < 0 means "don't check" (e.g. xref-stream objects).
	if wantNum >= 0 && int(t1.num) != wantNum {
		return Null{}
	}
	obj, err := p.parseObject()
	if err != nil {
		return Null{}
	}
	return obj
}

// GetDict resolves o to a Dict (or a Stream's dict), returning nil if it is not
// a dictionary.
func (d *Document) GetDict(o Object) Dict {
	switch v := d.Resolve(o).(type) {
	case Dict:
		return v
	case *Stream:
		return v.Dict
	default:
		return nil
	}
}

// GetName resolves o to a Name, returning ("", false) if it is not a name.
func (d *Document) GetName(o Object) (Name, bool) {
	n, ok := d.Resolve(o).(Name)
	return n, ok
}

// GetInt resolves o to an integer value.
func (d *Document) GetInt(o Object) (int, bool) {
	return IntValue(d.Resolve(o))
}

// GetArray resolves o to an Array, returning nil if it is not an array.
func (d *Document) GetArray(o Object) Array {
	if a, ok := d.Resolve(o).(Array); ok {
		return a
	}
	return nil
}

// GetStream resolves o to a *Stream, returning nil if it is not a stream.
func (d *Document) GetStream(o Object) *Stream {
	if s, ok := d.Resolve(o).(*Stream); ok {
		return s
	}
	return nil
}

// DecodedStream returns the fully decoded bytes of a stream, applying its filter
// chain. Image-only filters (e.g. DCTDecode) are left to the caller; in that
// case DecodedStream returns the raw bytes together with the remaining image
// filter name.
func (d *Document) DecodedStream(s *Stream) (data []byte, imageFilter string, err error) {
	if s == nil {
		return nil, "", fmt.Errorf("pdf: nil stream")
	}
	stages, imageFilter := d.filterStages(s.Dict)
	out, err := filter.Decode(s.Raw, stages)
	if err != nil {
		return nil, imageFilter, err
	}
	return out, imageFilter, nil
}

// filterStages builds the filter pipeline from a stream dict's Filter and
// DecodeParms. If the chain ends in an image-only filter, that filter is removed
// from the returned stages and named separately.
func (d *Document) filterStages(dict Dict) ([]filter.Stage, string) {
	filters := d.filterNames(dict)
	params := d.decodeParms(dict, len(filters))

	var stages []filter.Stage
	imageFilter := ""
	for i, name := range filters {
		if filter.IsImageFilter(string(name)) {
			imageFilter = string(name)
			break // leave this and any following filters to the image decoder
		}
		stages = append(stages, filter.Stage{Name: string(name), Params: params[i]})
	}
	return stages, imageFilter
}

func (d *Document) filterNames(dict Dict) []Name {
	f := d.Resolve(dict["Filter"])
	switch v := f.(type) {
	case Name:
		return []Name{v}
	case Array:
		out := make([]Name, 0, len(v))
		for _, e := range v {
			if n, ok := d.GetName(e); ok {
				out = append(out, n)
			}
		}
		return out
	default:
		return nil
	}
}

func (d *Document) decodeParms(dict Dict, n int) []filter.Params {
	out := make([]filter.Params, n)
	raw := d.Resolve(dict["DecodeParms"])
	if raw == nil {
		raw = d.Resolve(dict["DP"])
	}
	switch v := raw.(type) {
	case Dict:
		if n > 0 {
			out[0] = d.paramsFromDict(v)
		}
	case Array:
		for i := 0; i < n && i < len(v); i++ {
			if pd := d.GetDict(v[i]); pd != nil {
				out[i] = d.paramsFromDict(pd)
			}
		}
	}
	return out
}

func (d *Document) paramsFromDict(pd Dict) filter.Params {
	geti := func(key Name) int {
		if v, ok := d.GetInt(pd[key]); ok {
			return v
		}
		return 0
	}
	params := filter.Params{
		Predictor:        geti("Predictor"),
		Colors:           geti("Colors"),
		BitsPerComponent: geti("BitsPerComponent"),
		Columns:          geti("Columns"),
	}
	// EarlyChange must distinguish "explicit 0" from "unspecified", so only set
	// it when the key is actually present.
	if ec, ok := d.GetInt(pd["EarlyChange"]); ok {
		params.EarlyChange = &ec
	}
	params.CCITT = d.ccittParams(pd)
	return params
}

// boolValue returns the bool value of a resolved Boolean object, reporting
// false/false when o is not a Boolean (i.e. the key was absent).
func boolValue(o Object) (val, ok bool) {
	if b, isBool := o.(Boolean); isBool {
		return bool(b), true
	}
	return false, false
}

// ccittParams reads the CCITTFaxDecode /DecodeParms keys, applying PDF defaults.
// It returns nil when the dict carries no CCITT-specific keys, so non-CCITT
// stages keep their zero-value params.
func (d *Document) ccittParams(pd Dict) *filter.CCITTParams {
	_, hasK := d.GetInt(pd["K"])
	_, hasCols := d.GetInt(pd["Columns"])
	_, hasRows := d.GetInt(pd["Rows"])
	_, hasBI1 := boolValue(d.Resolve(pd["BlackIs1"]))
	_, hasEBA := boolValue(d.Resolve(pd["EncodedByteAlign"]))
	_, hasEOB := boolValue(d.Resolve(pd["EndOfBlock"]))
	if !hasK && !hasCols && !hasRows && !hasBI1 && !hasEBA && !hasEOB {
		return nil
	}
	p := &filter.CCITTParams{
		Columns:    1728,
		EndOfBlock: true,
	}
	if k, ok := d.GetInt(pd["K"]); ok {
		p.K = k
	}
	if c, ok := d.GetInt(pd["Columns"]); ok {
		p.Columns = c
	}
	if r, ok := d.GetInt(pd["Rows"]); ok {
		p.Rows = r
	}
	if v, ok := boolValue(d.Resolve(pd["BlackIs1"])); ok {
		p.BlackIs1 = v
	}
	if v, ok := boolValue(d.Resolve(pd["EncodedByteAlign"])); ok {
		p.EncodedByteAlign = v
	}
	if v, ok := boolValue(d.Resolve(pd["EndOfBlock"])); ok {
		p.EndOfBlock = v
	}
	return p
}
