// Copyright 2026 肖其顿 (XIAO QI DUN)
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jbig2

// Result 解析结果
type Result int

const (
	// ResultSuccess 成功
	ResultSuccess Result = 0
	// ResultFailure 失败
	ResultFailure Result = 1
	// ResultEndReached 到达终点
	ResultEndReached Result = 2
	// ResultDecodeToBeContinued 继续解码
	ResultDecodeToBeContinued Result = 3
	// ResultPageCompleted 页面完成
	ResultPageCompleted Result = 4
)

// Document 文档上下文
type Document struct {
	stream        *BitStream
	globalContext *Document
	segmentList   []*Segment
	page          *Image
	pageInfoList  []*PageInfo
	segment       *Segment
	offset        uint32
	groupedOffset uint32
	groupedIndex  int
	groupedParsed bool
	inPage        bool
	bufSpecified  bool
	randomAccess  bool
	Grouped       bool
	OrgMode       int
}

// GetSegments 获取段列表
// 返回: []*Segment 段列表
func (d *Document) GetSegments() []*Segment {
	return d.segmentList
}

// GetGlobalContext 获取全局上下文
// 返回: *Document 全局上下文
func (d *Document) GetGlobalContext() *Document {
	return d.globalContext
}

// PageInfo 页面信息
type PageInfo struct {
	Width             uint32
	Height            uint32
	ResolutionX       uint32
	ResolutionY       uint32
	DefaultPixelValue bool
	IsStriped         bool
	MaxStripeSize     uint16
}

// NewDocument 创建文档对象
// 入参: data 数据流, globalData 全局数据, randomAccess 随机访问, littleEndian 小端序
// 返回: *Document 文档对象
func NewDocument(data []byte, globalData []byte, randomAccess bool, littleEndian bool) *Document {
	stream := NewBitStream(data, 0)
	stream.SetLittleEndian(littleEndian)
	doc := &Document{
		stream:       stream,
		randomAccess: randomAccess,
	}
	if len(globalData) > 0 {
		globalStream := NewBitStream(globalData, 0)
		globalStream.SetLittleEndian(littleEndian)
		doc.globalContext = &Document{
			stream: globalStream,
		}
	}
	return doc
}

// ParseSegmentHeader 解析段头
// 入参: segment 段对象
// 返回: Result 结果
func (d *Document) ParseSegmentHeader(segment *Segment) Result {
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		segment.Number = val
	}
	var flags byte
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	segment.Flags.Type = flags & 0x3F
	segment.Flags.PageAssociationSize = (flags & 0x40) != 0
	segment.Flags.DeferredNonRetain = (flags & 0x80) != 0
	cTemp := d.stream.GetCurByte()
	if (cTemp >> 5) == 7 {
		var count uint32
		if val, err := d.stream.ReadInteger(); err != nil {
			return ResultFailure
		} else {
			count = val
		}
		count &= 0x1FFFFFFF
		segment.ReferredToSegmentCount = int32(count)
		if segment.ReferredToSegmentCount > 1024 {
			return ResultFailure
		}
		retentionBits := segment.ReferredToSegmentCount + 1
		bytesToSkip := (retentionBits + 7) / 8
		d.stream.AddOffset(uint32(bytesToSkip))
	} else {
		if val, err := d.stream.Read1Byte(); err != nil {
			return ResultFailure
		} else {
			cTemp = val
		}
		segment.ReferredToSegmentCount = int32(cTemp >> 5)
	}
	cSSize := 1
	if segment.Number > 65536 {
		cSSize = 4
	} else if segment.Number > 256 {
		cSSize = 2
	}
	cPSize := 1
	if segment.Flags.PageAssociationSize {
		cPSize = 4
	}
	if segment.ReferredToSegmentCount > 0 {
		segment.ReferredToSegmentNumbers = make([]uint32, segment.ReferredToSegmentCount)
		for i := int32(0); i < segment.ReferredToSegmentCount; i++ {
			switch cSSize {
			case 1:
				if val, err := d.stream.Read1Byte(); err != nil {
					return ResultFailure
				} else {
					segment.ReferredToSegmentNumbers[i] = uint32(val)
				}
			case 2:
				if val, err := d.stream.ReadShortInteger(); err != nil {
					return ResultFailure
				} else {
					segment.ReferredToSegmentNumbers[i] = uint32(val)
				}
			case 4:
				if val, err := d.stream.ReadInteger(); err != nil {
					return ResultFailure
				} else {
					segment.ReferredToSegmentNumbers[i] = val
				}
			}
			if !d.randomAccess && segment.ReferredToSegmentNumbers[i] >= segment.Number {
				return ResultFailure
			}
		}
	}
	if cPSize == 1 {
		if val, err := d.stream.Read1Byte(); err != nil {
			return ResultFailure
		} else {
			segment.PageAssociation = uint32(val)
		}
	} else {
		if val, err := d.stream.ReadInteger(); err != nil {
			return ResultFailure
		} else {
			segment.PageAssociation = val
		}
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		segment.DataLength = val
	}
	segment.Key = d.stream.GetKey()
	segment.DataOffset = d.stream.GetOffset()
	segment.State = JBig2SegmentDataUnparsed
	return ResultSuccess
}

// FindSegmentByNumber 查找段
// 入参: number 段编号
// 返回: *Segment 段对象
func (d *Document) FindSegmentByNumber(number uint32) *Segment {
	if d.globalContext != nil {
		if seg := d.globalContext.FindSegmentByNumber(number); seg != nil {
			return seg
		}
	}
	for _, seg := range d.segmentList {
		if seg.Number == number {
			return seg
		}
	}
	return nil
}

// ParseSegmentData 解析段数据
// 入参: segment 段对象
// 返回: Result 结果
func (d *Document) ParseSegmentData(segment *Segment) Result {
	switch segment.Flags.Type {
	case 0:
		return d.parseSymbolDict(segment)
	case 4, 6, 7:
		if !d.inPage {
			return ResultFailure
		}
		return d.parseTextRegion(segment)
	case 16:
		return d.parsePatternDict(segment)
	case 20, 22, 23:
		if !d.inPage {
			return ResultFailure
		}
		return d.parseHalftoneRegion(segment)
	case 36, 38, 39:
		if !d.inPage {
			return ResultFailure
		}
		return d.parseGenericRegion(segment)
	case 40, 42, 43:
		if !d.inPage {
			return ResultFailure
		}
		return d.parseGenericRefinementRegion(segment)
	case 48:
		return d.parsePageInfo(segment)
	case 49:
		d.inPage = false
		return ResultPageCompleted
	case 50:
		d.stream.AddOffset(segment.DataLength)
	case 51:
		return ResultEndReached
	case 52:
		d.stream.AddOffset(segment.DataLength)
	case 53:
		return d.parseTable(segment)
	case 62:
		d.stream.AddOffset(segment.DataLength)
	default:
		d.stream.AddOffset(segment.DataLength)
	}
	return ResultSuccess
}

// DecodeSequential 顺序解码
// 返回: Result 结果
func (d *Document) DecodeSequential() Result {
	if d.stream.GetByteLeft() <= 0 {
		return ResultEndReached
	}
	if d.Grouped {
		return d.decodeGrouped()
	}
	for d.stream.GetByteLeft() > 0 {
		if d.segment == nil {
			d.segment = NewSegment()
			ret := d.ParseSegmentHeader(d.segment)
			if ret != ResultSuccess {
				d.segment = nil
				break
			}
			d.offset = d.stream.GetOffset()
		}
		ret := d.ParseSegmentData(d.segment)
		if ret == ResultEndReached {
			d.segmentList = append(d.segmentList, d.segment)
			d.segment = nil
			return ResultSuccess
		}
		if ret == ResultPageCompleted {
			d.segmentList = append(d.segmentList, d.segment)
			d.segment = nil
			return ResultPageCompleted
		}
		if ret != ResultSuccess {
			d.segment = nil
			return ret
		}
		if d.segment.DataLength != 0xFFFFFFFF {
			newOffset := int64(d.offset) + int64(d.segment.DataLength)
			if uint32(newOffset) <= d.stream.GetLength() {
				d.stream.SetOffset(uint32(newOffset))
			} else {
				d.stream.SetOffset(d.stream.GetLength())
			}
		} else {
			d.stream.AddOffset(4)
		}
		d.segmentList = append(d.segmentList, d.segment)
		d.segment = nil
	}
	return ResultSuccess
}

// decodeGrouped 分组解码
// 返回: Result 结果
func (d *Document) decodeGrouped() Result {
	if !d.groupedParsed {
		for d.stream.GetByteLeft() > 0 {
			seg := NewSegment()
			ret := d.ParseSegmentHeader(seg)
			if ret != ResultSuccess {
				break
			}
			d.segmentList = append(d.segmentList, seg)
			if seg.Flags.Type == 51 {
				break
			}
		}
		d.groupedOffset = d.stream.GetOffset()
		d.groupedParsed = true
	}
	for d.groupedIndex < len(d.segmentList) {
		seg := d.segmentList[d.groupedIndex]
		if seg.Flags.Type == 51 {
			d.groupedIndex++
			return ResultEndReached
		}
		if seg.DataLength == 0 && seg.Flags.Type != 49 {
			d.groupedIndex++
			continue
		}
		d.stream.SetOffset(d.groupedOffset)
		d.segment = seg
		d.offset = d.groupedOffset
		ret := d.ParseSegmentData(seg)
		if ret == ResultFailure {
			return ResultFailure
		}
		if seg.DataLength != 0xFFFFFFFF {
			d.groupedOffset += seg.DataLength
			d.stream.SetOffset(d.groupedOffset)
		}
		d.segment = nil
		d.groupedIndex++
		if ret == ResultPageCompleted || ret == ResultEndReached {
			return ret
		}
	}
	return ResultEndReached
}

// parseSymbolDict 解析符号字典段
// 入参: segment 段对象
// 返回: Result 解析结果
func (d *Document) parseSymbolDict(segment *Segment) Result {
	var flags uint16
	if val, err := d.stream.ReadShortInteger(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	sdd := NewSDDProc()
	sdd.SDHUFF = (flags & 0x0001) != 0
	sdd.SDREFAGG = ((flags >> 1) & 0x0001) != 0
	sdd.SDMMR = ((flags >> 10) & 0x01) != 0
	if !sdd.SDHUFF {
		sdd.SDTEMPLATE = uint8((flags >> 2) & 0x0003)
		sdd.SDRTEMPLATE = ((flags >> 4) & 0x0001) != 0
		dwTemp := 2
		if sdd.SDTEMPLATE == 0 {
			dwTemp = 8
		}
		for i := 0; i < dwTemp; i++ {
			if val, err := d.stream.Read1Byte(); err != nil {
				return ResultFailure
			} else {
				sdd.SDAT[i] = int8(val)
			}
		}
	}
	if sdd.SDREFAGG && !sdd.SDRTEMPLATE {
		for i := 0; i < 4; i++ {
			if val, err := d.stream.Read1Byte(); err != nil {
				return ResultFailure
			} else {
				sdd.SDRAT[i] = int8(val)
			}
		}
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		sdd.SDNUMEXSYMS = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		sdd.SDNUMNEWSYMS = val
	}
	var inputSymbols []*Image
	if segment.ReferredToSegmentCount > 0 {
		for _, refNum := range segment.ReferredToSegmentNumbers {
			seg := d.FindSegmentByNumber(refNum)
			if seg == nil {
				return ResultFailure
			}
			if seg.Flags.Type == 0 && seg.SymbolDict != nil {
				inputSymbols = append(inputSymbols, seg.SymbolDict.Images...)
			}
		}
	}
	sdd.SDINSYMS = inputSymbols
	sdd.SDNUMINSYMS = uint32(len(inputSymbols))
	if sdd.SDHUFF {
		cSDHUFFDH := (flags >> 2) & 0x0003
		cSDHUFFDW := (flags >> 4) & 0x0003
		cSDHUFFBMSIZE := (flags >> 6) & 0x0001
		cSDHUFFAGGINST := (flags >> 7) & 0x0001
		if cSDHUFFDH == 2 || cSDHUFFDW == 2 {
			return ResultFailure
		}
		var tableSegments []*Segment
		for _, refNum := range segment.ReferredToSegmentNumbers {
			seg := d.FindSegmentByNumber(refNum)
			if seg != nil && seg.Flags.Type == 53 {
				tableSegments = append(tableSegments, seg)
			}
		}
		tableIdx := 0
		if cSDHUFFDH == 0 {
			sdd.SDHUFFDH = NewStandardTable(4)
		} else if cSDHUFFDH == 1 {
			sdd.SDHUFFDH = NewStandardTable(5)
		} else {
			if tableIdx < len(tableSegments) {
				sdd.SDHUFFDH = tableSegments[tableIdx].HuffmanTable
				tableIdx++
			} else {
				return ResultFailure
			}
		}
		if cSDHUFFDW == 0 {
			sdd.SDHUFFDW = NewStandardTable(2)
		} else if cSDHUFFDW == 1 {
			sdd.SDHUFFDW = NewStandardTable(3)
		} else {
			if tableIdx < len(tableSegments) {
				sdd.SDHUFFDW = tableSegments[tableIdx].HuffmanTable
				tableIdx++
			} else {
				return ResultFailure
			}
		}
		if cSDHUFFBMSIZE == 0 {
			sdd.SDHUFFBMSIZE = NewStandardTable(1)
		} else {
			if tableIdx < len(tableSegments) {
				sdd.SDHUFFBMSIZE = tableSegments[tableIdx].HuffmanTable
				tableIdx++
			} else {
				return ResultFailure
			}
		}
		if sdd.SDREFAGG {
			if cSDHUFFAGGINST == 0 {
				sdd.SDHUFFAGGINST = NewStandardTable(1)
			} else {
				if tableIdx < len(tableSegments) {
					sdd.SDHUFFAGGINST = tableSegments[tableIdx].HuffmanTable
					tableIdx++
				} else {
					return ResultFailure
				}
			}
		}
	}
	gbContextSize := 0
	grContextSize := 0
	if !sdd.SDHUFF {
		if sdd.SDTEMPLATE == 0 {
			gbContextSize = 65536
		} else {
			gbContextSize = 8192
		}
		if sdd.SDREFAGG {
			if sdd.SDRTEMPLATE {
				grContextSize = 1024
			} else {
				grContextSize = 8192
			}
		}
	}
	var gbContexts, grContexts []ArithCtx
	retainContexts := (flags & 0x0100) != 0
	if retainContexts && len(segment.ReferredToSegmentNumbers) > 0 {
		refSeg := d.FindSegmentByNumber(segment.ReferredToSegmentNumbers[0])
		if refSeg != nil {
			if len(refSeg.GBContexts) == gbContextSize {
				gbContexts = make([]ArithCtx, gbContextSize)
				copy(gbContexts, refSeg.GBContexts)
			}
			if len(refSeg.GRContexts) == grContextSize {
				grContexts = make([]ArithCtx, grContextSize)
				copy(grContexts, refSeg.GRContexts)
			}
		}
	}
	if gbContexts == nil {
		gbContexts = make([]ArithCtx, gbContextSize)
	}
	if grContexts == nil {
		grContexts = make([]ArithCtx, grContextSize)
	}
	var err error
	if sdd.SDHUFF {
		segment.SymbolDict, err = sdd.DecodeHuffman(d.stream, gbContexts, grContexts)
		d.stream.AlignByte()
	} else {
		arithDecoder := NewArithDecoder(d.stream)
		segment.SymbolDict, err = sdd.DecodeArith(arithDecoder, gbContexts, grContexts)
		d.stream.AlignByte()
		d.stream.AddOffset(2)
	}
	if err != nil {
		return ResultFailure
	}
	segment.ResultType = JBig2SymbolDictPointer
	return ResultSuccess
}

// ParseRegionInfo 解析区域信息
// 入参: ri 区域信息
// 返回: Result 结果
func (d *Document) ParseRegionInfo(ri *RegionInfo) Result {
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		ri.Width = int32(val)
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		ri.Height = int32(val)
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		ri.X = int32(val)
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		ri.Y = int32(val)
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		ri.Flags = val
	}
	return ResultSuccess
}

// composeOpFromRegionFlags 从区域标志获取组合操作
// 入参: flags 区域标志
// 返回: ComposeOp 组合操作
func composeOpFromRegionFlags(flags uint8) ComposeOp {
	if (flags & 0x07) == 4 {
		return ComposeReplace
	}
	return ComposeOp(flags & 0x03)
}

// expandPageForRegion 根据区域扩展页面
// 入参: ri 区域信息
func (d *Document) expandPageForRegion(ri *RegionInfo) {
	if d.bufSpecified || d.page == nil || len(d.pageInfoList) == 0 {
		return
	}
	pi := d.pageInfoList[len(d.pageInfoList)-1]
	if !pi.IsStriped {
		return
	}
	newHeight := int64(ri.Y) + int64(ri.Height)
	if newHeight <= int64(d.page.Height()) || newHeight > int64(1<<31-1) {
		return
	}
	d.page.Expand(int32(newHeight), pi.DefaultPixelValue)
}

// GetHuffmanTable 获取霍夫曼表
// 入参: idx 索引
// 返回: *HuffmanTable 霍夫曼表
func (d *Document) GetHuffmanTable(idx int) *HuffmanTable {
	return NewStandardTable(idx)
}

// DecodeSymbolIDHuffmanTable 解码符号ID霍夫曼表
// 入参: SBNUMSYMS 符号数
// 返回: []HuffmanCode 霍夫曼编码切片
func (d *Document) DecodeSymbolIDHuffmanTable(SBNUMSYMS uint32) []HuffmanCode {
	kRunCodesSize := 35
	huffmanCodes := make([]HuffmanCode, kRunCodesSize)
	for i := 0; i < kRunCodesSize; i++ {
		val, err := d.stream.ReadNBits(4)
		if err != nil {
			return nil
		}
		huffmanCodes[i].Codelen = int32(val)
	}
	if err := HuffmanAssignCode(huffmanCodes); err != nil {
		return nil
	}
	SBSYMCODES := make([]HuffmanCode, SBNUMSYMS)
	i := int32(0)
	loopSyms := 0
	for i < int32(SBNUMSYMS) {
		loopSyms++
		if loopSyms > int(SBNUMSYMS)*10 {
			return nil
		}
		var j int
		var nSafeVal int32
		nBits := 0
		loopInner := 0
		for {
			loopInner++
			if loopInner > 1000 {
				return nil
			}
			bit, err := d.stream.Read1Bit()
			if err != nil {
				return nil
			}
			nSafeVal = (nSafeVal << 1) | int32(bit)
			nBits++
			for j = 0; j < kRunCodesSize; j++ {
				if int32(nBits) == huffmanCodes[j].Codelen && nSafeVal == huffmanCodes[j].Code {
					break
				}
			}
			if j < kRunCodesSize {
				break
			}
		}
		runcode := int32(j)
		var run int32
		if runcode < 32 {
			SBSYMCODES[i].Codelen = runcode
			run = 0
		} else if runcode == 32 {
			val, err := d.stream.ReadNBits(2)
			if err != nil {
				return nil
			}
			run = int32(val) + 3
		} else if runcode == 33 {
			val, err := d.stream.ReadNBits(3)
			if err != nil {
				return nil
			}
			run = int32(val) + 3
		} else if runcode == 34 {
			val, err := d.stream.ReadNBits(7)
			if err != nil {
				return nil
			}
			run = int32(val) + 11
		}
		if run > 0 {
			if i+run > int32(SBNUMSYMS) {
				return nil
			}
			for k := int32(0); k < run; k++ {
				if runcode == 32 && i > 0 {
					SBSYMCODES[i+k].Codelen = SBSYMCODES[i-1].Codelen
				} else {
					SBSYMCODES[i+k].Codelen = 0
				}
			}
			i += run
		} else {
			i++
		}
	}
	if err := HuffmanAssignCode(SBSYMCODES); err != nil {
		return nil
	}
	return SBSYMCODES
}

// parseTextRegion 解析文本区域段
// 入参: segment 段对象
// 返回: Result 解析结果
func (d *Document) parseTextRegion(segment *Segment) Result {
	var ri RegionInfo
	if d.ParseRegionInfo(&ri) != ResultSuccess {
		return ResultFailure
	}
	var flags uint16
	if val, err := d.stream.ReadShortInteger(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	pTRD := NewTRDProc()
	pTRD.SBW = uint32(ri.Width)
	pTRD.SBH = uint32(ri.Height)
	pTRD.SBHUFF = (flags & 0x0001) != 0
	pTRD.SBREFINE = ((flags >> 1) & 0x0001) != 0
	dwTemp := (flags >> 2) & 0x0003
	pTRD.SBSTRIPS = 1 << dwTemp
	pTRD.REFCORNER = JBig2Corner((flags >> 4) & 0x0003)
	pTRD.TRANSPOSED = ((flags >> 6) & 0x0001) != 0
	pTRD.SBCOMBOP = ComposeOp((flags >> 7) & 0x0003)
	pTRD.SBDEFPIXEL = ((flags >> 9) & 0x0001) != 0
	pTRD.SBDSOFFSET = int8((flags >> 10) & 0x001F)
	if pTRD.SBDSOFFSET >= 0x10 {
		pTRD.SBDSOFFSET = pTRD.SBDSOFFSET - 0x20
	}
	pTRD.SBRTEMPLATE = ((flags >> 15) & 0x0001) != 0
	var huffFlags uint16
	if pTRD.SBHUFF {
		if val, err := d.stream.ReadShortInteger(); err != nil {
			return ResultFailure
		} else {
			huffFlags = val
		}
	}
	if pTRD.SBREFINE && !pTRD.SBRTEMPLATE {
		for i := 0; i < 4; i++ {
			if val, err := d.stream.Read1Byte(); err != nil {
				return ResultFailure
			} else {
				pTRD.SBRAT[i] = int8(val)
			}
		}
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pTRD.SBNUMINSTANCES = val
	}
	if segment.ReferredToSegmentCount > 0 {
		for _, refNum := range segment.ReferredToSegmentNumbers {
			if d.FindSegmentByNumber(refNum) == nil {
				return ResultFailure
			}
		}
	}
	dwNumSyms := uint32(0)
	for _, refNum := range segment.ReferredToSegmentNumbers {
		seg := d.FindSegmentByNumber(refNum)
		if seg != nil && seg.Flags.Type == 0 && seg.SymbolDict != nil {
			dwNumSyms += uint32(seg.SymbolDict.NumImages())
		}
	}
	pTRD.SBNUMSYMS = dwNumSyms
	SBSYMS := make([]*Image, pTRD.SBNUMSYMS)
	dwNumSyms = 0
	for _, refNum := range segment.ReferredToSegmentNumbers {
		seg := d.FindSegmentByNumber(refNum)
		if seg != nil && seg.Flags.Type == 0 && seg.SymbolDict != nil {
			dict := seg.SymbolDict
			for j := 0; j < dict.NumImages(); j++ {
				SBSYMS[dwNumSyms+uint32(j)] = dict.GetImage(j)
			}
			dwNumSyms += uint32(dict.NumImages())
		}
	}
	pTRD.SBSYMS = SBSYMS
	if pTRD.SBHUFF {
		if encodedTable := d.DecodeSymbolIDHuffmanTable(pTRD.SBNUMSYMS); encodedTable != nil {
			d.stream.AlignByte()
			pTRD.SBSYMCODES = encodedTable
		} else {
			return ResultFailure
		}
	} else {
		dwTemp = 0
		for (uint32(1) << dwTemp) < pTRD.SBNUMSYMS {
			dwTemp++
		}
		pTRD.SBSYMCODELEN = uint8(dwTemp)
	}
	if pTRD.SBHUFF {
		cSBHUFFFS := huffFlags & 0x0003
		cSBHUFFDS := (huffFlags >> 2) & 0x0003
		cSBHUFFDT := (huffFlags >> 4) & 0x0003
		cSBHUFFRDW := (huffFlags >> 6) & 0x0003
		cSBHUFFRDH := (huffFlags >> 8) & 0x0003
		cSBHUFFRDX := (huffFlags >> 10) & 0x0003
		cSBHUFFRDY := (huffFlags >> 12) & 0x0003
		cSBHUFFRSIZE := (huffFlags >> 14) & 0x0001
		if !pTRD.SBREFINE {
			cSBHUFFRDW = 0
			cSBHUFFRDH = 0
			cSBHUFFRDX = 0
			cSBHUFFRDY = 0
			cSBHUFFRSIZE = 0
		}
		if cSBHUFFFS == 2 || cSBHUFFRDW == 2 || cSBHUFFRDH == 2 || cSBHUFFRDX == 2 || cSBHUFFRDY == 2 {
			return ResultFailure
		}
		tableIdx := 0
		var tableSegments []*Segment
		for _, refNum := range segment.ReferredToSegmentNumbers {
			seg := d.FindSegmentByNumber(refNum)
			if seg != nil && seg.Flags.Type == 53 {
				tableSegments = append(tableSegments, seg)
			}
		}
		getUserTable := func() *HuffmanTable {
			if tableIdx < len(tableSegments) {
				t := tableSegments[tableIdx].HuffmanTable
				tableIdx++
				return t
			}
			return nil
		}
		if cSBHUFFFS == 0 {
			pTRD.SBHUFFFS = d.GetHuffmanTable(6)
		} else if cSBHUFFFS == 1 {
			pTRD.SBHUFFFS = d.GetHuffmanTable(7)
		} else {
			pTRD.SBHUFFFS = getUserTable()
		}
		if cSBHUFFDS == 0 {
			pTRD.SBHUFFDS = d.GetHuffmanTable(8)
		} else if cSBHUFFDS == 1 {
			pTRD.SBHUFFDS = d.GetHuffmanTable(9)
		} else if cSBHUFFDS == 2 {
			pTRD.SBHUFFDS = d.GetHuffmanTable(10)
		} else {
			pTRD.SBHUFFDS = getUserTable()
		}
		if cSBHUFFDT == 0 {
			pTRD.SBHUFFDT = d.GetHuffmanTable(11)
		} else if cSBHUFFDT == 1 {
			pTRD.SBHUFFDT = d.GetHuffmanTable(12)
		} else if cSBHUFFDT == 2 {
			pTRD.SBHUFFDT = d.GetHuffmanTable(13)
		} else {
			pTRD.SBHUFFDT = getUserTable()
		}
		if cSBHUFFRDW == 0 {
			pTRD.SBHUFFRDW = d.GetHuffmanTable(14)
		} else if cSBHUFFRDW == 1 {
			pTRD.SBHUFFRDW = d.GetHuffmanTable(15)
		} else {
			pTRD.SBHUFFRDW = getUserTable()
		}
		if cSBHUFFRDH == 0 {
			pTRD.SBHUFFRDH = d.GetHuffmanTable(14)
		} else if cSBHUFFRDH == 1 {
			pTRD.SBHUFFRDH = d.GetHuffmanTable(15)
		} else {
			pTRD.SBHUFFRDH = getUserTable()
		}
		if cSBHUFFRDX == 0 {
			pTRD.SBHUFFRDX = d.GetHuffmanTable(14)
		} else if cSBHUFFRDX == 1 {
			pTRD.SBHUFFRDX = d.GetHuffmanTable(15)
		} else {
			pTRD.SBHUFFRDX = getUserTable()
		}
		if cSBHUFFRDY == 0 {
			pTRD.SBHUFFRDY = d.GetHuffmanTable(14)
		} else if cSBHUFFRDY == 1 {
			pTRD.SBHUFFRDY = d.GetHuffmanTable(15)
		} else {
			pTRD.SBHUFFRDY = getUserTable()
		}
		if cSBHUFFRSIZE == 0 {
			pTRD.SBHUFFRSIZE = d.GetHuffmanTable(1)
		} else {
			pTRD.SBHUFFRSIZE = getUserTable()
		}
	}
	var grContexts []ArithCtx
	if pTRD.SBREFINE {
		size := 8192
		if pTRD.SBRTEMPLATE {
			size = 1024
		}
		grContexts = make([]ArithCtx, size)
	}
	segment.ResultType = JBig2ImagePointer
	var err error
	if pTRD.SBHUFF {
		var img *Image
		img, err = pTRD.DecodeHuffman(d.stream, grContexts)
		if err == nil {
			segment.Image = img
			d.stream.AlignByte()
		}
	} else {
		arithDecoder := NewArithDecoder(d.stream)
		var img *Image
		img, err = pTRD.DecodeArith(arithDecoder, grContexts, nil)
		if err == nil {
			segment.Image = img
			d.stream.AlignByte()
			d.stream.AddOffset(2)
		}
	}
	if err != nil || segment.Image == nil {
		return ResultFailure
	}
	if segment.Flags.Type != 4 {
		d.expandPageForRegion(&ri)
		d.page.ComposeFrom(ri.X, ri.Y, segment.Image, composeOpFromRegionFlags(ri.Flags))
		segment.Image = nil
	}
	return ResultSuccess
}

// parsePatternDict 解析模式字典段
// 入参: segment 段对象
// 返回: Result 解析结果
func (d *Document) parsePatternDict(segment *Segment) Result {
	var flags byte
	pPDD := NewPDDProc()
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		pPDD.HDPW = val
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		pPDD.HDPH = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pPDD.GRAYMAX = val
	}
	if pPDD.GRAYMAX > JBig2MaxPatternIndex {
		return ResultFailure
	}
	pPDD.HDMMR = (flags & 0x01) != 0
	pPDD.HDTEMPLATE = (flags >> 1) & 0x03
	segment.ResultType = JBig2PatternDictPointer
	var err error
	if pPDD.HDMMR {
		segment.PatternDict, err = pPDD.DecodeMMR(d.stream)
		if err != nil {
			return ResultFailure
		}
		d.stream.AlignByte()
	} else {
		size := 1024
		if pPDD.HDTEMPLATE == 0 {
			size = 65536
		} else if pPDD.HDTEMPLATE == 1 {
			size = 8192
		}
		gbContexts := make([]ArithCtx, size)
		arithDecoder := NewArithDecoder(d.stream)
		segment.PatternDict, err = pPDD.DecodeArith(arithDecoder, gbContexts)
		if err != nil {
			return ResultFailure
		}
		d.stream.AlignByte()
		d.stream.AddOffset(2)
	}
	return ResultSuccess
}

// parseHalftoneRegion 解析半色调区域段
// 入参: segment 段对象
// 返回: Result 解析结果
func (d *Document) parseHalftoneRegion(segment *Segment) Result {
	var ri RegionInfo
	var flags byte
	pHRD := NewHTRDProc()
	if d.ParseRegionInfo(&ri) != ResultSuccess {
		return ResultFailure
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HGW = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HGH = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HGX = int32(val)
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HGY = int32(val)
	}
	if val, err := d.stream.ReadShortInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HRX = uint16(val)
	}
	if val, err := d.stream.ReadShortInteger(); err != nil {
		return ResultFailure
	} else {
		pHRD.HRY = uint16(val)
	}
	pHRD.HBW = uint32(ri.Width)
	pHRD.HBH = uint32(ri.Height)
	pHRD.HMMR = (flags & 0x01) != 0
	pHRD.HTEMPLATE = (flags >> 1) & 0x03
	pHRD.HENABLESKIP = ((flags >> 3) & 0x01) != 0
	pHRD.HCOMBOP = ComposeOp((flags >> 4) & 0x07)
	pHRD.HDEFPIXEL = ((flags >> 7) & 0x01) != 0
	if segment.ReferredToSegmentCount != 1 {
		return ResultFailure
	}
	seg := d.FindSegmentByNumber(segment.ReferredToSegmentNumbers[0])
	if seg == nil || seg.Flags.Type != 16 || seg.PatternDict == nil {
		return ResultFailure
	}
	pPatternDict := seg.PatternDict
	if pPatternDict.NUMPATS == 0 {
		return ResultFailure
	}
	pHRD.HNUMPATS = pPatternDict.NUMPATS
	pHRD.HPATS = pPatternDict.HDPATS
	pHRD.HPW = uint8(pPatternDict.HDPATS[0].Width())
	pHRD.HPH = uint8(pPatternDict.HDPATS[0].Height())
	segment.ResultType = JBig2ImagePointer
	var err error
	if pHRD.HMMR {
		d.stream.AlignByte()
		segment.Image, err = pHRD.DecodeMMR(d.stream)
		if err != nil {
			return ResultFailure
		}
		d.stream.AlignByte()
	} else {
		size := GetHuffContextSize(pHRD.HTEMPLATE)
		gbContexts := make([]ArithCtx, size)
		arithDecoder := NewArithDecoder(d.stream)
		segment.Image, err = pHRD.DecodeArith(arithDecoder, gbContexts)
		if err != nil {
			return ResultFailure
		}
		d.stream.AlignByte()
		d.stream.AddOffset(2)
	}
	if segment.Flags.Type != 20 {
		d.expandPageForRegion(&ri)
		d.page.ComposeFrom(ri.X, ri.Y, segment.Image, composeOpFromRegionFlags(ri.Flags))
		segment.Image = nil
	}
	return ResultSuccess
}

// parseGenericRegion 解析通用区域段
// 入参: segment 段对象
// 返回: Result 解析结果
func (d *Document) parseGenericRegion(segment *Segment) Result {
	var ri RegionInfo
	var flags byte
	if d.ParseRegionInfo(&ri) != ResultSuccess {
		return ResultFailure
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	pGRD := NewGRDProc()
	pGRD.GBW = uint32(ri.Width)
	pGRD.GBH = uint32(ri.Height)
	pGRD.MMR = (flags & 0x01) != 0
	pGRD.GBTEMPLATE = (flags >> 1) & 0x03
	pGRD.TPGDON = ((flags >> 3) & 0x01) != 0
	if !pGRD.MMR {
		if pGRD.GBTEMPLATE == 0 {
			for i := 0; i < 8; i++ {
				if val, err := d.stream.Read1Byte(); err != nil {
					return ResultFailure
				} else {
					pGRD.GBAT[i] = int8(val)
				}
			}
		} else {
			for i := 0; i < 2; i++ {
				if val, err := d.stream.Read1Byte(); err != nil {
					return ResultFailure
				} else {
					pGRD.GBAT[i] = int8(val)
				}
			}
		}
	}
	pGRD.USESKIP = false
	segment.ResultType = JBig2ImagePointer
	if pGRD.MMR {
		res := pGRD.StartDecodeMMR(&segment.Image, d.stream)
		if res != JBig2SegmentParseComplete {
			return ResultFailure
		}
		d.stream.AlignByte()
	} else {
		size := GetHuffContextSize(pGRD.GBTEMPLATE)
		gbContexts := make([]ArithCtx, size)
		arithDecoder := NewArithDecoder(d.stream)
		var err error
		segment.Image, err = pGRD.DecodeArith(arithDecoder, gbContexts)
		if err != nil {
			return ResultFailure
		}
		d.stream.AlignByte()
		d.stream.AddOffset(2)
	}
	if segment.Flags.Type != 36 {
		rect := pGRD.GetReplaceRect()
		d.expandPageForRegion(&ri)
		d.page.ComposeFrom(ri.X+rect.Left, ri.Y+rect.Top, segment.Image, composeOpFromRegionFlags(ri.Flags))
		segment.Image = nil
	}
	return ResultSuccess
}

// GetHuffContextSize 获取上下文大小
// 入参: template 模板号
// 返回: int 大小
func GetHuffContextSize(template byte) int {
	if template == 0 {
		return 65536
	} else if template == 1 {
		return 8192
	}
	return 1024
}

// parseGenericRefinementRegion 解析通用细化区域段
// 入参: segment 段对象
// 返回: Result 解析结果
func (d *Document) parseGenericRefinementRegion(segment *Segment) Result {
	var ri RegionInfo
	var flags byte
	if d.ParseRegionInfo(&ri) != ResultSuccess {
		return ResultFailure
	}
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	pGRRD := NewGRRDProc()
	pGRRD.GRW = uint32(ri.Width)
	pGRRD.GRH = uint32(ri.Height)
	pGRRD.GRTEMPLATE = (flags & 0x01) != 0
	pGRRD.TPGRON = ((flags >> 1) & 0x01) != 0
	if !pGRRD.GRTEMPLATE {
		for i := 0; i < 4; i++ {
			if val, err := d.stream.Read1Byte(); err != nil {
				return ResultFailure
			} else {
				pGRRD.GRAT[i] = int8(val)
			}
		}
	}
	var pageSubImage *Image
	if segment.ReferredToSegmentCount > 0 {
		var pSeg *Segment
		for _, refNum := range segment.ReferredToSegmentNumbers {
			pSeg = d.FindSegmentByNumber(refNum)
			if pSeg == nil {
				return ResultFailure
			}
			if pSeg.Flags.Type == 4 || pSeg.Flags.Type == 20 || pSeg.Flags.Type == 36 || pSeg.Flags.Type == 40 {
				break
			}
		}
		if pSeg != nil && pSeg.Image != nil {
			pGRRD.GRREFERENCE = pSeg.Image
		} else {
			return ResultFailure
		}
	} else {
		pageSubImage = d.page.SubImage(ri.X, ri.Y, ri.Width, ri.Height)
		pGRRD.GRREFERENCE = pageSubImage
	}
	pGRRD.GRREFERENCEDX = 0
	pGRRD.GRREFERENCEDY = 0
	size := 8192
	if pGRRD.GRTEMPLATE {
		size = 1024
	}
	grContexts := make([]ArithCtx, size)
	arithDecoder := NewArithDecoder(d.stream)
	segment.ResultType = JBig2ImagePointer
	var err error
	segment.Image, err = pGRRD.Decode(arithDecoder, grContexts)
	if err != nil {
		return ResultFailure
	}
	d.stream.AlignByte()
	d.stream.AddOffset(2)
	if segment.Flags.Type != 40 {
		d.expandPageForRegion(&ri)
		d.page.ComposeFrom(ri.X, ri.Y, segment.Image, composeOpFromRegionFlags(ri.Flags))
	}
	return ResultSuccess
}

// parsePageInfo 解析页面信息段
// 入参: segment 段对象
// 返回: Result 解析结果
func (d *Document) parsePageInfo(segment *Segment) Result {
	pi := &PageInfo{}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pi.Width = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pi.Height = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pi.ResolutionX = val
	}
	if val, err := d.stream.ReadInteger(); err != nil {
		return ResultFailure
	} else {
		pi.ResolutionY = val
	}
	var flags byte
	if val, err := d.stream.Read1Byte(); err != nil {
		return ResultFailure
	} else {
		flags = val
	}
	var striping uint16
	if val, err := d.stream.ReadShortInteger(); err != nil {
		return ResultFailure
	} else {
		striping = val
	}
	pi.DefaultPixelValue = (flags & 4) != 0
	pi.IsStriped = (striping & 0x8000) != 0
	pi.MaxStripeSize = striping & 0x7FFF
	height := pi.Height
	if height == 0xFFFFFFFF {
		height = uint32(pi.MaxStripeSize)
	}
	d.page = NewImage(int32(pi.Width), int32(height))
	if d.page == nil {
		return ResultFailure
	}
	d.page.Fill(pi.DefaultPixelValue)
	d.pageInfoList = append(d.pageInfoList, pi)
	d.inPage = true
	return ResultSuccess
}

// parseTable 解析表段
// 入参: segment 段对象
// 返回: Result 解析结果
func (d *Document) parseTable(segment *Segment) Result {
	segment.ResultType = JBig2HuffmanTablePointer
	huff := NewTableFromStream(d.stream)
	if !huff.IsOK() {
		return ResultFailure
	}
	segment.HuffmanTable = huff
	d.stream.AlignByte()
	return ResultSuccess
}

// ReleasePageSegments 释放页面段数据
// 入参: pageNumber 页面编号
func (d *Document) ReleasePageSegments(pageNumber uint32) {
	n := 0
	for _, seg := range d.segmentList {
		if seg.PageAssociation != pageNumber {
			d.segmentList[n] = seg
			n++
		} else {
			seg.Image = nil
			seg.PatternDict = nil
			seg.SymbolDict = nil
			seg.HuffmanTable = nil
		}
	}
	for i := n; i < len(d.segmentList); i++ {
		d.segmentList[i] = nil
	}
	d.segmentList = d.segmentList[:n]
}
