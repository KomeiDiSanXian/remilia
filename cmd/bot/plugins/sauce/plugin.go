// Package sauce 实现以图搜图插件。
//
// 聚合 SauceNAO / IQDB / TraceMoe / ASCII2D 四个引擎并发检索图片来源，
// 对结果进行跨引擎去重、相似度排序与长度截断后回复。支持图片 URL 或
// 本地字节直传，并对裁切小图进行放大预处理以提高命中率。
package sauce

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Plugin 以图搜图插件。
//
// 聚合多引擎并发检索并合并结果：
//   - SauceNAO：画作/插画主库（需 API key）
//   - IQDB：八个 booru 图库，对裁切图最友好（免费）
//   - TraceMoe：动画截图逐帧识别，给出番名/话数/时间点（免费）
//   - ASCII2D：色合+特征检索，画作/裁剪补位（免费，可能被 Cloudflare 拦截）
type Plugin struct {
	saucenao *saucenaoClient
	ascii2d  *ascii2dClient
	iqdb     *iqdbClient
	tracemoe *traceMoeClient
	cfg      plugin.ConfigReader
	log      plugin.Logger
}

// New 创建以图搜图插件的描述符。
//
// 注册 /sauce 命令：用户发送图片并在标题中附带命令即触发检索。
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "sauce",
		Version: "2.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "以图搜图，聚合 SauceNAO / IQDB / TraceMoe / ASCII2D 查找图片来源",
			Category:    "工具",
			Tags:        []string{"搜图", "SauceNAO", "IQDB", "TraceMoe", "以图搜图"},
			HelpText: `以图搜图 — 聚合多引擎查找图片来源

用法：
  发送图片并在标题中附带 /sauce 命令

支持引擎：
  SauceNAO（需 API key）、IQDB（booru，对裁切图友好）、
  TraceMoe（动画截图）、ASCII2D（色合+特征，可选）

示例：
  发送图片 + 标题 /sauce`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log
			p.cfg = ctx.Config
			p.saucenao = newSauceNAOClient()
			p.ascii2d = newASCII2DClient()
			p.iqdb = newIQDBClient()
			p.tracemoe = newTraceMoeClient()

			sauceDef := command.NewDef("sauce").Description("以图搜图，查找图片来源").
				Example("/sauce").Build()
			ctx.OnCommandDefWith("", "/sauce", sauceDef, p.handleSauce, eventctx.OnMentionedBotOrNoMentions())

			return p, nil
		},
	}
}

// ── 配置读取 ───────────────────────────────────────────────────────────
//
// 所有配置项均从 plugins.sauce 命名空间读取，支持热重载；缺省时使用
// 与 config.example.yaml 一致的默认值。

// apiKey 返回 SauceNAO API Key，未配置时返回空字符串。
func (p *Plugin) apiKey() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("saucenao_api_key", "")
}

// maxResults 返回最多展示的结果数（默认 3，非法值回退为 3）。
func (p *Plugin) maxResults() int {
	n := 3
	if p.cfg != nil {
		n = p.cfg.GetInt("max_results", 3)
	}
	if n <= 0 {
		return 3
	}
	return n
}

// saucenaoDB 返回 SauceNAO 检索数据库 ID（999 = 全部）。
func (p *Plugin) saucenaoDB() int {
	if p.cfg == nil {
		return 999
	}
	return p.cfg.GetInt("saucenao_db", 999)
}

// sendThumbnails 是否逐条附带缩略图图片发送结果。
func (p *Plugin) sendThumbnails() bool {
	return p.cfg != nil && p.cfg.GetBool("send_thumbnails", false)
}

// enableASCII2D 是否启用 ASCII2D 引擎（默认关闭，可能被 Cloudflare 拦截）。
func (p *Plugin) enableASCII2D() bool {
	return p.cfg != nil && p.cfg.GetBool("enable_ascii2d", false)
}

// enableIQDB 是否启用 IQDB 引擎（默认开启）。
func (p *Plugin) enableIQDB() bool {
	return p.cfg == nil || p.cfg.GetBool("enable_iqdb", true)
}

// enableTraceMoe 是否启用 trace.moe 引擎（默认开启）。
func (p *Plugin) enableTraceMoe() bool {
	return p.cfg == nil || p.cfg.GetBool("enable_trace_moe", true)
}

// similarityThreshold 全局相似度阈值。存在不低于阈值的匹配时只展示这些，
// 否则保留全部（避免裁切/滤镜图被全部过滤）。0 = 不过滤。
func (p *Plugin) similarityThreshold() float64 {
	if p.cfg == nil {
		return 60
	}
	return p.cfg.GetFloat64("similarity_threshold", 60)
}

// traceMoeMinSimilarity trace.moe 最低相似度（0-100），过滤无意义弱匹配。
func (p *Plugin) traceMoeMinSimilarity() float64 {
	if p.cfg == nil {
		return 75
	}
	return p.cfg.GetFloat64("trace_moe_min_similarity", 75)
}

// upscaleSmall 是否在上传前将小图（任一边 < 400px）放大 2 倍。
func (p *Plugin) upscaleSmall() bool {
	return p.cfg == nil || p.cfg.GetBool("upscale_small", true)
}

// reportErrors 是否在结果中上报引擎失败原因。
func (p *Plugin) reportErrors() bool {
	return p.cfg == nil || p.cfg.GetBool("report_errors", true)
}

// ── 命令处理 ───────────────────────────────────────────────────────────

// searchAll 并发提交所有已启用引擎，返回合并前的全部结果与引擎错误报告。
// 命令处理器与 AI 工具共用此流程，保证两者行为一致。
func (p *Plugin) searchAll(reqCtx context.Context, in engineInput, maxResults int) ([]SearchResult, []string) {
	type sourceResult struct {
		name    string
		results []SearchResult
		err     error
	}

	ch := make(chan sourceResult, 4)
	sources := 0
	submit := func(name string, fn func() ([]SearchResult, error)) {
		sources++
		go func() {
			results, err := fn()
			ch <- sourceResult{name: name, results: results, err: err}
		}()
	}

	if ak := p.apiKey(); ak != "" {
		submit("SauceNAO", func() ([]SearchResult, error) {
			return p.saucenao.Search(reqCtx, ak, p.saucenaoDB(), in, maxResults)
		})
	}
	if p.enableIQDB() {
		submit("IQDB", func() ([]SearchResult, error) {
			return p.iqdb.Search(reqCtx, in, maxResults)
		})
	}
	if p.enableTraceMoe() {
		submit("TraceMoe", func() ([]SearchResult, error) {
			return p.tracemoe.Search(reqCtx, in, maxResults, p.traceMoeMinSimilarity()/100)
		})
	}
	if p.enableASCII2D() {
		submit("ASCII2D", func() ([]SearchResult, error) {
			return p.ascii2d.Search(reqCtx, in, maxResults)
		})
	}

	var allResults []SearchResult
	var errReports []string
	for i := 0; i < sources; i++ {
		select {
		case res := <-ch:
			if res.err != nil {
				if p.reportErrors() {
					errReports = append(errReports, res.name+": "+res.err.Error())
				}
				continue
			}
			allResults = append(allResults, res.results...)
		case <-reqCtx.Done():
			return nil, errReports
		}
	}
	return allResults, errReports
}

// handleSauce 处理 /sauce 命令。
//
// 流程：提取消息图片 URL → 下载并预处理 → 并发提交各已启用引擎 → 合并、
// 去重、排序、截断 → 按 send_thumbnails 配置回复文本或文本+缩略图。
func (p *Plugin) handleSauce(ctx *eventctx.Context) error {
	imageURL := findImageURL(ctx.GetPlatformEvent())
	if imageURL == "" {
		ctx.ReplyError("请在消息中包含图片（如发送图片并在标题中附带 /sauce）")
		return nil
	}

	reqCtx, cancel := context.WithTimeout(ctx.Context(), 30*time.Second)
	defer cancel()

	ctx.ReplySuccess("正在搜索图片来源，请稍候…")

	raw, err := downloadImage(reqCtx, imageURL, 20*1024*1024)
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("下载图片失败: %v", err))
		return nil
	}

	proc, err := preprocessImage(raw, PreprocessOptions{UpscaleSmall: p.upscaleSmall()})
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("图片处理失败: %v", err))
		return nil
	}

	in := engineInput{ImageURL: imageURL, Data: proc.Data, Mime: proc.Mime}

	allResults, errReports := p.searchAll(reqCtx, in, p.maxResults())

	merged := mergeResults(allResults, p.similarityThreshold())
	results := pickResults(merged, p.maxResults())

	if len(results) == 0 {
		msg := "未找到匹配结果"
		if len(errReports) > 0 {
			msg += "\n\n（引擎异常：\n" + strings.Join(errReports, "\n") + "）"
		}
		ctx.ReplyText(msg)
		return nil
	}

	if p.sendThumbnails() {
		// 发送策略：
		//   - 支持图文同发（CapCaption）的平台：缩略图 + 单条结果信息 caption 一条消息
		//   - 其他平台（QQ 等富媒体消息会丢弃 Text/Markdown）：图片逐张单独发，
		//     作品信息汇总为一条（Markdown 优先，纯文本降级）
		caps := ctx.GetPlatformCapabilities()
		captionOK := caps.Has(platform.CapCaption)
		var infos []string
		for i, r := range results {
			oneText := p.formatOneResult(r, i+1)
			if r.Thumbnail == "" {
				infos = append(infos, oneText)
				continue
			}
			data, err := downloadImage(reqCtx, r.Thumbnail, 10*1024*1024)
			if err != nil {
				infos = append(infos, oneText)
				continue
			}
			mimeType := detectMimeType(r.Thumbnail, data)
			att := platform.Attachment{
				Kind:     platform.AttachmentKindImage,
				Data:     data,
				Name:     "sauce" + extByMime(mimeType),
				MimeType: mimeType,
			}
			if captionOK {
				ctx.Reply(platform.TextMessage(oneText).WithAttachments(att))
			} else {
				ctx.Reply(platform.OutboundMessage{Attachments: []platform.Attachment{att}})
				infos = append(infos, oneText)
			}
		}
		if p.reportErrors() && len(errReports) > 0 {
			infos = append(infos, "（部分引擎异常）\n"+strings.Join(errReports, "\n"))
		}
		if len(infos) > 0 {
			summary := strings.Join(infos, "\n\n")
			if captionOK || !caps.Has(platform.CapMarkdown) {
				ctx.Reply(platform.TextMessage(summary))
			} else {
				ctx.Reply(platform.MarkdownMessage(summary))
			}
		}
		return nil
	}

	ctx.Reply(platform.TextMessage(p.formatResults(results, errReports, p.reportErrors())))
	return nil
}
