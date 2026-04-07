// Package webimage 通过可注入的 [Renderer] 接口提供 HTML→图片渲染能力。
//
// 由于完整的 HTML 渲染需要无头 Chromium 二进制——这是一项较重的部署依赖——
// 本包的核心模块刻意不引入任何浏览器驱动。
// 调用方在构建 [Client] 时通过 [WithRenderer] 注入具体的渲染实现：
//
//	client := webimage.New(webimage.WithRenderer(myRenderer))
//	png, err := client.Render(ctx, "<h1>你好</h1>")
//
// # 注入渲染器
//
// 使用 [RendererFunc] 可以将普通函数直接适配为渲染器，无需定义具名类型：
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
// # 单次调用覆盖选项
//
//	png, err := client.Render(ctx, html,
//	    webimage.WithWidth(1920),
//	    webimage.WithHeight(1080),
//	)
//
// # 无渲染器时的行为
//
// 若未注入渲染器，每次调用均返回 [ErrNoRenderer]。
// 这使得在配置结构体中持有默认值 [Client] 是安全的，无需在启动时强制要求非空渲染器。
package webimage
