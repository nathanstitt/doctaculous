package pdf

import (
	"fmt"
)

// parsedObjStream is a decoded /Type /ObjStm: its contained objects in stored
// order. Instances are memoized per-Document in objStreamCache.
type parsedObjStream struct {
	objects []Object // objects in stored order
}

// parseObjectFromStream returns the object at index within the object stream
// identified by streamObjNum.
func (d *Document) parseObjectFromStream(streamObjNum, index int) (Object, error) {
	ps, err := d.loadObjStream(streamObjNum)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(ps.objects) {
		return nil, fmt.Errorf("pdf: object stream index %d out of range (%d objects)", index, len(ps.objects))
	}
	return ps.objects[index], nil
}

func (d *Document) loadObjStream(streamObjNum int) (*parsedObjStream, error) {
	d.cacheMu.Lock()
	if ps, ok := d.objStreamCache[streamObjNum]; ok {
		d.cacheMu.Unlock()
		return ps, nil
	}
	// Refuse to re-enter a stream already being loaded. This is not the same
	// recursion the parser's depth cap covers: the cycle here runs through the
	// DOCUMENT, not the token stream --
	//
	//	loadObjStream -> GetInt(/N) -> Resolve -> loadObject
	//	    -> parseObjectFromStream -> loadObjStream
	//
	// so a stream whose /N or /First is an indirect reference living inside that
	// same stream recurses until the stack is gone. Each level builds a fresh
	// parser, so no per-parser counter can see it. Found by fuzzing.
	if d.objStreamLoading[streamObjNum] {
		d.cacheMu.Unlock()
		return nil, fmt.Errorf("pdf: object stream %d refers to itself", streamObjNum)
	}
	if d.objStreamLoading == nil {
		d.objStreamLoading = map[int]bool{}
	}
	d.objStreamLoading[streamObjNum] = true
	d.cacheMu.Unlock()
	defer func() {
		d.cacheMu.Lock()
		delete(d.objStreamLoading, streamObjNum)
		d.cacheMu.Unlock()
	}()

	entry, ok := d.xref[streamObjNum]
	if !ok || entry.inStream {
		return nil, fmt.Errorf("pdf: object stream %d not found", streamObjNum)
	}
	obj, gen := d.parseObjectAtGen(entry.offset, streamObjNum)
	s, ok := obj.(*Stream)
	if !ok {
		return nil, fmt.Errorf("pdf: object %d is not a stream", streamObjNum)
	}
	// The ObjStm container is a normal encrypted stream: decrypt its raw bytes
	// before filter-decoding. The objects parsed out of the decoded data are
	// then plaintext and must NOT be decrypted individually (see parseXrefObject).
	if d.enc != nil {
		s = &Stream{Dict: s.Dict, Raw: d.enc.decryptStream(streamObjNum, gen, s.Raw)}
	}

	data, imgF, err := d.DecodedStream(s)
	if err != nil {
		return nil, fmt.Errorf("pdf: decoding object stream %d: %w", streamObjNum, err)
	}
	if imgF != "" {
		return nil, fmt.Errorf("pdf: object stream %d uses image filter", streamObjNum)
	}

	n, _ := d.GetInt(s.Dict["N"])
	first, _ := d.GetInt(s.Dict["First"])
	ps, err := parseObjStmData(data, n, first)
	if err != nil {
		return nil, err
	}

	d.cacheMu.Lock()
	d.objStreamCache[streamObjNum] = ps
	d.cacheMu.Unlock()
	return ps, nil
}

// parseObjStmData parses the header (N pairs of "objNum offset") and the N
// objects that follow, beginning at byte offset first.
func parseObjStmData(data []byte, n, first int) (*parsedObjStream, error) {
	if n <= 0 {
		return &parsedObjStream{}, nil
	}
	// N comes from the file and sizes both slices below, so it has to be checked
	// against what the data can actually hold BEFORE allocating. The header is N
	// pairs of integers separated by whitespace, so each entry needs at least
	// four bytes ("0 0 "); a stream claiming more than that is malformed
	// whatever follows.
	//
	// Found by fuzzing: a 504-byte input declaring /N 40000000020 asked for a
	// 320 GB slice. The header loop below would have rejected the very first
	// pair, but the process was already dead -- an allocation cannot be
	// validated after the fact.
	if maxEntries := len(data) / 4; n > maxEntries {
		return nil, fmt.Errorf("pdf: object stream declares %d objects, more than its %d bytes can hold", n, len(data))
	}
	// Parse the integer header.
	hdr := newObjParser(data)
	offsets := make([]int, n)
	for i := range n {
		t1, err := hdr.take() // object number (unused for indexing)
		if err != nil || t1.kind != tokInteger {
			return nil, fmt.Errorf("pdf: malformed object stream header")
		}
		t2, err := hdr.take() // byte offset relative to first
		if err != nil || t2.kind != tokInteger {
			return nil, fmt.Errorf("pdf: malformed object stream header")
		}
		offsets[i] = int(t2.num)
	}

	objects := make([]Object, n)
	for i := range n {
		start := first + offsets[i]
		// Bound each object to the next object's start (or end of data) so the
		// "N G R" lookahead in parseObject cannot cross into the following
		// object's bytes. Object stream offsets are ascending per the spec.
		end := len(data)
		if i+1 < n {
			if nextStart := first + offsets[i+1]; nextStart >= start && nextStart <= len(data) {
				end = nextStart
			}
		}
		if start < 0 || start > len(data) || start > end {
			objects[i] = Null{}
			continue
		}
		p := newObjParser(data[start:end])
		obj, err := p.parseObject()
		if err != nil {
			objects[i] = Null{}
			continue
		}
		objects[i] = obj
	}
	return &parsedObjStream{objects: objects}, nil
}
