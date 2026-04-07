package webimage

import (
	"context"
	"errors"
	"fmt"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// Format identifies the output image encoding.
type Format int

const (
	FormatPNG  Format = iota // Lossless PNG (default)
	FormatJPEG               // Lossy JPEG
)

// Options controls the viewport geometry and output format for a single render.
type Options struct {
	// Width is the browser viewport width in pixels (default 1280).
	Width int
	// Height is the browser viewport height in pixels.
	// 0 means "full page height" (renderer-dependent).
	Height int
	// Quality is the JPEG quality [1, 100] (default 90).
	// Ignored when Format == FormatPNG.
	Quality int
	// Format selects the output encoding (default FormatPNG).
	Format Format
	// WaitSelector, if non-empty, instructs the renderer to wait for this CSS
	// selector to appear in the DOM before capturing the screenshot.
	// Useful for pages that load content asynchronously.
	WaitSelector string
}

// RenderOption is a function that mutates an [Options] value for a single call.
// Obtain one from [WithWidth], [WithHeight], [WithFormat], etc.
type RenderOption func(*Options)

// WithWidth sets the viewport width for a single render call.
func WithWidth(w int) RenderOption { return func(o *Options) { o.Width = w } }

// WithHeight sets the viewport height for a single render call.
func WithHeight(h int) RenderOption { return func(o *Options) { o.Height = h } }

// WithQuality sets the JPEG quality [1, 100] for a single render call.
func WithQuality(q int) RenderOption { return func(o *Options) { o.Quality = q } }

// WithFormat sets the output image encoding for a single render call.
func WithFormat(f Format) RenderOption { return func(o *Options) { o.Format = f } }

// WithWaitSelector instructs the renderer to wait for the given CSS selector
// before capturing the screenshot.
func WithWaitSelector(sel string) RenderOption {
	return func(o *Options) { o.WaitSelector = sel }
}

// ─── Renderer interface ───────────────────────────────────────────────────────

// Renderer is the interface satisfied by any concrete HTML-to-image backend.
//
// src is either an HTML string (srcIsURL == false) or a URL (srcIsURL == true).
// Implementations must return PNG- or JPEG-encoded image bytes.
//
// See the package documentation for a chromedp-based example implementation.
type Renderer interface {
	Render(ctx context.Context, src string, srcIsURL bool, opts Options) ([]byte, error)
}

// RendererFunc is a function type that implements [Renderer].
// It lets you pass a plain function as a renderer without defining a named type:
//
//	renderer := webimage.RendererFunc(func(ctx context.Context, src string, srcIsURL bool, opts webimage.Options) ([]byte, error) {
//	    // ... implementation ...
//	})
type RendererFunc func(ctx context.Context, src string, srcIsURL bool, opts Options) ([]byte, error)

// Render implements [Renderer].
func (f RendererFunc) Render(ctx context.Context, src string, srcIsURL bool, opts Options) ([]byte, error) {
	return f(ctx, src, srcIsURL, opts)
}

// ─── Client ───────────────────────────────────────────────────────────────────

// Client is the main entry point for HTML-to-image rendering.
// Create one with [New]; the zero value is usable (but returns [ErrNoRenderer]
// on every call until a renderer is injected).
type Client struct {
	renderer Renderer
	defaults Options
}

// ClientOption configures a [Client].
type ClientOption func(*Client)

// WithRenderer sets the concrete renderer implementation.
// Without this option the client returns [ErrNoRenderer] on every call.
func WithRenderer(r Renderer) ClientOption {
	return func(c *Client) { c.renderer = r }
}

// WithDefaults sets the default [Options] for all calls made from this client.
// Individual calls can still override individual fields via [RenderOption] funcs.
func WithDefaults(o Options) ClientOption {
	return func(c *Client) { c.defaults = o }
}

// New creates a [Client] with optional configuration.
//
// Example:
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

// Render converts an HTML string to a screenshot image.
//
// Returns [ErrNoRenderer] if no renderer was injected via [WithRenderer].
// Per-call options override the client's defaults for this call only.
func (c *Client) Render(ctx context.Context, html string, opts ...RenderOption) ([]byte, error) {
	return c.doRender(ctx, html, false, opts)
}

// RenderURL captures a full-page screenshot of the given URL.
//
// Returns [ErrNoRenderer] if no renderer was injected via [WithRenderer].
// Per-call options override the client's defaults for this call only.
func (c *Client) RenderURL(ctx context.Context, url string, opts ...RenderOption) ([]byte, error) {
	return c.doRender(ctx, url, true, opts)
}

// SetRenderer replaces the client's renderer at runtime.
// Useful for deferred initialisation or swapping backends in tests.
func (c *Client) SetRenderer(r Renderer) {
	c.renderer = r
}

// HasRenderer reports whether a renderer has been configured.
func (c *Client) HasRenderer() bool {
	return c.renderer != nil
}

func (c *Client) doRender(ctx context.Context, src string, isURL bool, extraOpts []RenderOption) ([]byte, error) {
	if c.renderer == nil {
		return nil, fmt.Errorf("%w: inject one via webimage.WithRenderer(r) or client.SetRenderer(r)", ErrNoRenderer)
	}
	o := c.defaults
	for _, fn := range extraOpts {
		fn(&o)
	}
	return c.renderer.Render(ctx, src, isURL, o)
}

// ─── Errors ───────────────────────────────────────────────────────────────────

// ErrNoRenderer is returned when a [Client] has no renderer configured.
// Wrap or compare with [errors.Is]:
//
//	if errors.Is(err, webimage.ErrNoRenderer) { ... }
var ErrNoRenderer = errors.New("webimage: no renderer configured")
