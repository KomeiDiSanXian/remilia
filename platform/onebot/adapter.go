package onebot

import (
	stdctx "context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/gorilla/websocket"
)

// healthyConnDuration 是判定一次连接"健康"的最短存活时长。
// 超过该时长后断线视为偶发故障，重连退避从头开始而非继续放大。
const healthyConnDuration = 60 * time.Second

// ────────────────────────────────────────────────────────────────────────────
// ForwardWSAdapter
// ────────────────────────────────────────────────────────────────────────────

// ForwardWSAdapter 是一个 platform.Adapter，它主动连接到 OneBot V11 实现的
// WebSocket 服务端并从中接收事件。
//
// 支持 "/" 组合端点，该端点在单个连接上同时提供事件推送和 API 调用能力。
//
// 通信流程：
//
//	adapter ──WS 连接──▶ OneBot 实现（go-cqhttp / NapCat / …）
//	adapter ◀──── 事件 ──  OneBot 实现
//	adapter ──── API 调用 ▶  OneBot 实现  （通过 wsAPIClient）
//
// 使用示例：
//
//	adapter := onebot.NewForwardWSAdapter(onebot.DefaultConfig("ws://127.0.0.1:6700"))
type ForwardWSAdapter struct {
	platform.DisconnectNotifier

	config    Config
	sender    *onebotSender
	apiClient *wsAPIClient // 与 sender 共享，重连时更新

	mu      sync.RWMutex
	conn    *websocket.Conn
	running bool
	cancel  stdctx.CancelFunc

	starting atomic.Bool
	wg       sync.WaitGroup

	// 连接成功后设置的机器人身份信息
	botID   string
	botName string
}

// NewForwardWSAdapter 使用给定的 Config 创建 ForwardWSAdapter。
func NewForwardWSAdapter(cfg Config) *ForwardWSAdapter {
	cfg.setDefaults()
	a := &ForwardWSAdapter{config: cfg}
	return a
}

// NewAdapter 是使用 DefaultConfig 的便捷构造函数。
func NewAdapter(url string) *ForwardWSAdapter {
	return NewForwardWSAdapter(DefaultConfig(url))
}

// ── platform.Adapter ────────────────────────────────────────────────────────

// Platform 返回 "onebot"。
func (a *ForwardWSAdapter) Platform() string { return PlatformID }

// Sender 返回 OneBot 消息发送器。
func (a *ForwardWSAdapter) Sender() platform.Sender {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.sender != nil {
		return a.sender
	}
	return &platform.NoopSender{}
}

// onebotCapabilities 返回 OneBot V11 平台的功能集。
// 所有三种通信模式（ForwardWS / HTTPPost / ReverseWS）共享相同的能力。
func onebotCapabilities() platform.Capabilities {
	return platform.Capabilities{
		MessageDelete:   true,
		ThreadReply:     true,
		MentionAll:      true,
		Reactions:       false,
		MessageEdit:     false,
		MultiAttachment: false,
		FileUpload:      false,
		VoiceChannel:    false,
	}
}

// Capabilities 返回 OneBot V11 平台的功能集。
func (a *ForwardWSAdapter) Capabilities() platform.Capabilities { return onebotCapabilities() }

// IsRunning 当 WS 连接处于活跃状态时返回 true。
func (a *ForwardWSAdapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Start 建立 WebSocket 连接并开始处理事件。
//
// 阻塞直到 ctx 被取消。连接断开后使用指数退避自动重连。
func (a *ForwardWSAdapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
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

	logger.Infof("[onebot.ForwardWSAdapter] Starting, connecting to %s", a.config.URL)

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		cancel()
	}()

	a.runWithReconnect(cancelCtx, handler)
	return nil
}

// Stop 优雅地关闭 WebSocket 连接。
func (a *ForwardWSAdapter) Stop(_ stdctx.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	conn := a.conn
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		// 必须用 WriteControl 而非 WriteMessage：gorilla/websocket 只允许一个并发写者，
		// 而此刻可能有 handler 正在 wsAPIClient.Call 里向同一连接写数据帧
		// （其写锁是 apiClient 私有的，管不到这里），并发 WriteMessage 会
		// panic("concurrent write to websocket connection")。
		// WriteControl 是唯一可安全并发调用的写方法。
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(5*time.Second))
		_ = conn.Close()
	}
	a.wg.Wait()
	return nil
}

// ── platform.BotIdentity ────────────────────────────────────────────────────

// BotID 返回机器人的 QQ 号（连接成功后获取）。
func (a *ForwardWSAdapter) BotID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.botID
}

// BotName 返回机器人的昵称（连接成功后获取）。
func (a *ForwardWSAdapter) BotName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.botName
}

// ── platform.HealthDetailer ──────────────────────────────────────────────────

func (a *ForwardWSAdapter) HealthDetail() map[string]any {
	detail := map[string]any{
		"connection": "forward_websocket",
	}
	a.mu.RLock()
	detail["connected"] = a.conn != nil
	a.mu.RUnlock()
	return detail
}

// 编译时断言
var (
	_ platform.RecoverableAdapter = (*ForwardWSAdapter)(nil)
	_ platform.BotIdentity        = (*ForwardWSAdapter)(nil)
	_ platform.HealthDetailer     = (*ForwardWSAdapter)(nil)
)

// ────────────────────────────────────────────────────────────────────────────
// 重连循环
// ────────────────────────────────────────────────────────────────────────────

// runWithReconnect 使用指数退避运行连接-接收循环。
func (a *ForwardWSAdapter) runWithReconnect(ctx stdctx.Context, handler func(platform.Event)) {
	delay := a.config.ReconnectDelay
	maxDelay := a.config.ReconnectMaxDelay
	attempt := 0

	for {
		if ctx.Err() != nil {
			return
		}

		if attempt > 0 {
			// 指数退避（限制指数上界，避免 float64→Duration 溢出为负值导致零延迟热重连循环）
			exp := min(attempt-1, 40)
			backoff := min(time.Duration(float64(delay)*math.Pow(1.5, float64(exp))), maxDelay)
			logger.WithFields(logger.Fields{
				"attempt": attempt,
				"delay":   backoff,
			}).Warn("[onebot.ForwardWSAdapter] Reconnecting...")

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
		}

		connectedAt := time.Now()
		err := a.runOnce(ctx, handler)
		if ctx.Err() != nil {
			// ctx 已取消，正常关闭
			return
		}
		if err != nil {
			logger.WithError(err).Warn("[onebot.ForwardWSAdapter] Connection lost")
			a.NotifyDisconnect(err)
			// 上一次连接稳定存活足够久，视为健康，重置退避。
			//
			// 不能只在 err == nil 时重置：runOnce 仅在 ctx 取消时才返回 nil，
			// 而那种情况上面已经 return，因此 attempt 会随进程生命周期单调
			// 递增——运行数周后一次普通断线也要等满 maxDelay 才重连。
			if time.Since(connectedAt) >= healthyConnDuration {
				attempt = 1
			} else {
				attempt++
			}
		} else {
			attempt = 0
		}
	}
}

// runOnce 建立单次 WS 连接并持续处理消息，直到断线或 ctx 取消。
func (a *ForwardWSAdapter) runOnce(ctx stdctx.Context, handler func(platform.Event)) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}

	reqHeader := http.Header{}
	if a.config.Token != "" {
		reqHeader.Set("Authorization", "Bearer "+a.config.Token)
	}

	conn, _, err := dialer.DialContext(ctx, a.config.URL, reqHeader)
	if err != nil {
		return fmt.Errorf("onebot: dial %s: %w", a.config.URL, err)
	}

	logger.Infof("[onebot.ForwardWSAdapter] Connected to %s", a.config.URL)

	// 限制单帧大小，避免异常/恶意对端声明超大帧导致 OOM。
	conn.SetReadLimit(maxWSMessageBytes)

	// ctx 取消看门狗：ReadMessage 无法被 ctx 中断，必须显式关闭 socket 才能
	// 唤醒读循环。否则对端静默时 Start 永不返回，Stop 也会卡在 wg.Wait()。
	//
	// 该 goroutine 刻意不加入 a.wg：本函数结尾会调用 a.wg.Wait()，而看门狗
	// 要等到 connClosed 关闭才退出，两者互等会直接死锁。它由下方
	// close(connClosed) 显式收尾，不会泄漏。
	connClosed := make(chan struct{})
	defer close(connClosed)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-connClosed:
		}
	}()

	// 为此次连接创建新的 API 客户端
	apiClient := newWSAPIClient(conn, a.config.APITimeout)
	sender := newSender(apiClient)

	a.mu.Lock()
	a.conn = conn
	a.apiClient = apiClient
	a.sender = sender
	a.mu.Unlock()

	// 启动事件泵 goroutine
	eventCh := make(chan platform.Event, a.config.EventBufferSize)
	errCh := make(chan error, 1)

	a.wg.Go(func() {
		errCh <- a.receiveLoop(ctx, conn, apiClient, eventCh)
	})

	// 获取一次机器人身份。
	//
	// 必须放在 receiveLoop 启动之后：get_login_info 的响应由 receiveLoop 经
	// apiClient.routeResponse 派发，若在此之前同步调用，响应永远无法送达，
	// 必然空等满 APITimeout（每次连接/重连都白白阻塞数秒），
	// 且 botID 始终为空，导致 IsFromSelf() 永不命中、可能自我回复成环。
	// 这里用独立 goroutine 执行，避免拖慢首个事件的处理。
	a.wg.Go(func() {
		a.fetchBotIdentity(ctx, sender)
	})

	// 将事件分发给 handler
	a.wg.Go(func() {
		for {
			select {
			case ev, ok := <-eventCh:
				if !ok {
					return
				}
				platform.SafeDispatch(handler, ev)
			case <-ctx.Done():
				return
			}
		}
	})

	// 等待接收循环结束
	receiveErr := <-errCh
	close(eventCh)
	a.wg.Wait()

	// 清理连接
	a.mu.Lock()
	a.conn = nil
	a.mu.Unlock()

	_ = conn.Close()
	return receiveErr
}

// receiveLoop 读取 WebSocket 消息，将其路由到事件或 API 响应。
func (a *ForwardWSAdapter) receiveLoop(
	ctx stdctx.Context,
	conn *websocket.Conn,
	apiClient *wsAPIClient,
	eventCh chan<- platform.Event,
) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("onebot: read: %w", err)
		}

		// 判断这是 API 响应（有 "status" 和 "retcode"）还是事件
		if isAPIResponse(msg) {
			apiClient.routeResponse(msg)
			continue
		}

		ev, err := parseEvent(msg)
		if err != nil {
			logger.WithError(err).Warn("[onebot.ForwardWSAdapter] Failed to parse event")
			continue
		}

		if ev.Kind() == platform.EventKindUnknown {
			// silently drop unmapped events
			continue
		}

		select {
		case eventCh <- ev:
		case <-ctx.Done():
			return nil
		}
	}
}

// fetchBotIdentity 调用 get_login_info 以填充 botID 和 botName。
func (a *ForwardWSAdapter) fetchBotIdentity(ctx stdctx.Context, s *onebotSender) {
	fetchCtx, cancel := stdctx.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	info, err := s.GetLoginInfo(fetchCtx)
	if err != nil {
		logger.WithError(err).Debug("[onebot.ForwardWSAdapter] Could not fetch login info")
		return
	}
	a.mu.Lock()
	a.botID = strconv.FormatInt(info.UserID, 10)
	a.botName = info.Nickname
	a.mu.Unlock()
	logger.Infof("[onebot.ForwardWSAdapter] Bot identity: %s (%s)", info.Nickname, a.botID)
}

// ────────────────────────────────────────────────────────────────────────────
// 辅助函数
// ────────────────────────────────────────────────────────────────────────────

// isAPIResponse 当 JSON 负载看起来像 API 响应（含 "status" 和 "retcode" 字段）
// 而非事件时返回 true。
func isAPIResponse(msg []byte) bool {
	var probe struct {
		Status  *string `json:"status"`
		Retcode *int    `json:"retcode"`
		Echo    *string `json:"echo"`
	}
	if err := json.Unmarshal(msg, &probe); err != nil {
		return false
	}
	// API 响应含 retcode；事件含 post_type
	return probe.Retcode != nil || probe.Echo != nil
}
