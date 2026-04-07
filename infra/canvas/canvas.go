// 已废弃：该文件保留仅为兼容旧版引用。
//
// infra/canvas 包已合并至 infra/textimage，请直接使用：
//
//	textimage.NewVectorCanvas(width, height)   // 替代 canvas.New
//	textimage.NewVectorCard(width)             // 替代 canvas.NewCard
//	*textimage.VectorCanvas                    // 替代 *canvas.Canvas
//
// 本包将在未来版本中删除。
package canvas

import (
	"image"
	"image/color"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

// Canvas 已废弃。请改用 [textimage.VectorCanvas]。
//
// Deprecated: 使用 textimage.NewVectorCanvas 或 textimage.NewVectorCard 代替。
type Canvas = textimage.VectorCanvas

// New 已废弃。请改用 [textimage.NewVectorCanvas]。
//
// Deprecated: 使用 textimage.NewVectorCanvas(width, height) 代替。
func New(width, height int) *Canvas {
	return textimage.NewVectorCanvas(width, height)
}

// NewCard 已废弃。请改用 [textimage.NewVectorCard]。
//
// Deprecated: 使用 textimage.NewVectorCard(width) 代替。
func NewCard(width int) *Canvas {
	return textimage.NewVectorCard(width)
}

// 以下变量仅用于确保 image 和 color 包在编译时被引用，防止 import 被优化掉。
var (
	_ image.Image = (*image.RGBA)(nil)
	_ color.Color = color.Black
)
