package gif

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	stdgif "image/gif"
	"io"
)

// Encoder 逐帧构建 GIF 动图。
//
// Encoder 的所有方法均非并发安全；
// 若需要在多个 goroutine 中添加帧，请通过外部互斥锁保护。
type Encoder struct {
	frames    []*image.Paletted
	delays    []int // GIF 时间单位：1/100 秒
	disposals []byte
	loopCount int
	palette   color.Palette
	dithering bool
}

// New 创建带默认配置的 Encoder。
// 默认配置：256 色网页安全调色板 + Floyd-Steinberg 抖动 + 无限循环（LoopCount=0）。
func New(opts ...Option) *Encoder {
	e := &Encoder{
		loopCount: 0,
		palette:   defaultPalette(),
		dithering: true,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// ─── 选项 ──────────────────────────────────────────────────────────────────────

// Option 配置 [Encoder]。
type Option func(*Encoder)

// WithPalette 为所有后续帧设置自定义颜色调色板。
// 调色板最多支持 256 个颜色条目，超出部分在编码时会被静默截断。
func WithPalette(p color.Palette) Option {
	return func(e *Encoder) { e.palette = p }
}

// WithLoopCount 设置 GIF 循环次数。
//
//   - 0   = 无限循环（默认）
//   - -1  = 仅播放一次，不循环
//   - n>0 = 循环 n 次
func WithLoopCount(n int) Option {
	return func(e *Encoder) { e.loopCount = n }
}

// WithDithering 启用或禁用 Floyd-Steinberg 误差扩散抖动（默认：启用）。
// 禁用抖动速度更快，但在渐变图像上可能出现明显色带。
func WithDithering(enabled bool) Option {
	return func(e *Encoder) { e.dithering = enabled }
}

// ─── 帧操作 API ────────────────────────────────────────────────────────────────

// AddFrame 将 img 颜色量化后作为新动画帧追加。
// delayMs 为帧间延迟（毫秒），向下取整至 10ms 精度（GIF 时间单位为 1/100 秒）；
// 不足 10ms 时自动按 10ms 处理。
func (e *Encoder) AddFrame(img image.Image, delayMs int) error {
	paletted, err := quantise(img, e.palette, e.dithering)
	if err != nil {
		return fmt.Errorf("gif: 颜色量化失败: %w", err)
	}
	e.appendFrame(paletted, delayMs)
	return nil
}

// AddPaletted 直接追加预量化帧，跳过颜色转换（零拷贝，适合需要精确控制调色板的场景）。
func (e *Encoder) AddPaletted(img *image.Paletted, delayMs int) {
	e.appendFrame(img, delayMs)
}

func (e *Encoder) appendFrame(img *image.Paletted, delayMs int) {
	delay := delayMs / 10
	if delay < 1 {
		delay = 1
	}
	e.frames = append(e.frames, img)
	e.delays = append(e.delays, delay)
	e.disposals = append(e.disposals, stdgif.DisposalBackground)
}

// ─── 输出 ──────────────────────────────────────────────────────────────────────

// Encode 将 GIF 动图写入 w。
// 若尚未添加任何帧，则返回错误。
func (e *Encoder) Encode(w io.Writer) error {
	if len(e.frames) == 0 {
		return fmt.Errorf("gif: 无帧可编码")
	}
	return stdgif.EncodeAll(w, &stdgif.GIF{
		Image:     e.frames,
		Delay:     e.delays,
		Disposal:  e.disposals,
		LoopCount: e.loopCount,
	})
}

// Bytes 将 GIF 动图编码并返回字节切片。
// 若尚未添加任何帧，则返回错误。
func (e *Encoder) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := e.Encode(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Len 返回已添加的帧数。
func (e *Encoder) Len() int { return len(e.frames) }

// Reset 清空所有帧缓冲，同时保留调色板、抖动和循环次数等配置。
func (e *Encoder) Reset() {
	e.frames = e.frames[:0]
	e.delays = e.delays[:0]
	e.disposals = e.disposals[:0]
}

// ─── 颜色量化 ──────────────────────────────────────────────────────────────────

// quantise 将任意 image.Image 转换为使用指定调色板的 *image.Paletted。
// dither=true 时启用 Floyd-Steinberg 误差扩散抖动。
func quantise(img image.Image, p color.Palette, dither bool) (*image.Paletted, error) {
	bounds := img.Bounds()
	dst := image.NewPaletted(bounds, p)
	if dither {
		stddraw.FloydSteinberg.Draw(dst, bounds, img, bounds.Min)
	} else {
		stddraw.Draw(dst, bounds, img, bounds.Min, stddraw.Src)
	}
	return dst, nil
}

// ─── 默认调色板 ────────────────────────────────────────────────────────────────

// defaultPalette 返回 256 色网页安全调色板：
// 216 色（6×6×6 RGB 立方体）+ 40 级均匀灰阶，共 256 个条目。
func defaultPalette() color.Palette {
	p := make(color.Palette, 0, 256)

	// 6×6×6 RGB 立方体（216 色）
	for r := range 6 {
		for g := range 6 {
			for b := range 6 {
				p = append(p, color.RGBA{
					R: uint8(r * 51),
					G: uint8(g * 51),
					B: uint8(b * 51),
					A: 255,
				})
			}
		}
	}

	// 40 级灰阶，补足至 256 个条目
	for i := 1; i <= 40; i++ {
		v := uint8(i * 255 / 40)
		p = append(p, color.RGBA{R: v, G: v, B: v, A: 255})
	}

	return p
}
