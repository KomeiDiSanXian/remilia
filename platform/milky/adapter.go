package milky

import (
	stdctx "context"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/gorilla/websocket"
)

// PlatformID 是 Milky (QQ) 平台的唯一标识符。
const PlatformID = "milky"

// milkyCapabilities 返回 Milky 平台的能力集合。
func milkyCapabilities() platform.Capabilities {
	return platform.Capabilities{
		Markdown:        false, // Milky 协议没有原生的 Markdown 类型
		Buttons:         false,
		MultiAttachment: false, // QQ 通常每条消息只允许一个媒体附件
		MessageEdit:     false,
		MessageDelete:   true,
		Embeds:          false,
		FileUpload:      true,
		GuildSupport:    false,
		Reactions:       true,
		ThreadReply:     true,
		TypingIndicator: false,
		MentionAll:      true,
		VoiceChannel:    false,
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 适配器
// ────────────────────────────────────────────────────────────────────────────

// Adapter 是 Milky QQ 协议适配器。
//
// 通过 WebSocket 连接到 ws://{host}/event，
// 通过 POST http://{host}/api/{endpoint} 调用 HTTP API。
//
// 适配器在意外断开连接时会自动重连。
//
// 使用示例：
//
//	adapter, err := milky.NewAdapter(milky.Config{
//	    BaseURL:     "http://127.0.0.1:6700",
//	    AccessToken: "your-token",
//	})
//	bot, _ := remilia.NewBotBuilder().WithPlatformAdapter(adapter).Build()
type Adapter struct {
	platform.DisconnectNotifier

	cfg     Config
	client  *milkyClient
	sender  *milkySender
	workers int

	mu       sync.RWMutex
	running  bool
	cancel   stdctx.CancelFunc
	starting atomic.Bool

	// 机器人身份信息（首次连接时通过 get_login_info 填充）
	botID   infraatomic.Value[string]
	botName infraatomic.Value[string]
}

// NewAdapter 使用给定配置创建 Adapter。
//
// 适配器在调用 Start 之前不会建立连接。
func NewAdapter(cfg Config) (*Adapter, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("milky: Config.BaseURL must not be empty")
	}
	cfg = cfg.withDefaults()

	workers := cfg.WorkerCount
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	client := newMilkyClient(cfg)
	return &Adapter{
		cfg:     cfg,
		client:  client,
		sender:  newSender(client),
		workers: workers,
	}, nil
}

// Platform 返回 "milky"。
func (a *Adapter) Platform() string { return PlatformID }

// Sender 返回 Milky 消息发送器。
func (a *Adapter) Sender() platform.Sender { return a.sender }

// Capabilities 返回 Milky 平台能力集合。
func (a *Adapter) Capabilities() platform.Capabilities { return milkyCapabilities() }

// IsRunning 若 WebSocket 连接处于活跃状态则返回 true。
func (a *Adapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// BotID 实现 platform.BotIdentity，返回机器人的 QQ 号。
func (a *Adapter) BotID() string {
	return a.botID.Load()
}

// BotName 实现 platform.BotIdentity，返回机器人的 QQ 昵称。
func (a *Adapter) BotName() string {
	return a.botName.Load()
}

// ────────────────────────────────────────────────────────────────────────────
// platform.Adapter 生命周期
// ────────────────────────────────────────────────────────────────────────────

// Start 连接到 Milky 服务端并开始处理事件。
//
// 阻塞直到 ctx 被取消或发生致命错误。
// 根据 Config.ReconnectDelay 和 Config.MaxReconnect 配置，在网络断开时自动重连。
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

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		cancel()
	}()

	// 在后台获取机器人身份信息——尽力而为，不阻塞
	go a.fetchBotIdentity(cancelCtx)

	// 工作协程池
	bufSize := a.cfg.EventBufferSize
	if bufSize <= 0 {
		bufSize = 128
	}
	eventCh := make(chan platform.Event, bufSize)
	workCh := make(chan platform.Event, a.workers*2)

	var wg sync.WaitGroup
	for i := 0; i < a.workers; i++ {
		wg.Go(func() {
			for evt := range workCh {
				safeInvoke(handler, evt)
			}
		})
	}

	// 分发器：eventCh → workCh
	wg.Go(func() {
		defer close(workCh)
		for {
			select {
			case evt, ok := <-eventCh:
				if !ok {
					return
				}
				select {
				case workCh <- evt:
				case <-cancelCtx.Done():
					return
				}
			case <-cancelCtx.Done():
				return
			}
		}
	})

	// WebSocket 事件循环（含重连逻辑）
	err := a.eventLoop(cancelCtx, eventCh)

	close(eventCh)
	wg.Wait()

	return err
}

// Stop 优雅地断开适配器连接。
func (a *Adapter) Stop(_ stdctx.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// WebSocket 事件循环
// ────────────────────────────────────────────────────────────────────────────

// eventLoop 连接到 Milky WebSocket 端点并将事件转发到 eventCh。
// 使用指数退避自动重连，直到 ctx 结束或超过 MaxReconnect 次数。
func (a *Adapter) eventLoop(ctx stdctx.Context, eventCh chan<- platform.Event) error {
	wsURL := buildWSURL(a.cfg.BaseURL, a.cfg.AccessToken)
	delay := a.cfg.ReconnectDelay
	maxDelay := 60 * time.Second
	attempts := 0

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		conn, err := a.dial(ctx, wsURL)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			logger.WithFields(logger.Fields{"platform": PlatformID}).
				WithError(err).
				Warn("[milky.Adapter] WebSocket dial failed, will retry")
			a.NotifyDisconnect(err)

			attempts++
			if a.cfg.MaxReconnect > 0 && attempts > a.cfg.MaxReconnect {
				return fmt.Errorf("milky: exceeded max reconnect attempts (%d): %w", a.cfg.MaxReconnect, err)
			}
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
			delay = minDuration(delay*2, maxDelay)
			continue
		}

		attempts = 0
		delay = a.cfg.ReconnectDelay
		logger.Infof("[milky.Adapter] WebSocket connected to %s (workers=%d)", a.cfg.BaseURL, a.workers)

		disconnectErr := a.readLoop(ctx, conn, eventCh)
		conn.Close()

		if ctx.Err() != nil {
			return ctx.Err()
		}

		logger.WithFields(logger.Fields{"platform": PlatformID}).
			WithError(disconnectErr).
			Warn("[milky.Adapter] WebSocket disconnected, reconnecting...")
		a.NotifyDisconnect(disconnectErr)

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		delay = minDuration(delay*2, maxDelay)
	}
}

// dial 建立到 Milky 事件端点的 WebSocket 连接。
func (a *Adapter) dial(ctx stdctx.Context, wsURL string) (*websocket.Conn, error) {
	dialCtx, cancel := stdctx.WithTimeout(ctx, a.cfg.DialTimeout)
	defer cancel()

	dialer := websocket.Dialer{HandshakeTimeout: a.cfg.DialTimeout}
	header := http.Header{}
	if a.cfg.AccessToken != "" {
		header.Set("Authorization", "Bearer "+a.cfg.AccessToken)
	}
	conn, _, err := dialer.DialContext(dialCtx, wsURL, header)
	if err != nil {
		return nil, fmt.Errorf("milky: dial %s: %w", wsURL, err)
	}
	return conn, nil
}

// readLoop 从 WebSocket 连接中持续读取 JSON 事件，直到连接关闭或 ctx 被取消。
func (a *Adapter) readLoop(ctx stdctx.Context, conn *websocket.Conn, eventCh chan<- platform.Event) error {
	readDone := make(chan error, 1)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				readDone <- err
				return
			}

			evt, parseErr := parseRawEvent(msg)
			if parseErr != nil {
				logger.WithError(parseErr).Warn("[milky.Adapter] Failed to parse event, skipping")
				continue
			}

			select {
			case eventCh <- evt:
			case <-ctx.Done():
				readDone <- ctx.Err()
				return
			}
		}
	}()

	select {
	case err := <-readDone:
		return err
	case <-ctx.Done():
		_ = conn.Close()
		<-readDone // 等待读取 goroutine 退出
		return ctx.Err()
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 辅助函数
// ────────────────────────────────────────────────────────────────────────────

// fetchBotIdentity 调用 get_login_info 并缓存机器人的 QQ 号和昵称。
func (a *Adapter) fetchBotIdentity(ctx stdctx.Context) {
	var out getLoginInfoOutput
	if err := a.client.call(ctx, "get_login_info", struct{}{}, &out); err != nil {
		logger.WithError(err).Warn("[milky.Adapter] Failed to fetch bot identity")
		return
	}
	a.botID.Store(fmt.Sprintf("%d", out.Uin))
	a.botName.Store(out.Nickname)
	logger.Infof("[milky.Adapter] Bot identity: QQ=%d nickname=%s", out.Uin, out.Nickname)
}

// buildWSURL 将 HTTP 基础 URL 转换为 WebSocket 事件 URL。
// 例如："http://127.0.0.1:6700" → "ws://127.0.0.1:6700/event"
func buildWSURL(baseURL, accessToken string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		// 回退：简单字符串替换
		ws := strings.Replace(baseURL, "http://", "ws://", 1)
		ws = strings.Replace(ws, "https://", "wss://", 1)
		if accessToken != "" {
			return ws + "/event?access_token=" + url.QueryEscape(accessToken)
		}
		return ws + "/event"
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = "/event"
	if accessToken != "" {
		q := u.Query()
		q.Set("access_token", accessToken)
		u.RawQuery = q.Encode()
	}
	// access_token 优先通过 Authorization 请求头传递，
	// 也支持通过 query 参数作为备用方案。
	return u.String()
}

// safeInvoke 调用 handler，并捕获 panic 以防止 worker 崩溃。
func safeInvoke(handler func(platform.Event), event platform.Event) {
	platform.SafeDispatch(handler, event)
}

// minDuration 返回两个时间段中较小的一个。
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// 编译期接口实现检查。
var _ platform.Adapter = (*Adapter)(nil)
var _ platform.RecoverableAdapter = (*Adapter)(nil)
var _ platform.BotIdentity = (*Adapter)(nil)

// 编译期检查 milkySender 是否实现了所有声明的可选接口。
var _ platform.Sender = (*milkySender)(nil)
var _ platform.MessageDeleter = (*milkySender)(nil)
var _ platform.GroupManager = (*milkySender)(nil)
var _ platform.AutoModerator = (*milkySender)(nil)
var _ platform.InvitationHandler = (*milkySender)(nil)
var _ platform.ReactionSender = (*milkySender)(nil)

// 编译期检查 milkyEvent 是否实现了所有声明的接口。
var _ platform.Event = (*milkyEvent)(nil)
var _ platform.RawEvent = (*milkyEvent)(nil)
var _ platform.ReplyEvent = (*milkyEvent)(nil)
var _ platform.MentionsEvent = (*milkyEvent)(nil)
