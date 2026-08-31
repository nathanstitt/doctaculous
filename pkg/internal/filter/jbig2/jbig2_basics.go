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

const (
	// JBig2OOB 越界标志
	JBig2OOB = 1
	// JBig2MaxReferredSegmentCount 最大参考段数
	JBig2MaxReferredSegmentCount = 64
	// JBig2MaxExportSymbols 最大导出符号数
	JBig2MaxExportSymbols = 65535
	// JBig2MaxNewSymbols 最大新符号数
	JBig2MaxNewSymbols = 65535
	// JBig2MaxPatternIndex 最大模式索引
	JBig2MaxPatternIndex = 65535
	// JBig2MaxImageSize 最大图像尺寸
	JBig2MaxImageSize = 65535
)

// ComposeOp 组合操作类型
type ComposeOp int

const (
	// ComposeOr 或操作
	ComposeOr ComposeOp = 0
	// ComposeAnd 与操作
	ComposeAnd ComposeOp = 1
	// ComposeXor 异或操作
	ComposeXor ComposeOp = 2
	// ComposeXnor 同或操作
	ComposeXnor ComposeOp = 3
	// ComposeReplace 替换操作
	ComposeReplace ComposeOp = 4
)

// RegionInfo 区域信息
type RegionInfo struct {
	Width  int32
	Height int32
	X      int32
	Y      int32
	Flags  uint8
}

// HuffmanCode 霍夫曼编码
type HuffmanCode struct {
	Codelen    int32
	Code       int32
	Val1       int32
	Val2       int32
	LowerRange bool
}

// Rect 矩形
type Rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// Width 获取宽度
// 返回: int32 宽度
func (r *Rect) Width() int32 {
	return r.Right - r.Left
}

// Height 获取高度
// 返回: int32 高度
func (r *Rect) Height() int32 {
	return r.Bottom - r.Top
}
