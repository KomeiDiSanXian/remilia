package webimage

import (
	"context"
	"errors"
	"fmt"
)

// ─── 类型定义 ──────────────────────────────────────────────────────────────────

// Format 标识输出图片的编码格式。
type Format int

const (
	FormatPNG  Format = iota // 无损 PNG（默认）
	FormatJPEG               // 有损 JPEG
)

// Options 控制单次渲染的视口尺寸和输出格式。
type Options struct {
	// Width 浏览器视口宽度（像素），默认 1280。
	Width int
	// Height 浏览器视口高度（像素）。
	// 0 表示"完整页面高度"（具体行为由渲染器决定）。
	Height int
	// Quality JPEG 质量 [1, 100]，默认 90。
	// Format == FormatPNG 时忽略此字段。
	Quality int
	// Format 输出图片格式，默认 FormatPNG。
	Format Format
	// WaitSelector 非空时，渲染器在截图前等待该 CSS 选择器出现在 DOM 中。
	// 适用于异步加载内容的页面。
	WaitSelector string
}

// RenderOption 是单次调用时修改 [Options] 的函数类型。
// 通过 [WithWidth]、[WithHeight]、[WithFormat] 等函数获取。
type RenderOption func(*Options)

// WithWidth 为单次渲染设置视口宽度。
func WithWidth(w int) RenderOption { return func(o *Options) { o.Width = w } }

// WithHeight 为单次渲染设置视口高度。
func WithHeight(h int) RenderOption { return func(o *Options) { o.Height = h } }

// WithQuality 为单次渲染设置 JPEG 质量 [1, 100]。
func WithQuality(q int) RenderOption { return func(o *Options) { o.Quality = q } }

// WithFormat 为单次渲染设置输出图片编码格式。
func WithFormat(f Format) RenderOption { return func(o *Options) { o.Format = f } }

// WithWaitSelector 指定渲染器在截图前等待的 CSS 选择器。
func WithWaitSelector(sel string) RenderOption {
	return func(o *Options) { o.WaitSelector = sel }
}

// ─── Renderer 接口 ─────────────────────────────────────────────────────────────

// Renderer 是所有 HTML→图片后端需要实现的接口。
//
// src 为 HTML 字符串（srcIsURL==false）或 URL（srcIsURL==true）。
// 实现应返回 PNG 或 JPEG 编码的图片字节。
//
// 参考 doc.go 中的 chromedp 示例实现。
type Renderer interface {
	Render(ctx context.Context, src string, srcIsURL bool, opts Options) ([]byte, error)
}

// RendererFunc 是实现 [Renderer] 接口的函数类型。
// 可将普通函数直接用作渲染器，无需定义具名结构体：
//
//	renderer := webimage.RendererFunc(func(ctx context.Context, src string, srcIsURL bool, opts webimage.Options) ([]byte, error) {
//	    // ...具体实现...
//	})
type RendererFunc func(ctx context.Context, src string, srcIsURL bool, opts Options) ([]byte, error)

// Render 实现 [Renderer] 接口。
func (f RendererFunc) Render(ctx context.Context, src string, srcIsURL bool, opts Options) ([]byte, error) {
	return f(ctx, src, srcIsURL, opts)
}

// ─── Client ───────────────────────────────────────────────────────────────────

// Client 是 HTML→图片渲染的主要入口。
// 通过 [New] 创建；零值也可直接使用，但每次调用均返回 [ErrNoRenderer]，
// 直到通过 [Client.SetRenderer] 或 [WithRenderer] 注入渲染器。
type Client struct {
	renderer Renderer
	defaults Options
}

// ClientOption 配置 [Client]。
type ClientOption func(*Client)

// WithRenderer 设置具体的渲染器实现。
// 未调用此选项时，每次渲染调用均返回 [ErrNoRenderer]。
func WithRenderer(r Renderer) ClientOption {
	return func(c *Client) { c.renderer = r }
}

// WithDefaults 为该 Client 的所有调用设置默认 [Options]。
// 单次调用仍可通过 [RenderOption] 参数覆盖具体字段。
func WithDefaults(o Options) ClientOption {
	return func(c *Client) { c.defaults = o }
}

// New 创建带可选配置的 [Client]。
//
// 示例：
//
//	client := webimage.New(
//	    webimage.WithRenderer(myChromedpRenderer),
//	    webimage.WithDefaults(webimage.Options{Width: 1280, Height: 720, Quality: 90}),
//	)
func New(opts ...ClientOption) *Client {
	c := &Client{
		defaults: Options{
			Width:   1280,
			Height:  720,
			Quality: 90,
			Format:  FormatPNG,
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Render 将 HTML 字符串渲染为截图图片。
// 若未注入渲染器，返回 [ErrNoRenderer]。
// 单次覆盖选项仅影响本次调用，不修改 Client 默认值。
func (c *Client) Render(ctx context.Context, html string, opts ...RenderOption) ([]byte, error) {
	return c.doRender(ctx, html, false, opts)
}

// RenderURL 对指定 URL 进行全页截图。
// 若未注入渲染器，返回 [ErrNoRenderer]。
// 单次覆盖选项仅影响本次调用，不修改 Client 默认值。
func (c *Client) RenderURL(ctx context.Context, url string, opts ...RenderOption) ([]byte, error) {
	return c.doRender(ctx, url, true, opts)
}

// SetRenderer 在运行时替换渲染器实现。
// 适用于延迟初始化或在测试中切换后端。
func (c *Client) SetRenderer(r Renderer) {
	c.renderer = r
}

// HasRenderer 报告是否已配置渲染器。
func (c *Client) HasRenderer() bool {
	return c.renderer != nil
}

func (c *Client) doRender(ctx context.Context, src string, isURL bool, extraOpts []RenderOption) ([]byte, error) {
	if c.renderer == nil {
		return nil, fmt.Errorf("%w：请通过 webimage.WithRenderer(r) 或 client.SetRenderer(r) 注入渲染器", ErrNoRenderer)
	}
	o := c.defaults
	for _, fn := range extraOpts {
		fn(&o)
	}
	return c.renderer.Render(ctx, src, isURL, o)
}

// ─── 错误定义 ──────────────────────────────────────────────────────────────────

// ErrNoRenderer 在 [Client] 未配置渲染器时返回。
// 可通过 [errors.Is] 判断：
//
//	if errors.Is(err, webimage.ErrNoRenderer) { ... }
var ErrNoRenderer = errors.New("webimage: 未配置渲染器")
