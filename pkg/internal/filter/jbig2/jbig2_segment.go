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

// JBig2SegmentState 段状态
type JBig2SegmentState int

const (
	JBig2SegmentHeaderUnparsed JBig2SegmentState = 0
	JBig2SegmentDataUnparsed   JBig2SegmentState = 1
	JBig2SegmentParseComplete  JBig2SegmentState = 2
	JBig2SegmentPaused         JBig2SegmentState = 3
	JBig2SegmentError          JBig2SegmentState = 4
)

// JBig2ResultType 段结果类型
type JBig2ResultType int

const (
	JBig2VoidPointer         JBig2ResultType = 0
	JBig2ImagePointer        JBig2ResultType = 1
	JBig2SymbolDictPointer   JBig2ResultType = 2
	JBig2PatternDictPointer  JBig2ResultType = 3
	JBig2HuffmanTablePointer JBig2ResultType = 4
)

// SegmentFlags 段标志位
type SegmentFlags struct {
	Type                uint8
	PageAssociationSize bool
	DeferredNonRetain   bool
}

// Segment 段结构
type Segment struct {
	Number                   uint32
	Flags                    SegmentFlags
	ReferredToSegmentCount   int32
	ReferredToSegmentNumbers []uint32
	PageAssociation          uint32
	DataLength               uint32
	HeaderLength             uint32
	DataOffset               uint32
	Key                      uint64
	State                    JBig2SegmentState
	ResultType               JBig2ResultType
	SymbolDict               *SymbolDict
	PatternDict              *PatternDict
	Image                    *Image
	HuffmanTable             *HuffmanTable
	GBContexts               []ArithCtx
	GRContexts               []ArithCtx
}

// NewSegment 创建段对象
// 返回: *Segment 段对象
func NewSegment() *Segment {
	return &Segment{
		State:      JBig2SegmentHeaderUnparsed,
		ResultType: JBig2VoidPointer,
	}
}
