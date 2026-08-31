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

// SymbolDict 符号字典
type SymbolDict struct {
	gbContexts []ArithCtx
	grContexts []ArithCtx
	Images     []*Image
}

// NewSymbolDict 创建符号字典对象
// 返回: *SymbolDict 符号字典对象
func NewSymbolDict() *SymbolDict {
	return &SymbolDict{}
}

// DeepCopy 深拷贝符号字典
// 返回: *SymbolDict 符号字典副本
func (s *SymbolDict) DeepCopy() *SymbolDict {
	dst := NewSymbolDict()
	for _, img := range s.Images {
		if img != nil {
			dst.Images = append(dst.Images, img.Duplicate())
		} else {
			dst.Images = append(dst.Images, nil)
		}
	}
	dst.gbContexts = make([]ArithCtx, len(s.gbContexts))
	copy(dst.gbContexts, s.gbContexts)
	dst.grContexts = make([]ArithCtx, len(s.grContexts))
	copy(dst.grContexts, s.grContexts)
	return dst
}

// AddImage 添加图像到字典
// 入参: image 图像对象
func (s *SymbolDict) AddImage(image *Image) {
	s.Images = append(s.Images, image)
}

// NumImages 获取字典中的图像数量
// 返回: int 图像数量
func (s *SymbolDict) NumImages() int {
	return len(s.Images)
}

// GetImage 从字典获取图像
// 入参: index 索引
// 返回: *Image 图像对象
func (s *SymbolDict) GetImage(index int) *Image {
	if index < 0 || index >= len(s.Images) {
		return nil
	}
	return s.Images[index]
}

// GbContexts 获取算术编码通用上下文
// 返回: []ArithCtx 上下文集合
func (s *SymbolDict) GbContexts() []ArithCtx {
	return s.gbContexts
}

// GrContexts 获取算术编码细化上下文
// 返回: []ArithCtx 上下文集合
func (s *SymbolDict) GrContexts() []ArithCtx {
	return s.grContexts
}

// SetGbContexts 设置算术编码通用上下文
// 入参: contexts 上下文集合
func (s *SymbolDict) SetGbContexts(contexts []ArithCtx) {
	s.gbContexts = contexts
}

// SetGrContexts 设置算术编码细化上下文
// 入参: contexts 上下文集合
func (s *SymbolDict) SetGrContexts(contexts []ArithCtx) {
	s.grContexts = contexts
}
