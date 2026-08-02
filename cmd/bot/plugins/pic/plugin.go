// Package pic plugin.go — 随机图片插件的命令处理与配置。
//
// 命令: /pic [tags...] [xN] [-site name]
// AI 工具: get_random_image(tags, count?)
package pic

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Plugin 随机图片插件实例。
type Plugin struct {
	client *booruClient
	cfg    plugin.ConfigReader
	log    plugin.Logger
}

// New 创建随机图片插件的 Descriptor。
//
// 命令:
//   - /pic                  随机发送一张图（rating 策略允许的任意站点）
//   - /pic <tags...>        按标签随机发图，如 /pic 東方
//   - /pic cat x3           一次发 3 张
//   - /pic -site rule34 xxx 指定站点（受 rating 策略约束）
//
// AI:
//   - get_random_image(tags, count?) → 图片 URL 与作品信息文本
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "pic",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "按标签发送随机图片，聚合 Safebooru / Gelbooru / rule34 / Konachan / Yande.re",
			Category:    "娱乐",
			Tags:        []string{"图片", "随机", "booru", "标签", "NSFW"},
			HelpText: `随机图片 — 按标签从多个图库随机发送图片

用法：
  /pic                  随机一张
  /pic <tags...>        按标签随机，如 /pic 東方、/pic touhou hairband
  /pic cat x3           一次多张（xN 后缀须位于末尾，上限受 max_count 配置）
  /pic -count 3         显式指定张数（与 xN 等价，优先级更高）
  /pic -site rule34 xxx 指定站点（受 rating 配置约束）
  /pic x3 -count 1      搜索名为 x3 的标签（-count 显式张数可消除歧义）

内容分级由 plugins.pic.rating 配置控制（默认 safe）。
档位（由轻到重）：safe（安全）< sensitive（轻度敏感，如泳装/暗示）
< questionable（敏感级）< explicit（露骨级 NSFW）。
注意：sensitive 是 gelbooru 迁移时新增的档位，旧三档体系
（safe/questionable/explicit）没有对应档位——旧站点上此类内容
归入 questionable。精确配置 sensitive 时仅 gelbooru 可用。

rating 为精确档位或区间：
  - 单档：rating: "safe" 只发安全级内容
  - 区间：rating: "safe..questionable" 发安全+轻度敏感+敏感级
  - all：全部档位不限制
站点仅在其可提供的档位与区间有交集时参与请求
（如 rating: "explicit" 时 safebooru / konachan 不可用）。

各站档位映射（自动处理，无需配置）：
  - gelbooru：general ↔ safe；sensitive / questionable / explicit 一一对应
  - safebooru / konachan：仅 safe（safebooru 新旧评级并存，均视为 safe）
  - yande.re / rule34：保留旧体系，与内部档位一致

认证配置（可选）：
  - plugins.pic.gelbooru_user_id / gelbooru_api_key：gelbooru.com 必需
    （免费注册获取），否则返回 401
  - plugins.pic.rule34_user_id / rule34_api_key：rule34.xxx 必需，
    在 https://rule34.xxx/index.php?page=account&s=options 获取
  - konachan 使用 SFW 镜像 konachan.net（仅 safe 内容，无反爬拦截）

示例：
  /pic
  /pic 東方 x2
  /pic -site konachan original`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log
			p.cfg = ctx.Config
			// 凭据仅在 Setup 时读取一次（修改需重启插件）
			// 凭据与代理仅在 Setup 时读取一次（修改需重启插件）
			client, cerr := newBooruClient(p.credentials(), p.proxy())
			if cerr != nil {
				return nil, fmt.Errorf("pic: 初始化客户端失败: %w", cerr)
			}
			p.client = client

			picDef := command.NewDef("pic").Description("按标签发送随机图片").
				Arg("tags", "图片标签，多个标签用空格分隔；末尾 xN 为数量后缀（如 cat x3），中间位置一律视为标签", false).
				Flag("count", "", "显式指定张数（与 xN 后缀等价，优先级更高）", command.ArgTypeInt).
				Flag("site", "", "指定图库站点：safebooru/gelbooru/rule34/konachan/yandere（受 rating 策略约束）", command.ArgTypeString).
				Example("/pic").Example("/pic 東方").Example("/pic cat x3").
				Example("/pic -site konachan original").Example("/pic x3 -count 1").Build()
			ctx.OnCommandDefWith("", "/pic", picDef, p.handlePic, eventctx.OnMentionedBotOrNoMentions())

			return p, nil
		},
	}
}

// ── 配置读取 ───────────────────────────────────────────────────────────

// credentials 汇总各站点的 API 认证凭据。
func (p *Plugin) credentials() booruCredentials {
	if p.cfg == nil {
		return booruCredentials{}
	}
	return booruCredentials{
		GelbooruUserID: p.cfg.GetString("gelbooru_user_id", ""),
		GelbooruAPIKey: p.cfg.GetString("gelbooru_api_key", ""),
		Rule34UserID:   p.cfg.GetString("rule34_user_id", ""),
		Rule34APIKey:   p.cfg.GetString("rule34_api_key", ""),
	}
}

// proxy 返回图库请求的代理地址（如 "http://127.0.0.1:7890"）。
// 空值沿用环境变量代理或直连；Setup 时读取一次（修改需重启）。
func (p *Plugin) proxy() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("proxy", "")
}

// rating 返回全局内容分级区间（默认 safe，即只发安全级内容）。
func (p *Plugin) rating() RatingRange {
	if p.cfg == nil {
		return RatingRange{Min: RatingSafe, Max: RatingSafe}
	}
	return parseRatingRange(p.cfg.GetString("rating", string(RatingSafe)))
}

// enabledSites 返回站点白名单（空 = 全部内置站点）。
func (p *Plugin) enabledSites() []string {
	if p.cfg == nil {
		return nil
	}
	return p.cfg.GetStringSlice("sites", nil)
}

// maxCount 返回单次最多发送张数（默认 3，非法值回退为 3）。
func (p *Plugin) maxCount() int {
	n := 3
	if p.cfg != nil {
		n = p.cfg.GetInt("max_count", 3)
	}
	if n <= 0 {
		return 3
	}
	return n
}

// ── 参数解析 ───────────────────────────────────────────────────────────

// picArgs 解析后的 /pic 命令参数。
type picArgs struct {
	Tags  []string
	Count int
	Site  string // 指定站点名（空 = 自动选择）
}

// parsePicArgs 解析 /pic 参数：
//   - -site <name> 指定站点
//   - -count N 显式指定张数（与 xN 后缀等价，优先级更高）
//   - 末尾的 xN（如 x3）作为数量后缀；中间位置的 xN 一律视为标签
//   - 其余全部视为标签
//
// 消歧说明：想搜索名为 x3 的标签时，用 -count 显式指定张数，
// 如 /pic x3 -count 1，此时 x3 作为标签、张数为 1。
func parsePicArgs(positional []string, maxCount int) picArgs {
	args := picArgs{Count: 1}
	countSet := false // 是否显式指定过 -count
	var tags []string
	for i := 0; i < len(positional); i++ {
		arg := positional[i]
		if strings.EqualFold(arg, "-site") && i+1 < len(positional) {
			args.Site = positional[i+1]
			i++
			continue
		}
		if strings.EqualFold(arg, "-count") && i+1 < len(positional) {
			if n, err := strconv.Atoi(positional[i+1]); err == nil && n > 0 {
				args.Count = n
				countSet = true
			}
			i++
			continue
		}
		tags = append(tags, arg)
	}

	// 未显式指定 -count 时，末尾的 xN 形式视为数量后缀
	if len(tags) > 0 && !countSet {
		last := tags[len(tags)-1]
		if n, ok := countSuffix(last); ok {
			args.Count = n
			tags = tags[:len(tags)-1]
		}
	}

	args.Tags = tags
	if args.Count > maxCount {
		args.Count = maxCount
	}
	return args
}

// countSuffix 解析 xN 形式（如 x3 / X2）的数量后缀，非该形式时返回 false。
func countSuffix(s string) (int, bool) {
	if len(s) < 2 || (s[0] != 'x' && s[0] != 'X') {
		return 0, false
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// ── 命令处理 ───────────────────────────────────────────────────────────

// handlePic 处理 /pic 命令。
//
// 流程：解析参数 → 确定候选站点（指定或按 rating 兼容全部）→ 逐个尝试
// 请求随机图（单个站点失败/无结果时自动降级到下一站）→ 下载图片 →
// 按平台能力发送（图文一条或图片+汇总信息）。
func (p *Plugin) handlePic(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil {
		ctx.ReplyError("用法: /pic [标签...] [x数量] [-site 站点名]")
		return nil
	}

	args := parsePicArgs(parsed.Positional, p.maxCount())

	var candidates []site
	if args.Site != "" {
		s, ok := p.resolveSite(args.Site)
		if !ok {
			ctx.ReplyError(fmt.Sprintf("站点 %q 不存在或当前 rating 策略下不可用", args.Site))
			return nil
		}
		candidates = []site{s}
	} else {
		candidates = candidateSites(p.enabledSites(), p.rating())
		if len(candidates) == 0 {
			ctx.ReplyError("没有可用的图库站点（请检查 plugins.pic.sites 白名单与 rating 配置）")
			return nil
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx.Context(), 90*time.Second)
	defer cancel()

	s, posts, err := p.fetchWithFallback(reqCtx, candidates, args.Tags, args.Count)
	if err != nil {
		ctx.ReplyError(err.Error())
		return nil
	}
	if len(posts) == 0 {
		ctx.ReplyText("没有找到匹配的图片，换个标签试试？")
		return nil
	}

	p.sendPicResult(ctx, reqCtx, s, posts)
	return nil
}

// sendPicResult 并发下载并发送图片结果。
//
// 发送策略：
//   - 支持图文同发（CapCaption）的平台：图片附件 + 单条作品信息 caption 一条消息
//   - 其他平台（QQ 等富媒体会丢弃文本）：图片逐张单独发，作品信息汇总一条
//     （Markdown 优先，纯文本降级）
//
// 多张图片时并发下载（受 max_count 钳制，默认 ≤3），完成后按原顺序发送，
// 避免大图串行下载拖慢整体响应。
func (p *Plugin) sendPicResult(ctx *eventctx.Context, reqCtx context.Context, s site, posts []picPost) {
	caps := ctx.GetPlatformCapabilities()
	captionOK := caps.Has(platform.CapCaption)
	referer := "https://" + s.Domain + "/"

	type dlResult struct {
		post picPost
		data []byte
		err  error
	}
	results := make([]dlResult, len(posts))
	var wg sync.WaitGroup
	for i, post := range posts {
		wg.Add(1)
		go func(i int, post picPost) {
			defer wg.Done()
			data, err := p.client.downloadImage(reqCtx, post.FileURL, referer, maxPicBytes)
			results[i] = dlResult{post: post, data: data, err: err}
		}(i, post)
	}
	wg.Wait()

	for i, res := range results {
		if res.err != nil {
			logger.Warnf("[pic] download %s failed: %v", res.post.FileURL, res.err)
			continue
		}
		att := platform.Attachment{
			Kind:     platform.AttachmentKindImage,
			Data:     res.data,
			Name:     "pic_" + strconv.Itoa(res.post.ID) + ".jpg",
			MimeType: "image/jpeg",
		}
		if captionOK {
			ctx.Reply(platform.TextMessage(formatPostText(res.post, i+1)).WithAttachments(att))
		} else {
			ctx.Reply(platform.OutboundMessage{Attachments: []platform.Attachment{att}})
		}
	}

	if !captionOK {
		if caps.Has(platform.CapMarkdown) {
			ctx.Reply(platform.MarkdownMessage(formatResultsMD(s.DisplayName, posts)))
		} else {
			ctx.Reply(platform.TextMessage(formatResultsText(s.DisplayName, posts)))
		}
	}
}

// fetchWithFallback 获取随机图片：单站直接尝试，多站并发取最快成功者。
//
// 多站场景下并发请求全部候选站点（每站独立 20s 超时），返回第一个
// 成功的结果并取消其余请求——慢站点（如 gelbooru）不再拖垮整次命令。
// 全部站点失败时返回汇总错误（含各站点失败原因）。
func (p *Plugin) fetchWithFallback(ctx context.Context, candidates []site, tags []string, count int) (site, []picPost, error) {
	if len(candidates) == 0 {
		return site{}, nil, nil
	}
	if len(candidates) == 1 {
		// 用户指定站点：单站尝试
		s := candidates[0]
		siteCtx, scancel := context.WithTimeout(ctx, 20*time.Second)
		defer scancel()
		posts, err := p.fetchPosts(siteCtx, s, tags, count)
		if err != nil {
			return site{}, nil, fmt.Errorf("获取图片失败：%s", s.DisplayName+": "+err.Error())
		}
		return s, posts, nil
	}

	// 并发尝试全部候选站点：第一个成功即返回，取消其余请求。
	// 结果通道带缓冲（容量=站点数），即使提前返回也不会阻塞 goroutine。
	raceCtx, raceCancel := context.WithCancel(ctx)
	defer raceCancel()

	type result struct {
		s     site
		posts []picPost
		err   error
	}
	ch := make(chan result, len(candidates))
	for _, s := range candidates {
		go func(s site) {
			siteCtx, scancel := context.WithTimeout(raceCtx, 20*time.Second)
			defer scancel()
			posts, err := p.fetchPosts(siteCtx, s, tags, count)
			ch <- result{s: s, posts: posts, err: err}
		}(s)
	}

	var errs []string
	for range candidates {
		select {
		case res := <-ch:
			if res.err != nil {
				errs = append(errs, res.s.DisplayName+": "+res.err.Error())
				continue
			}
			if len(res.posts) == 0 {
				continue
			}
			return res.s, res.posts, nil
		case <-ctx.Done():
			return site{}, nil, fmt.Errorf("获取图片超时")
		}
	}
	if len(errs) > 0 {
		return site{}, nil, fmt.Errorf("获取图片失败：%s", strings.Join(errs, "；"))
	}
	return site{}, nil, nil
}

// resolveSite 解析用户指定站点并校验其可用性。
func (p *Plugin) resolveSite(name string) (site, bool) {
	s, ok := findSite(name)
	if !ok {
		return site{}, false
	}
	if !s.usable(p.rating()) {
		return site{}, false
	}
	return s, true
}

// fetchPosts 在指定站点请求随机图片（供命令与 AI 工具共用）。
func (p *Plugin) fetchPosts(ctx context.Context, s site, tags []string, count int) ([]picPost, error) {
	if count <= 0 {
		count = 1
	}
	return p.client.fetchRandom(ctx, s, tags, p.rating(), count)
}
