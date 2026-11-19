// Package wechat 实现企业微信智能机器人（AI Bot）的 platform.Adapter。
//
// # 为什么是"智能机器人"
//
// 企业微信侧有四种可用的接入形态，只有智能机器人能当作通用聊天机器人用：
//
//	群机器人(webhook)   只能单向推送，收不到任何消息
//	自建应用            能收私聊，但**收不到群消息**，群聊需由应用自己创建
//	智能机器人(URL 回调) 群聊 @机器人 / 单聊，回复走一次性 response_url
//	智能机器人(长连接)   同上，且支持主动推送、素材上传、模板卡片更新
//
// 因此本适配器落地的是「智能机器人」的 API 模式，两种传输都支持：
//
//   - ws（默认）：连 wss://openws.work.weixin.qq.com，30s 心跳，自动重连。
//     无需公网地址、无需加解密，功能最全。
//   - webhook：企业微信把加密 JSON 回调 POST 到本端，需要公网可达地址，
//     回复只能走回调里的 response_url（一次性、1 小时有效）。
//
// # 快速开始
//
//	adapter, err := wechat.NewAdapter(wechat.Config{
//	    BotID:  "BOT_ID",
//	    Secret: "LONG_CONNECTION_SECRET",
//	})
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
//
// # 一个机器人只能有一条长连接
//
// 服务端对同一 BotID 只保留一条有效长连接：新连接订阅成功会踢掉旧连接，
// 旧连接收到 disconnected_event 后被断开。多副本部署时**只能有一个副本**
// 去连（主备切换而非多活），否则两个进程会互相踢，表现为"消息随机丢"。
// 本适配器因此把 disconnected_event 当成致命错误上报，而不是无脑重连——
// 重连只会让两边互相踢得更有节奏。
package wechat

import (
	stdctx "context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// PlatformID 是平台标识符（与 config.Bot.WeChat 的配置键、注册表键一致）。
const PlatformID = "wechat"

const (
	// healthyConnDuration 是判定一次连接"健康"的最短存活时长。
	// 超过该时长后断线视为偶发故障，重连计数与退避从头开始。
	healthyConnDuration = 60 * time.Second
)

// Adapter 是企业微信智能机器人的 platform.Adapter 实现。
//
// 实现的可选接口：
//   - platform.BotIdentity        （BotID 来自配置）
//   - platform.RecoverableAdapter （断开/重连通知）
//   - platform.HealthDetailer     （连接状态、模式、重连次数）
//   - platform.MediaOriginProvider（企业微信媒体直链域名）
type Adapter struct {
	platform.DisconnectNotifier

	cfg     Config
	webhook *webhookAdapter
	sender  *wechatSender

	mu      sync.RWMutex
	client  *wsClient // ws 模式下当前连接（重连时替换）
	running bool
	cancel  stdctx.CancelFunc

	starting atomic.Bool
	botID    atomic.Value // string
	attempts atomic.Int64 // 累计重连次数，仅用于 HealthDetail

	// kicked 表示已被新连接踢下线：此时不应重连。
	kicked atomic.Bool
}

// NewAdapter 按配置构造适配器（不触发网络）。
//
// 构造阶段只做校验与默认值填充：真正的连接/监听在 Start 中建立，
// 这样配置写错时能在装配阶段就失败，而不是等 Registry 异步启动后才在日志里出现。
func NewAdapter(cfg Config) (*Adapter, error) {
	if cfg.BotID == "" {
		return nil, fmt.Errorf("wechat adapter: bot_id 不能为空（企业微信智能机器人 BotID）")
	}
	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	a := &Adapter{cfg: cfg}
	a.botID.Store(cfg.BotID)
	a.sender = newSender(a)

	if cfg.ResolveMode() == ModeWebhook {
		crypt, err := newWXCrypt(cfg.Token, cfg.EncodingAESKey, "")
		if err != nil {
			return nil, err
		}
		a.webhook = newWebhookAdapter(cfg, crypt, a.sender)
	}
	return a, nil
}

// Platform 返回 "wechat"。
func (a *Adapter) Platform() string { return PlatformID }

// Sender 返回企业微信消息发送器。
func (a *Adapter) Sender() platform.Sender { return a.sender }

// Capabilities 返回企业微信智能机器人的能力集合。
//
// FileUpload 依赖传输方式：只有长连接提供分片上传接口，URL 回调模式无法
// 上传素材，因此没有 media_id 就拿不到可发送的图片/文件。
func (a *Adapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Markdown:        true, // markdown 消息体，正文支持常见 markdown
		Buttons:         true, // 按钮交互型模板卡片
		MultiAttachment: false,
		MessageEdit:     false,
		MessageDelete:   false,
		Embeds:          false,
		FileUpload:      a.cfg.ResolveMode() == ModeWS,
		GuildSupport:    false,
		Reactions:       false,
		ThreadReply:     false, // 主动回复会自带引用，无法指定引用目标
		TypingIndicator: false,
		MentionAll:      false,
		VoiceChannel:    false,
		Caption:         false, // 正文与媒体是两条独立消息
		Forward:         false,
		MaxTextLength:   20480, // markdown.content 上限（字节）
		MaxAttachmentMB: 20,    // 普通文件上限；图片 10MB、语音 2MB、视频 10MB
	}
}

// IsRunning 报告适配器是否在运行。
func (a *Adapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Start 启动事件循环（ws 模式阻塞在连接循环，webhook 模式阻塞在 HTTP 服务）。
func (a *Adapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
	if !a.starting.CompareAndSwap(false, true) {
		return nil
	}
	defer a.starting.Store(false)

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	cancelCtx, cancel := stdctx.WithCancel(ctx)
	a.cancel = cancel
	a.running = true
	a.mu.Unlock()

	// 每次启动都清掉上次"被踢下线"的标记：Registry 热替换可能用同一个实例
	// 重新 Start，残留的标记会让新实例一连接就放弃重连。
	a.kicked.Store(false)

	defer func() {
		a.mu.Lock()
		a.running = false
		a.client = nil
		a.mu.Unlock()
		cancel()
	}()

	if a.cfg.ResolveMode() == ModeWebhook {
		return a.webhook.Start(cancelCtx, handler)
	}
	return a.runWS(cancelCtx, handler)
}

// Stop 优雅停止适配器。
func (a *Adapter) Stop(_ stdctx.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	client := a.client
	hook := a.webhook
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if client != nil {
		_ = client.Close()
	}
	if hook != nil {
		// 关闭 HTTP 服务必须带超时：Background() 会无限等待在途请求。
		shutCtx, shutCancel := stdctx.WithTimeout(stdctx.Background(), 10*time.Second)
		defer shutCancel()
		return hook.Stop(shutCtx)
	}
	return nil
}

// ── platform.BotIdentity ─────────────────────────────────────────────────────

// BotID 返回配置里的智能机器人 BotID。
func (a *Adapter) BotID() string {
	if v, ok := a.botID.Load().(string); ok {
		return v
	}
	return ""
}

// BotName 返回机器人显示名称。
//
// 企业微信没有"查询机器人资料"的接口，名称只能人工配置；未配置时返回空串
// （平台允许 BotName 为空）。
func (a *Adapter) BotName() string { return "" }

// ── platform.HealthDetailer ──────────────────────────────────────────────────

// HealthDetail 返回运行时健康信息。
func (a *Adapter) HealthDetail() map[string]any {
	mode := a.cfg.ResolveMode()
	detail := map[string]any{
		"connection":  mode,
		"bot_id":      a.BotID(),
		"reconnect_n": a.attempts.Load(),
	}
	if mode == ModeWS {
		a.mu.RLock()
		connected := a.client != nil
		a.mu.RUnlock()
		detail["subscribed"] = connected
		if a.kicked.Load() {
			detail["kicked_by_new_connection"] = true
		}
	} else if a.webhook != nil {
		detail["listen_addr"] = a.cfg.Webhook.ListenAddr
		detail["path"] = a.cfg.Webhook.Path
	}
	return detail
}

// ── platform.MediaOriginProvider ─────────────────────────────────────────────

// MediaOrigins 声明企业微信媒体直链的来源域名。
//
// 入站附件直链指向腾讯云 COS，出站策略默认只放行公网 https，本声明让下载侧
// 知道这些主机是平台自托管的，不必让运维在每个插件配置里手抄。
//
// 注意直链 **5 分钟过期且内容加密**（AES-256-CBC），必须配合附件
// Extra[ExtraKeyAesKey] 用 [DecryptMedia] 解密；不要把 URL 持久化。
func (a *Adapter) MediaOrigins() []string {
	return []string{
		// 智能机器人入站媒体：https://ww-aibot-img-<appid>.cos.<region>.myqcloud.com
		"ww-aibot-img-1258476243.cos.ap-guangzhou.myqcloud.com",
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 长连接事件循环
// ────────────────────────────────────────────────────────────────────────────

// runWS 维护长连接：拨号 → 订阅 → 读循环，断开后指数退避重连。
func (a *Adapter) runWS(ctx stdctx.Context, handler func(platform.Event)) error {
	delay := a.cfg.ReconnectDelay
	attempts := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		startedAt := time.Now()
		err := a.runConnection(ctx, handler)
		if err == nil || errors.Is(err, stdctx.Canceled) || errors.Is(err, stdctx.DeadlineExceeded) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		a.NotifyDisconnect(err)

		// 被新连接踢掉：重连只会和对方互相踢，必须停下来等人工处理。
		if a.kicked.Load() {
			logger.WithFields(logger.Fields{"platform": PlatformID}).
				Error("[wechat.Adapter] 已被其他连接踢下线（同一 BotID 只允许一条长连接），停止重连")
			return fmt.Errorf("wechat: 长连接被新连接接管，已停止重连（同一机器人只允许一条长连接）: %w", err)
		}

		// 稳定存活过一段时间才重置退避：否则"握手成功立刻被拒"的部署
		// 会让退避永远涨不上去，退化成固定节奏的重连风暴。
		if time.Since(startedAt) >= healthyConnDuration {
			attempts = 0
			delay = a.cfg.ReconnectDelay
		} else {
			attempts++
			a.attempts.Add(1)
			if a.cfg.MaxReconnect > 0 && attempts > a.cfg.MaxReconnect {
				return fmt.Errorf("wechat: 超过最大重连次数 (%d): %w", a.cfg.MaxReconnect, err)
			}
		}

		logger.WithFields(logger.Fields{
			"platform": PlatformID,
			"attempt":  attempts,
			"delay":    delay,
		}).WithError(err).Warn("[wechat.Adapter] 长连接断开，准备重连")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay = minDuration(delay*2, defaultMaxReconnectDelay)
	}
}

// runConnection 建立一条连接并跑完它的生命周期。
//
// 顺序很重要：**读循环必须先起来**，再发 aibot_subscribe。回执帧只能经
// readLoop 分发到等待队列，先订阅后读的写法会让 subscribe 自己等到超时
// （一条健康的连接在启动阶段就被判成"订阅失败"）。
func (a *Adapter) runConnection(ctx stdctx.Context, handler func(platform.Event)) error {
	client := newWSClient(a.cfg, a.handleFrame(ctx, handler))

	if err := client.dial(ctx); err != nil {
		return err
	}
	a.setClient(client)
	defer a.setClient(nil)
	defer func() { _ = client.Close() }()

	readCtx, cancelRead := stdctx.WithCancel(ctx)
	defer cancelRead()

	readErr := make(chan error, 1)
	go func() { readErr <- client.readLoop(readCtx) }()

	if err := client.subscribe(ctx); err != nil {
		cancelRead()
		// 读循环因连接关闭而退出；它的错误只是订阅失败的副作用，
		// 这里只等它退出（否则 Close 后仍可能在途写日志）。
		if readErr := <-readErr; readErr != nil {
			logger.WithError(readErr).Debug("[wechat.Adapter] 订阅失败后读循环退出")
		}
		return err
	}

	pingCtx, cancelPing := stdctx.WithCancel(ctx)
	defer cancelPing()
	go client.pingLoop(pingCtx)

	logger.WithFields(logger.Fields{
		"platform": PlatformID,
		"bot_id":   a.BotID(),
	}).Info("[wechat.Adapter] 长连接已订阅")

	return <-readErr
}

// setClient 发布/清除当前连接（发送侧据此判断能否回复）。
func (a *Adapter) setClient(c *wsClient) {
	a.mu.Lock()
	a.client = c
	a.mu.Unlock()
}

// currentClient 返回当前连接（未连接时为 nil）。
func (a *Adapter) currentClient() *wsClient {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.client
}

// handleFrame 把下行帧转成 platform.Event 并交给 handler。
func (a *Adapter) handleFrame(_ stdctx.Context, handler func(platform.Event)) func(string, jsonx.RawMessage) {
	return func(reqID string, body jsonx.RawMessage) {
		var cb Callback
		if err := jsonx.Unmarshal(body, &cb); err != nil {
			logger.WithError(err).Warn("[wechat.Adapter] 解析回调失败，跳过")
			return
		}

		// 被踢下线：标记后关闭连接，由重连循环上报致命错误。
		if cb.MsgType == msgTypeEvent && cb.Event != nil && cb.Event.EventType == eventTypeDisconnected {
			a.kicked.Store(true)
			logger.WithFields(logger.Fields{
				"platform": PlatformID,
				"aibotid":  cb.AIBotID,
			}).Warn("[wechat.Adapter] 收到 disconnected_event：本连接已被新连接接管")
			if c := a.currentClient(); c != nil {
				_ = c.Close()
			}
			return
		}

		evt := newEvent(&cb, a.BotID())
		if evt == nil {
			logger.WithFields(logger.Fields{
				"platform": PlatformID,
				"msgtype":  cb.MsgType,
				"chattype": cb.ChatType,
			}).Debug("[wechat.Adapter] 不支持的回调类型，忽略")
			return
		}
		// 回复通道随事件一起走：ws 模式是回执 req_id，webhook 模式是 response_url。
		attachReplyTokens(evt, cb.MsgID, reqID)
		platform.SafeDispatch(handler, evt)
	}
}

// attachReplyTokens 把回复所需的授权信息写入事件的 ChatInfo.Tokens。
//
// 框架层的 Sender 只认 Tokens，不感知传输方式：
//   - ws：TokenReplyReqID —— 回调帧的 req_id，回 aibot_respond_msg 时原样透传
//   - webhook：由 webhookAdapter 写入 TokenResponseURL
//
// 注意 req_id 来自**帧头**而不是 body，是长连接模式唯一的回复授权，因此必须
// 在这里落进事件（回调 body 里没有等价字段）。
func attachReplyTokens(evt platform.Event, msgID, reqID string) {
	we, ok := evt.(*wechatEvent)
	if !ok {
		return
	}
	if we.chat.Tokens == nil {
		we.chat.Tokens = make(map[string]string, 2)
	}
	if msgID != "" {
		we.chat.Tokens[TokenMsgID] = msgID
	}
	if reqID != "" {
		we.chat.Tokens[TokenReplyReqID] = reqID
	}
}

// minDuration 返回两个时长中较小的一个。
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// 编译期接口检查。
var (
	_ platform.Adapter             = (*Adapter)(nil)
	_ platform.BotIdentity         = (*Adapter)(nil)
	_ platform.RecoverableAdapter  = (*Adapter)(nil)
	_ platform.HealthDetailer      = (*Adapter)(nil)
	_ platform.MediaOriginProvider = (*Adapter)(nil)
)
