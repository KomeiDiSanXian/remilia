// Package sauce 实现以图搜图插件。
//
// 聚合 SauceNAO / IQDB / TraceMoe / AnimeTrace 四个引擎并发检索图片来源，
// 对结果进行跨引擎去重、相似度排序与长度截断后回复。支持图片 URL 或
// 本地字节直传，并对裁切小图进行放大预处理以提高命中率。图片可通过
// 消息附件、引用消息或"先发命令再等待图片"三种方式提供。
package sauce

import (
	"context"
	"fmt"
	"net/http"
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
//   - AnimeTrace：动画/Galgame 图片角色与作品识别（免费）
type Plugin struct {
	saucenao   *saucenaoClient
	iqdb       *iqdbClient
	tracemoe   *traceMoeClient
	animetrace *animeTraceClient
	httpClient *http.Client
	cfg        plugin.ConfigReader
	log        plugin.Logger
	reg        plugin.RegistryWriter
}

// New 创建以图搜图插件的描述符。
//
// 注册 /sauce 命令：用户发送图片并在标题中附带命令即触发检索；
// 未携带图片时等待用户补发，引用消息含图片时也可直接触发。
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "sauce",
		Version: "2.1.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "以图搜图，聚合 SauceNAO / IQDB / TraceMoe / AnimeTrace 查找图片来源",
			Category:    "工具",
			Tags:        []string{"搜图", "SauceNAO", "IQDB", "TraceMoe", "AnimeTrace", "以图搜图"},
			HelpText: `以图搜图 — 聚合多引擎查找图片来源

用法：
  1. 发送图片并在标题中附带 /sauce 命令
  2. 发送 /sauce 后等待，再发送要搜索的图片（60 秒内）
  3. 引用一条含图片的消息并发送 /sauce

参数：
  -engine <name>  指定引擎：saucenao / iqdb / tracemoe / animetrace / all（默认 all）

支持引擎：
  SauceNAO（需 API key）、IQDB（booru，对裁切图友好）、
  TraceMoe（动画截图）、AnimeTrace（动画/Galgame 角色识别）

提示：
  IQDB 高峰期排队时，其他引擎结果会先返回并提示排队状态，
  IQDB 完成后自动补发结果

示例：
  发送图片 + 标题 /sauce
  /sauce -engine animetrace`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log
			p.cfg = ctx.Config
			p.reg = ctx.Reg

			// 共享 Transport：全部引擎与图片下载统一走代理配置；
			// 各引擎客户端持有独立超时（IQDB 排队需更长超时）。
			if terr := initSauceTransport(p.proxy()); terr != nil {
				return nil, fmt.Errorf("sauce: 初始化代理失败: %w", terr)
			}
			p.httpClient = newSauceHTTPClient(30 * time.Second)
			p.saucenao = newSauceNAOClient(newSauceHTTPClient(15 * time.Second))
			p.iqdb = newIQDBClient(newSauceHTTPClient(p.iqdbTimeout()), p.iqdbRetries())
			p.tracemoe = newTraceMoeClient(newSauceHTTPClient(20 * time.Second))
			p.animetrace = newAnimeTraceClient(newSauceHTTPClient(20 * time.Second))

			sauceDef := command.NewDef("sauce").Description("以图搜图，查找图片来源").
				Flag("engine", "", "指定引擎：saucenao / iqdb / tracemoe / animetrace / all（默认 all）", command.ArgTypeString).
				Example("/sauce").Example("/sauce -engine animetrace").Build()
			ctx.OnCommandDefWith("", "/sauce", sauceDef, p.handleSauce, eventctx.OnMentionedBotOrNoMentions())

			return p, nil
		},
	}
}

// ── 引擎集合 ───────────────────────────────────────────────────────────

// engineSet 是本次检索启用的引擎集合（去重后的名称列表）。
type engineSet map[string]bool

// engineNames 全部可用引擎名。
var engineNames = []string{"saucenao", "iqdb", "tracemoe", "animetrace"}

// allEngines 返回全部已启用引擎的集合。
func (p *Plugin) allEngines() engineSet {
	s := engineSet{}
	if p.apiKey() != "" {
		s["saucenao"] = true
	}
	if p.enableIQDB() {
		s["iqdb"] = true
	}
	if p.enableTraceMoe() {
		s["tracemoe"] = true
	}
	if p.enableAnimeTrace() {
		s["animetrace"] = true
	}
	return s
}

// parseEngineSet 解析 -engine 参数为引擎集合。
//
// 支持多个值以逗号分隔（如 "tracemoe,animetrace"）；"all" 表示全部；
// 空值等同 "all"。非法名称返回 nil 并给出可读错误。
func (p *Plugin) parseEngineSet(raw string) (engineSet, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" || raw == "all" {
		return p.allEngines(), nil
	}

	valid := map[string]bool{}
	for _, n := range engineNames {
		valid[n] = true
	}

	s := engineSet{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if !valid[name] {
			return nil, fmt.Errorf("未知引擎 %q，可选：%s", name, strings.Join(engineNames, " / "))
		}
		s[name] = true
	}
	if len(s) == 0 {
		return p.allEngines(), nil
	}
	return s, nil
}

// ── 命令处理 ───────────────────────────────────────────────────────────

// engineOutcome 单个引擎的检索结果。
type engineOutcome struct {
	name    string
	results []SearchResult
	err     error
}

// engineTask 待提交的引擎任务。
type engineTask struct {
	name string
	fn   func() ([]SearchResult, error)
}

// engineTasks 构建本次检索要提交的引擎任务列表（按引擎集合与启用开关过滤）。
// reqCtx 由调用方提供（检索总超时），任务闭包共享该上下文。
func (p *Plugin) engineTasks(reqCtx context.Context, in engineInput, maxResults int, engines engineSet) []engineTask {
	var tasks []engineTask
	if engines["saucenao"] && p.apiKey() != "" {
		tasks = append(tasks, engineTask{"SauceNAO", func() ([]SearchResult, error) {
			return p.saucenao.Search(reqCtx, p.apiKey(), p.saucenaoDB(), in, maxResults)
		}})
	}
	if engines["iqdb"] && p.enableIQDB() {
		tasks = append(tasks, engineTask{"IQDB", func() ([]SearchResult, error) {
			return p.iqdb.Search(reqCtx, in, maxResults)
		}})
	}
	if engines["tracemoe"] && p.enableTraceMoe() {
		tasks = append(tasks, engineTask{"TraceMoe", func() ([]SearchResult, error) {
			return p.tracemoe.Search(reqCtx, in, maxResults, p.traceMoeMinSimilarity()/100)
		}})
	}
	if engines["animetrace"] && p.enableAnimeTrace() {
		tasks = append(tasks, engineTask{"AnimeTrace", func() ([]SearchResult, error) {
			return p.animetrace.Search(reqCtx, in, maxResults, false)
		}})
	}
	return tasks
}

// submitEngines 并发提交所有引擎任务，返回结果通道。
// 通道容量与任务数相同，所有任务完成后无需调用方关闭通道。
func submitEngines(reqCtx context.Context, tasks []engineTask) <-chan engineOutcome {
	ch := make(chan engineOutcome, len(tasks))
	for _, t := range tasks {
		go func(t engineTask) {
			results, err := t.fn()
			ch <- engineOutcome{name: t.name, results: results, err: err}
		}(t)
	}
	return ch
}

// searchAll 并发提交集合内所有引擎，等待全部完成后返回合并前的
// 全部结果与引擎错误报告。AI 工具使用此流程（需要完整结果）；
// 命令流程使用 searchBatched 以获得分批回复体验。
func (p *Plugin) searchAll(reqCtx context.Context, in engineInput, maxResults int, engines engineSet) ([]SearchResult, []string) {
	tasks := p.engineTasks(reqCtx, in, maxResults, engines)
	ch := submitEngines(reqCtx, tasks)

	var allResults []SearchResult
	var errReports []string
	for i := 0; i < len(tasks); i++ {
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
			return allResults, errReports
		}
	}
	return allResults, errReports
}

// handleSauce 处理 /sauce 命令。
//
// 流程：解析 -engine 参数 → 解析图片输入（消息附件 / 引用消息 / 等待
// 用户补发）→ 下载并预处理 → 并发提交各已启用引擎 → 合并、去重、排序、
// 截断 → 按 send_thumbnails 配置回复文本或文本+缩略图。
func (p *Plugin) handleSauce(ctx *eventctx.Context) error {
	engines, err := p.resolveEnginesFromCommand(ctx)
	if err != nil {
		ctx.ReplyError(err.Error())
		return nil
	}

	imageURL, ok := p.resolveImageSource(ctx, engines)
	if !ok {
		return nil // 已进入等待流程或已回复错误
	}

	p.runSearch(ctx, imageURL, engines)
	return nil
}

// resolveEnginesFromCommand 解析命令中的 -engine 参数。
//
// 框架解析器只识别 --engine（双横线）与单字符短标志；
// 单横线多字符形式 -engine 会落入 Positional，与 pic 的 -site 同策略，
// 这里手动从 Positional 中解析。两种形式都支持：
//   - --engine iqdb / -e iqdb（增强解析 Flags，需 def 定义）
//   - -engine iqdb / -engine=iqdb（手动 Positional 解析）
func (p *Plugin) resolveEnginesFromCommand(ctx *eventctx.Context) (engineSet, error) {
	// 1. 增强解析 Flags（覆盖 --engine 与定义的单字符短标志）
	if parsed := ctx.GetParsedCommand(); parsed != nil {
		if raw := parsed.GetString("engine"); raw != "" {
			return p.parseEngineSet(raw)
		}
	}

	// 2. 手动从 Positional 解析 -engine <value> / -engine=<value>
	parsed, err := eventctx.ParseCommand(ctx)
	if err == nil {
		pos := parsed.Positional
		for i := 0; i < len(pos); i++ {
			arg := pos[i]
			if strings.EqualFold(arg, "-engine") && i+1 < len(pos) {
				return p.parseEngineSet(pos[i+1])
			}
			if v, ok := strings.CutPrefix(arg, "-engine="); ok && v != "" {
				return p.parseEngineSet(v)
			}
		}
	}
	return p.allEngines(), nil
}

// runSearch 以指定图片 URL 执行完整检索并回复结果。
//
// 分批发送策略：等待所有非 IQDB 引擎完成后，若 IQDB 尚未返回，
// 再给 IQDB 一段宽限期（iqdb_grace，默认 10s）：
//   - 宽限期内返回 → 所有结果合并为一条消息发送
//   - 宽限期结束仍未返回 → 先发送其他引擎结果并提示"IQDB 排队中"，
//     IQDB 完成后（成功或失败）补发一条消息
func (p *Plugin) runSearch(ctx *eventctx.Context, imageURL string, engines engineSet) {
	reqCtx, cancel := context.WithTimeout(ctx.Context(), p.searchTimeout())
	// 进入 IQDB 后台补发路径后，取消权移交给补发 goroutine：
	// runSearch 返回时不得提前 cancel reqCtx，否则 IQDB 任务被立即中断。
	followUpStarted := false
	defer func() {
		if !followUpStarted {
			cancel()
		}
	}()

	ctx.ReplySuccess("正在搜索图片来源，请稍候…")

	raw, err := p.downloadImage(reqCtx, imageURL, 20*1024*1024)
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("下载图片失败: %v", err))
		return
	}

	proc, err := preprocessImage(raw, PreprocessOptions{UpscaleSmall: p.upscaleSmall()})
	if err != nil {
		ctx.ReplyError(fmt.Sprintf("图片处理失败: %v", err))
		return
	}

	in := engineInput{ImageURL: imageURL, Data: proc.Data, Mime: proc.Mime}

	tasks := p.engineTasks(reqCtx, in, p.maxResults(), engines)
	iqdbSelected := false
	nonIQDB := 0
	for _, t := range tasks {
		if t.name == "IQDB" {
			iqdbSelected = true
		} else {
			nonIQDB++
		}
	}
	ch := submitEngines(reqCtx, tasks)

	// 第一阶段：等待所有非 IQDB 引擎完成（顺带收集提前返回的 IQDB）
	var results []SearchResult
	var errs []string
	iqdbGot := !iqdbSelected
	collect := func(res engineOutcome) {
		if res.err != nil {
			if p.reportErrors() {
				errs = append(errs, res.name+": "+res.err.Error())
			}
			return
		}
		results = append(results, res.results...)
	}
	for pending := nonIQDB; pending > 0; {
		select {
		case res := <-ch:
			if res.name == "IQDB" {
				iqdbGot = true
				collect(res)
				continue
			}
			pending--
			collect(res)
		case <-reqCtx.Done():
			p.sendSearchResults(ctx, results, errs, "")
			return
		}
	}

	// 第二阶段：IQDB 尚未返回时给予宽限期
	if iqdbSelected && !iqdbGot {
		res, received := waitIQDBOutcome(ch, reqCtx, p.iqdbGrace())
		if received {
			collect(res)
		} else {
			// 宽限期到：先发第一批，IQDB 后台继续等待并补发
			note := "IQDB 正在排队，结果稍后补充"
			p.sendSearchResults(ctx, results, errs, note)
			followUpStarted = true
			go func() {
				defer cancel()
				p.sendIQDBFollowUp(ctx, ch, reqCtx)
			}()
			return
		}
	}

	p.sendSearchResults(ctx, results, errs, "")
}

// waitIQDBOutcome 在宽限期内等待 IQDB 结果。
//
// 返回 (outcome, true) 表示宽限期内已拿到结果；返回 (_, false) 表示
// 宽限期结束仍未返回（调用方应进入后台补发路径）。
func waitIQDBOutcome(ch <-chan engineOutcome, reqCtx context.Context, grace time.Duration) (engineOutcome, bool) {
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case res := <-ch:
		return res, true
	case <-timer.C:
		return engineOutcome{}, false
	case <-reqCtx.Done():
		return engineOutcome{name: "IQDB", err: reqCtx.Err()}, true
	}
}

// sendIQDBFollowUp 在后台等待 IQDB 结果并补发消息（runSearch 宽限期后的分支）。
//
// 使用 ctx.Clone() 的独立上下文，不依赖原 handler 的生命周期。
func (p *Plugin) sendIQDBFollowUp(origCtx *eventctx.Context, ch <-chan engineOutcome, reqCtx context.Context) {
	clone := origCtx.Clone()
	select {
	case res := <-ch:
		if res.err != nil {
			if p.reportErrors() {
				clone.ReplyText("📡 " + res.err.Error())
			}
			return
		}
		if len(res.results) == 0 {
			clone.ReplyText("📡 IQDB 未找到匹配结果")
			return
		}
		merged := mergeResults(res.results, p.similarityThreshold())
		results := pickResults(merged, p.maxResults())
		if len(results) == 0 {
			clone.ReplyText("📡 IQDB 未找到匹配结果")
			return
		}
		var b strings.Builder
		b.WriteString("📡 IQDB 检索完成：\n\n")
		for i, r := range results {
			b.WriteString(p.formatOneResult(r, i+1))
			b.WriteString("\n\n")
		}
		clone.Reply(platform.TextMessage(strings.TrimRight(b.String(), "\n")))
	case <-reqCtx.Done():
		clone.ReplyText("📡 IQDB 排队超时，未获取结果")
	}
}

// sendSearchResults 合并、排序、截断结果并按 send_thumbnails 配置回复。
//
// note 为附加提示（如"IQDB 正在排队，结果稍后补充"），追加在消息末尾。
func (p *Plugin) sendSearchResults(ctx *eventctx.Context, allResults []SearchResult, errReports []string, note string) {
	merged := mergeResults(allResults, p.similarityThreshold())
	results := pickResults(merged, p.maxResults())

	if len(results) == 0 {
		msg := "未找到匹配结果"
		if note != "" {
			msg += "\n\n" + note
		}
		if len(errReports) > 0 {
			msg += "\n\n（引擎异常：\n" + strings.Join(errReports, "\n") + "）"
		}
		ctx.ReplyText(msg)
		return
	}

	if p.sendThumbnails() {
		// 发送策略：
		//   - 支持图文同发（CapCaption）的平台：缩略图 + 单条结果信息 caption 一条消息
		//   - 其他平台（QQ 等富媒体消息会丢弃 Text/Markdown）：图片逐张单独发，
		//     作品信息汇总为一条（Markdown 优先，纯文本降级）
		reqCtx, cancel := context.WithTimeout(ctx.Context(), p.searchTimeout())
		defer cancel()
		caps := ctx.GetPlatformCapabilities()
		captionOK := caps.Has(platform.CapCaption)
		var infos []string
		for i, r := range results {
			oneText := p.formatOneResult(r, i+1)
			if r.Thumbnail == "" {
				infos = append(infos, oneText)
				continue
			}
			data, err := p.downloadImage(reqCtx, r.Thumbnail, 10*1024*1024)
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
		if note != "" {
			infos = append(infos, note)
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
		return
	}

	msg := p.formatResults(results, errReports, p.reportErrors())
	if note != "" {
		msg += "\n\n" + note
	}
	ctx.Reply(platform.TextMessage(msg))
}
