// Package webimage provides HTML-to-image rendering through an injectable
// [Renderer] interface.
//
// Because full HTML rendering requires a headless Chromium binary – a heavy
// deployment dependency – this package deliberately keeps the core module
// free of any browser driver.  You inject a concrete implementation at
// construction time via [WithRenderer]:
//
//	client := webimage.New(webimage.WithRenderer(myRenderer))
//	png, err := client.Render(ctx, "<h1>Hello</h1>")
//
// # Providing a renderer
//
// The simplest way to adapt an existing function is [RendererFunc]:
//
//	import "github.com/chromedp/chromedp"
//
//	renderer := webimage.RendererFunc(func(ctx context.Context, src string, srcIsURL bool, opts webimage.Options) ([]byte, error) {
//	    allocCtx, cancel := chromedp.NewExecAllocator(ctx, append(
//	        chromedp.DefaultExecAllocatorOptions[:],
//	        chromedp.Flag("headless", true),
//	        chromedp.WindowSize(opts.Width, opts.Height),
//	    )...)
//	    defer cancel()
//	    taskCtx, cancel := chromedp.NewContext(allocCtx)
//	    defer cancel()
//
//	    var buf []byte
//	    var tasks chromedp.Tasks
//	    if srcIsURL {
//	        tasks = chromedp.Tasks{
//	            chromedp.Navigate(src),
//	            chromedp.FullScreenshot(&buf, opts.Quality),
//	        }
//	    } else {
//	        tasks = chromedp.Tasks{
//	            chromedp.Navigate("about:blank"),
//	            chromedp.SetContent(src),
//	            chromedp.FullScreenshot(&buf, opts.Quality),
//	        }
//	    }
//	    return buf, chromedp.Run(taskCtx, tasks)
//	})
//
//	client := webimage.New(webimage.WithRenderer(renderer))
//
// # Per-call options
//
// Width, height, and output quality can be adjusted on individual calls:
//
//	png, err := client.Render(ctx, html,
//	    webimage.WithWidth(1920),
//	    webimage.WithHeight(1080),
//	)
//
// # Error without renderer
//
// If no renderer is injected, every render call returns [ErrNoRenderer].
// This makes it safe to create a default-value [Client] in configuration
// structs without requiring a non-nil renderer at startup time.
package webimage
