// Package bilibili 提供 Bilibili UP 主信息查询、直播状态查询和用户搜索功能。
//
// 命令: /bili user <uid/用户名>, /bili live <uid/用户名>, /bili search <关键词>
// AI 工具: search_bilibili_user, get_bilibili_user_info, get_bilibili_live_status
package bilibili

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

var _ health.CheckProvider = (*Plugin)(nil)

// Plugin Bilibili 查询插件实例。
type Plugin struct {
	client *biliClient
	probes []*health.APIProbe
	log    plugin.Logger
	cfg    plugin.ConfigReader
	watch  *watchManager
	reg    *platform.Registry // 平台适配器注册表（用于开播通知主动推送）
}

// Option bilibili 插件的可选配置项。
type Option func(*Plugin)

// WithPlatformRegistry 设置平台适配器注册表。
// 用于开播订阅通知的主动推送（从注册表动态获取 SessionNotifier）。
func WithPlatformRegistry(reg *platform.Registry) Option {
	return func(p *Plugin) { p.reg = reg }
}

// New 创建 Bilibili 查询插件的 Descriptor。
//
// 命令:
//   - /bili user <uid/用户名>
//   - /bili live <uid/用户名>
//   - /bili search <关键词>
//
// AI:
//   - search_bilibili_user(keyword) → 搜索用户
//   - get_bilibili_user_info(uid)   → UP 主详情
//   - get_bilibili_live_status(uid) → 直播状态
func New(opts ...Option) *plugin.Descriptor {
	p := &Plugin{}
	for _, o := range opts {
		o(p)
	}
	return &plugin.Descriptor{
		Name:    "bilibili",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "Bilibili UP 主信息、直播状态、用户搜索",
			Category:    "工具",
			Tags:        []string{"B站", "bilibili", "直播", "查询", "搜索"},
			HelpText: `B站查询 — 查询 UP 主信息、直播状态、视频、番剧

用法：
  /bili user <uid/用户名>      查询 UP 主信息（支持直接输入用户名自动解析）
  /bili live <uid/用户名>      查询直播状态（纯数字优先按 UID，无直播间时回退房间号）
  /bili live room:<房间号>     显式按房间号查询直播状态（避免与 UID 歧义）
  /bili search <关键词>         按用户名搜索 UP 主
  /bili video <BV号>           查询视频信息（播放/弹幕/硬币/收藏等）
  /bili videos <uid/用户名>    查询 UP 主最近投稿
  /bili bangumi <关键词>       搜索番剧/影视
  /bili watch <uid/用户名>     订阅开播通知（在所在群订阅，开播时推送）
  /bili unwatch <uid/用户名>   取消订阅
  /bili watch list             查看本群订阅列表

示例：
  /bili user 泠鸢yousa
  /bili live 282994
  /bili live room:47377
  /bili search 泠鸢
  /bili video BV1GJ411x7h7
  /bili videos 282994
  /bili bangumi 间谍过家家
  /bili watch 282994`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log
			p.cfg = ctx.Config
			proxy := p.proxy()
			p.client = newBiliClient(p.sessdata(), proxy)
			biliImageSessdata = p.sessdata()
			biliImageProxy = proxy
			watchInterval := time.Duration(0)
			if p.cfg != nil {
				watchInterval = p.cfg.GetDuration("watch_interval", 60*time.Second)
			}
			if watchInterval <= 0 {
				watchInterval = 60 * time.Second
			}
			p.watch = newWatchManager("data/bilibili", watchInterval)
			// Setup 阶段即注册推送能力（依赖注入的 registry），重启后订阅通知自动恢复
			p.registerNotifier(nil)

			biliProbe := health.NewAPIProbe("api.bilibili.com", "https://api.bilibili.com/x/web-interface/nav", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			liveProbe := health.NewAPIProbe("api.live.bilibili.com", "https://api.live.bilibili.com/room/v1/Room/getRoomInfoOld?mid=1", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			searchProbe := health.NewAPIProbe("search.bilibili.com", "https://api.bilibili.com/x/web-interface/search/type?search_type=bili_user&keyword=test", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			p.probes = []*health.APIProbe{biliProbe, liveProbe, searchProbe}

			for _, pr := range p.probes {
				ctx.Spawn(func(runCtx context.Context) {
					pr.StartBackground(runCtx, 1*time.Minute)
				})
			}

			biliDef := command.NewDef("bili").Description("Bilibili UP 主信息、直播状态、用户搜索").
				SubCommand(command.NewDef("user").Description("查询 UP 主信息").Build()).
				SubCommand(command.NewDef("live").Description("查询直播状态（支持 room:<房间号>）").Build()).
				SubCommand(command.NewDef("search").Description("搜索 UP 主").Build()).
				SubCommand(command.NewDef("video").Description("查询视频信息").Build()).
				SubCommand(command.NewDef("videos").Description("查询 UP 主最近投稿").Build()).
				SubCommand(command.NewDef("bangumi").Description("搜索番剧/影视").Build()).
				SubCommand(command.NewDef("watch").Description("订阅开播通知").Build()).
				Example("/bili user 泠鸢yousa").Example("/bili live 282994").Example("/bili live room:47377").Example("/bili search 泠鸢").
				Example("/bili video BV1GJ411x7h7").Example("/bili videos 282994").Example("/bili bangumi 间谍过家家").Example("/bili watch 282994").Build()
			ctx.OnCommandDefWith("", "/bili", biliDef, p.handleBili, eventctx.OnMentionedBotOrNoMentions())

			if !ctx.DryRun {
				ctx.SpawnNamed("bili-watch-loop", p.watchLoop)
			}

			return p, nil
		},
	}
}

// handleBili 处理 /bili 命令。
func (p *Plugin) handleBili(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		ctx.ReplyError("用法：/bili user|live|search|video|videos|bangumi|watch 详见帮助")
		return nil
	}

	subCmd := parsed.Positional[0]

	reqCtx, cancel := context.WithTimeout(ctx.Context(), 10*time.Second)
	defer cancel()

	switch strings.ToLower(subCmd) {
	case "search", "s":
		if len(parsed.Positional) < 2 {
			ctx.ReplyError("请输入搜索关键词，例如：/bili search 泠鸢")
			return nil
		}
		keyword := strings.Join(parsed.Positional[1:], " ")
		results, _, err := p.client.SearchUser(reqCtx, keyword, 1)
		if err != nil {
			ctx.ReplyText(fmt.Sprintf("[B站] 搜索失败: %v", err))
			return nil
		}
		if len(results) == 0 {
			ctx.ReplyText(fmt.Sprintf("未找到与「%s」相关的用户", keyword))
			return nil
		}
		png, imgErr := renderUserSearchResults(results, keyword)
		if imgErr != nil {
			ctx.ReplyText(formatSearchUserText(results))
			return nil
		}
		ctx.Reply(platform.ImageDataMessage(png, "bilibili_search.png", "image/png"))
		return err

	case "user", "u":
		if len(parsed.Positional) < 2 {
			ctx.ReplyError("请输入 UID 或用户名，例如：/bili user 泠鸢yousa")
			return nil
		}
		input := parsed.Positional[1]
		mid, resolvedName, err := p.client.ResolveUID(reqCtx, input)
		if err != nil {
			ctx.ReplyText(fmt.Sprintf("[B站] %v", err))
			return nil
		}
		user, rel, err := p.client.FetchUserInfo(reqCtx, mid)
		if err != nil {
			ctx.ReplyText(fmt.Sprintf("[B站] %s\nUID: %d\n(数据获取失败: %v)", resolvedName, mid, err))
			return nil
		}
		png, imgErr := renderUserCard(user, rel)
		if imgErr != nil {
			ctx.ReplyText(formatBiliUserText(user, rel))
			return nil
		}
		ctx.Reply(platform.ImageDataMessage(png, "bilibili_user.png", "image/png"))
		return err

	case "live", "l":
		if len(parsed.Positional) < 2 {
			ctx.ReplyError("请输入 UID/用户名/房间号，例如：/bili live 泠鸢yousa；或 room:<房间号> 显式指定房间号")
			return nil
		}
		input := parsed.Positional[1]

		// 显式房间号：/bili live room:<房间号>，避免与 UID 歧义
		if strings.HasPrefix(strings.ToLower(input), "room:") {
			roomID, rerr := strconv.ParseInt(strings.TrimPrefix(input, "room:"), 10, 64)
			if rerr != nil {
				ctx.ReplyError("房间号格式不正确，示例：/bili live room:47377")
				return nil
			}
			live, lerr := p.client.FetchLiveInfoByRoom(reqCtx, roomID)
			if lerr != nil {
				ctx.ReplyText(fmt.Sprintf("[B站] 查询直播状态失败: %v", lerr))
				return nil
			}
			p.sendLiveCard(ctx, live, "")
			return nil
		}

		mid, resolvedName, err := p.client.ResolveUID(reqCtx, input)
		if err != nil {
			ctx.ReplyText(fmt.Sprintf("[B站] %v", err))
			return nil
		}
		live, err := p.client.FetchLiveInfo(reqCtx, mid)
		if err != nil {
			ctx.ReplyText(fmt.Sprintf("[B站] 查询直播状态失败: %v", err))
			return nil
		}
		// 纯数字输入时，若该 UID 无直播间（可能输入的是房间号），回退按房间号查询
		if live.RoomID <= 0 || live.RoomStatus == 0 {
			if _, perr := strconv.ParseInt(input, 10, 64); perr == nil && live.RoomID > 0 && live.RoomID != mid {
				if byRoom, rerr := p.client.FetchLiveInfoByRoom(reqCtx, mid); rerr == nil && byRoom.RoomID > 0 {
					live = byRoom
				}
			}
		}
		p.sendLiveCard(ctx, live, resolvedName)
		return nil

	case "video", "v":
		if len(parsed.Positional) < 2 {
			ctx.ReplyError("请输入 BV 号，例如：/bili video BV1GJ411x7h7")
			return nil
		}
		bvid := strings.ToUpper(parsed.Positional[1])
		if !strings.HasPrefix(bvid, "BV") {
			bvid = "BV" + bvid
		}
		info, err := p.client.FetchVideoInfo(reqCtx, bvid)
		if err != nil {
			ctx.ReplyText(fmt.Sprintf("[B站] 查询视频失败: %v", err))
			return nil
		}
		png, imgErr := renderVideoCard(info)
		if imgErr != nil {
			ctx.ReplyText(formatVideoText(info))
			return nil
		}
		ctx.Reply(platform.ImageDataMessage(png, "bilibili_video.png", "image/png"))
		return nil

	case "videos", "vs":
		if len(parsed.Positional) < 2 {
			ctx.ReplyError("请输入 UID 或用户名，例如：/bili videos 282994")
			return nil
		}
		mid, resolvedName, err := p.client.ResolveUID(reqCtx, parsed.Positional[1])
		if err != nil {
			ctx.ReplyText(fmt.Sprintf("[B站] %v", err))
			return nil
		}
		items, err := p.client.FetchVideos(reqCtx, mid, 1)
		if err != nil {
			ctx.ReplyText(fmt.Sprintf("[B站] 查询投稿失败: %v", err))
			return nil
		}
		if len(items) == 0 {
			ctx.ReplyText(fmt.Sprintf("[B站] %s 暂无投稿", resolvedName))
			return nil
		}
		png, imgErr := renderVideoListCard(items, resolvedName)
		if imgErr != nil {
			ctx.ReplyText(formatVideoListText(items, resolvedName))
			return nil
		}
		ctx.Reply(platform.ImageDataMessage(png, "bilibili_videos.png", "image/png"))
		return nil

	case "bangumi", "bg":
		if len(parsed.Positional) < 2 {
			ctx.ReplyError("请输入番剧关键词，例如：/bili bangumi 间谍过家家")
			return nil
		}
		keyword := strings.Join(parsed.Positional[1:], " ")
		results, err := p.client.SearchBangumi(reqCtx, keyword, 5)
		if err != nil {
			ctx.ReplyText(fmt.Sprintf("[B站] 搜索番剧失败: %v", err))
			return nil
		}
		if len(results) == 0 {
			ctx.ReplyText(fmt.Sprintf("未找到与「%s」相关的番剧", keyword))
			return nil
		}
		png, imgErr := renderBangumiResults(results, keyword)
		if imgErr != nil {
			ctx.ReplyText(formatBangumiText(results))
			return nil
		}
		ctx.Reply(platform.ImageDataMessage(png, "bilibili_bangumi.png", "image/png"))
		return nil

	case "watch", "w":
		p.handleWatch(ctx, reqCtx, parsed.Positional)
		return nil

	case "unwatch":
		p.handleUnwatch(ctx, reqCtx, parsed.Positional)
		return nil

	default:
		ctx.ReplyError("未知子命令，支持：user, live, search, video, videos, bangumi, watch")
		return nil
	}
}

// sendLiveCard 渲染并发送直播状态卡片（渲染失败时回退纯文本）。
func (p *Plugin) sendLiveCard(ctx *eventctx.Context, live *LiveInfo, resolvedName string) {
	if live.UserName == "" && resolvedName != "" {
		live.UserName = resolvedName
	}
	png, imgErr := renderLiveCard(live)
	if imgErr != nil {
		ctx.ReplyText(formatLiveText(live))
		return
	}
	ctx.Reply(platform.ImageDataMessage(png, "bilibili_live.png", "image/png"))
}

// handleWatch 处理 /bili watch <uid/用户名> 与 /bili watch list。
// 订阅绑定到当前所在群（群消息）或订阅者（私聊），开播时主动推送。
func (p *Plugin) handleWatch(ctx *eventctx.Context, reqCtx context.Context, args []string) {
	if len(args) < 2 {
		ctx.ReplyError("用法：/bili watch <uid/用户名> 或 /bili watch list")
		return
	}
	if strings.EqualFold(args[1], "list") || strings.EqualFold(args[1], "ls") {
		entries := p.watch.list(ctx.GetChatInfo().ID)
		if len(entries) == 0 {
			ctx.ReplyText("本群/会话暂无开播订阅")
			return
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📺 开播订阅 (%d):\n", len(entries)))
		for _, e := range entries {
			status := "未开播"
			if e.Living {
				status = "直播中"
			}
			sb.WriteString(fmt.Sprintf("• %s (UID: %d) - %s\n", e.Name, e.UID, status))
		}
		ctx.ReplyText(sb.String())
		return
	}

	input := args[1]
	mid, name, err := p.client.ResolveUID(reqCtx, input)
	if err != nil {
		ctx.ReplyText(fmt.Sprintf("[B站] %v", err))
		return
	}
	if p.watch.contains(ctx.GetChatInfo().ID, mid) {
		ctx.ReplyText(fmt.Sprintf("已在订阅中：%s (UID: %d)", name, mid))
		return
	}
	live, err := p.client.FetchLiveInfo(reqCtx, mid)
	if err != nil {
		ctx.ReplyText(fmt.Sprintf("[B站] 订阅失败: %v", err))
		return
	}
	p.registerNotifier(ctx)
	p.watch.add(ctx.GetChatInfo().ID, mid, name, live)
	ctx.ReplyText(fmt.Sprintf("已订阅开播通知：%s (UID: %d)%s", name, mid, func() string {
		if live.IsLiving {
			return "（当前正在直播）"
		}
		return ""
	}()))
}

// registerNotifier 注册主动推送能力（SessionNotifier）。
// 优先使用 Setup 阶段注入的 platform.Registry（重启后无需事件即可推送），
// 未注入时回退到从事件上下文获取 sender（平台支持 SessionNotifier 才可用）。
func (p *Plugin) registerNotifier(ctx *eventctx.Context) {
	if p.watch.hasNotifier() {
		return
	}
	if p.reg != nil {
		for _, a := range p.reg.All() {
			if sn, ok := a.Sender().(platform.SessionNotifier); ok {
				p.watch.setNotifier(func(chatID, msg string) error {
					return sn.NotifyGroup(context.Background(), chatID, platform.TextMessage(msg))
				})
				if p.log != nil {
					p.log.Infof("[bilibili] 开播通知已绑定平台 %s", a.Platform())
				}
				return
			}
		}
		if p.log != nil {
			p.log.Warnf("[bilibili] 当前平台不支持主动推送，开播通知将不会发送")
		}
		return
	}
	sender := ctx.GetPlatformSender()
	if sender == nil {
		return
	}
	sn, ok := sender.(platform.SessionNotifier)
	if !ok {
		p.log.Warnf("[bilibili] 当前平台不支持主动推送，开播通知将不会发送")
		return
	}
	p.watch.setNotifier(func(chatID, msg string) error {
		return sn.NotifyGroup(context.Background(), chatID, platform.TextMessage(msg))
	})
}

// handleUnwatch 处理 /bili unwatch <uid/用户名>。
func (p *Plugin) handleUnwatch(ctx *eventctx.Context, reqCtx context.Context, args []string) {
	if len(args) < 2 {
		ctx.ReplyError("用法：/bili unwatch <uid/用户名>")
		return
	}
	mid, name, err := p.client.ResolveUID(reqCtx, args[1])
	if err != nil {
		ctx.ReplyText(fmt.Sprintf("[B站] %v", err))
		return
	}
	if !p.watch.remove(ctx.GetChatInfo().ID, mid) {
		ctx.ReplyText(fmt.Sprintf("本群/会话未订阅：%s (UID: %d)", name, mid))
		return
	}
	ctx.ReplyText(fmt.Sprintf("已取消订阅：%s (UID: %d)", name, mid))
}

// watchLoop 后台轮询订阅的直播间状态，开播时向订阅群推送通知。
func (p *Plugin) watchLoop(lctx context.Context) {
	interval := p.watch.interval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 启动时先完成一轮状态初始化（避免把"已开播"当新开播推送）
	p.watchInitAll(lctx)
	for {
		select {
		case <-lctx.Done():
			return
		case <-ticker.C:
			p.watchPoll(lctx)
		}
	}
}

// watchInitAll 初始化所有订阅的当前直播状态（不推送）。
func (p *Plugin) watchInitAll(lctx context.Context) {
	for _, sub := range p.watch.all() {
		ctx, cancel := context.WithTimeout(lctx, 10*time.Second)
		live, err := p.client.FetchLiveInfo(ctx, sub.UID)
		cancel()
		if err != nil {
			p.log.Warnf("[bilibili] watch init %d failed: %v", sub.UID, err)
			continue
		}
		p.watch.setLiving(sub.ChatID, sub.UID, live.IsLiving)
	}
}

// watchPoll 检查一轮订阅状态，开播时推送通知。
func (p *Plugin) watchPoll(lctx context.Context) {
	subs := p.watch.all()
	if len(subs) == 0 {
		return
	}
	// 去重：同一 UID 只查一次，避免多次请求
	seen := make(map[int64]struct{})
	for _, sub := range subs {
		if _, ok := seen[sub.UID]; ok {
			continue
		}
		seen[sub.UID] = struct{}{}

		ctx, cancel := context.WithTimeout(lctx, 10*time.Second)
		live, err := p.client.FetchLiveInfo(ctx, sub.UID)
		cancel()
		if err != nil {
			p.log.Warnf("[bilibili] watch poll %d failed: %v", sub.UID, err)
			continue
		}
		p.watch.applyLive(sub.UID, live, p.pushToSubscribers)
	}
}

// pushToSubscribers 向订阅了该 UID 的所有会话推送开播通知。
func (p *Plugin) pushToSubscribers(uid int64, live *LiveInfo) {
	for _, sub := range p.watch.byUID(uid) {
		msg := fmt.Sprintf("📺 %s 开播了！\n标题: %s\n观看人数: %d\n直播间: https://live.bilibili.com/%d",
			sub.Name, live.Title, live.WatcherNum, live.RoomID)
		p.log.Infof("[bilibili] push live notify to chat %s for %s (uid %d)", sub.ChatID, sub.Name, uid)
		_ = p.notifyChat(sub.ChatID, msg)
	}
}

// notifyChat 向指定会话主动推送消息（尝试 SessionNotifier，失败仅记日志）。
func (p *Plugin) notifyChat(chatID string, msg string) error {
	nf, ok := p.watch.notifier.Load().(notifierFn)
	if !ok || nf == nil {
		return fmt.Errorf("no active session notifier")
	}
	return nf(chatID, msg)
}

// ListTools 返回可供 AI 调用的工具列表。实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "search_bilibili_user",
			Categories:  []string{"bilibili"},
			Description: "搜索 Bilibili UP 主，通过用户名关键词查找，返回用户名、UID、粉丝数、等级等",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"keyword": {Type: "string", Description: "搜索关键词，如 泠鸢、老番茄、LexBurner"},
				},
				Required: []string{"keyword"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				keyword, _ := args["keyword"].(string)
				if keyword == "" {
					return "", fmt.Errorf("keyword is required")
				}
				reqCtx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()

				results, _, err := p.client.SearchUser(reqCtx, keyword, 1)
				if err != nil {
					return "", err
				}
				if len(results) == 0 {
					return fmt.Sprintf("未找到与「%s」相关的用户", keyword), nil
				}
				return formatSearchUserText(results), nil
			},
		},
		{
			Name:        "get_bilibili_user_info",
			Categories:  []string{"bilibili"},
			Description: "获取 Bilibili UP 主信息，包含用户名、等级、粉丝数、签名等。支持 UID 数字或用户名搜索",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"uid": {Type: "string", Description: "UP 主的 UID 数字或用户名，如 282994 或 泠鸢yousa"},
				},
				Required: []string{"uid"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				uid, _ := args["uid"].(string)
				if uid == "" {
					return "", fmt.Errorf("uid is required")
				}
				reqCtx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()

				mid, _, err := p.client.ResolveUID(reqCtx, uid)
				if err != nil {
					return "", fmt.Errorf("无法解析 UID: %w", err)
				}
				user, rel, err := p.client.FetchUserInfo(reqCtx, mid)
				if err != nil {
					return "", err
				}
				return formatBiliUserText(user, rel), nil
			},
		},
		{
			Name:        "get_bilibili_live_status",
			Categories:  []string{"bilibili"},
			Description: "获取 Bilibili UP 主的直播状态，是否在直播、直播标题、观看人数等。支持 UID 数字、用户名或 room:<房间号> 前缀",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"uid": {Type: "string", Description: "UP 主的 UID 数字、用户名，或 room:<房间号>（如 room:47377）"},
				},
				Required: []string{"uid"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				uid, _ := args["uid"].(string)
				if uid == "" {
					return "", fmt.Errorf("uid is required")
				}
				reqCtx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()

				if strings.HasPrefix(strings.ToLower(uid), "room:") {
					roomID, rerr := strconv.ParseInt(strings.TrimPrefix(uid, "room:"), 10, 64)
					if rerr != nil {
						return "", fmt.Errorf("房间号格式不正确: %s", uid)
					}
					live, lerr := p.client.FetchLiveInfoByRoom(reqCtx, roomID)
					if lerr != nil {
						return "", lerr
					}
					return formatLiveText(live), nil
				}

				mid, _, err := p.client.ResolveUID(reqCtx, uid)
				if err != nil {
					return "", fmt.Errorf("无法解析 UID: %w", err)
				}
				live, err := p.client.FetchLiveInfo(reqCtx, mid)
				if err != nil {
					return "", err
				}
				return formatLiveText(live), nil
			},
		},
		{
			Name:        "get_bilibili_video_info",
			Categories:  []string{"bilibili"},
			Description: "获取 Bilibili 视频信息，包含标题、UP 主、播放量、弹幕、点赞、硬币、收藏、时长等",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"bvid": {Type: "string", Description: "视频 BV 号，如 BV1GJ411x7h7"},
				},
				Required: []string{"bvid"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				bvid, _ := args["bvid"].(string)
				if bvid == "" {
					return "", fmt.Errorf("bvid is required")
				}
				bvid = strings.ToUpper(bvid)
				if !strings.HasPrefix(bvid, "BV") {
					bvid = "BV" + bvid
				}
				reqCtx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()

				info, err := p.client.FetchVideoInfo(reqCtx, bvid)
				if err != nil {
					return "", err
				}
				return formatVideoText(info), nil
			},
		},
		{
			Name:        "get_bilibili_user_videos",
			Categories:  []string{"bilibili"},
			Description: "获取 Bilibili UP 主的最近投稿列表（最新 5 条）",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"uid": {Type: "string", Description: "UP 主的 UID 数字或用户名"},
				},
				Required: []string{"uid"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				uid, _ := args["uid"].(string)
				if uid == "" {
					return "", fmt.Errorf("uid is required")
				}
				reqCtx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()

				mid, name, err := p.client.ResolveUID(reqCtx, uid)
				if err != nil {
					return "", fmt.Errorf("无法解析 UID: %w", err)
				}
				items, err := p.client.FetchVideos(reqCtx, mid, 1)
				if err != nil {
					return "", err
				}
				if len(items) == 0 {
					return fmt.Sprintf("%s 暂无投稿", name), nil
				}
				return formatVideoListText(items, name), nil
			},
		},
		{
			Name:        "search_bilibili_bangumi",
			Categories:  []string{"bilibili"},
			Description: "搜索 Bilibili 番剧/影视，返回番名、地区、评分、集数等",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"keyword": {Type: "string", Description: "番剧关键词，如 间谍过家家、进击的巨人"},
				},
				Required: []string{"keyword"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				keyword, _ := args["keyword"].(string)
				if keyword == "" {
					return "", fmt.Errorf("keyword is required")
				}
				reqCtx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()

				results, err := p.client.SearchBangumi(reqCtx, keyword, 5)
				if err != nil {
					return "", err
				}
				if len(results) == 0 {
					return fmt.Sprintf("未找到与「%s」相关的番剧", keyword), nil
				}
				return formatBangumiText(results), nil
			},
		},
	}
}

// ListSkills 返回可供 AI 使用的技能列表。实现 ai.SkillProvider。
func (p *Plugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "bilibili_query",
			Description: "查询 Bilibili UP 主信息、直播状态、视频、番剧",
			Prompt:      "你是一个 B站 助手。当用户询问 B 站 UP 主信息或直播状态时，使用 search_bilibili_user 搜索用户，再用 get_bilibili_user_info 或 get_bilibili_live_status 获取详情；询问视频信息用 get_bilibili_video_info；询问 UP 主投稿用 get_bilibili_user_videos；询问番剧用 search_bilibili_bangumi。所有工具都支持直接输入用户名而非 UID。然后以自然语言总结。",
			Tools:       p.ListTools(),
		},
	}
}

// HealthCheckers 返回插件的 API 健康探针。实现 health.CheckProvider。
func (p *Plugin) HealthCheckers() []health.Checker {
	out := make([]health.Checker, len(p.probes))
	for i, pr := range p.probes {
		out[i] = pr
	}
	return out
}

// sessdata 返回真实账号的 SESSDATA Cookie（从浏览器登录态复制）。
// 匿名随机指纹 Cookie 极易触发 -799 风控限流，配置真实账号后可稳定访问；
// Setup 时读取一次（修改需重启）。
func (p *Plugin) sessdata() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("sessdata", "")
}

// proxy 返回 B 站请求的代理地址（如 "http://127.0.0.1:7890"）。
// 空值沿用环境变量代理或直连；Setup 时读取一次（修改需重启）。
func (p *Plugin) proxy() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("proxy", "")
}
