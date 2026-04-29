package onebot

import (
	stdctx "context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// ────────────────────────────────────────────────────────────────────────────
// HTTPPostAdapter
// ────────────────────────────────────────────────────────────────────────────

// HTTPPostAdapter 是一个 platform.Adapter，通过 HTTP POST 接收 OneBot V11 事件
// （即"正向 HTTP POST"通信模式）。
//
// 通信流程：
//
//	OneBot 实现 ──POST /──▶ adapter（本适配器）  [事件 JSON 正文]
//	OneBot 实现 ◀── 200 ── adapter               [可选的快速操作 JSON]
//	adapter ──── POST /:action ──▶ OneBot 实现 HTTP API 服务器
//
// OneBot 实现须配置：
//   - http_post.enable = true
//   - http_post.url = http://this-host:ListenAddr/
//
// API 调用（send_msg 等）发往 Config.URL（OneBot HTTP API 服务器）。
//
// 使用示例：
//
//	cfg := onebot.DefaultHTTPPostConfig(":8080", "http://127.0.0.1:5700")
//	adapter := onebot.NewHTTPPostAdapter(cfg)
type HTTPPostAdapter struct {
	config  Config
	sender  *onebotSender
	apiHTTP *httpAPIClient

	mu      sync.RWMutex
	running bool
	cancel  stdctx.CancelFunc
	server  *http.Server
	handler func(platform.Event)

	botID   string
	botName string

	starting atomic.Bool
	wg       sync.WaitGroup
}

// NewHTTPPostAdapter 使用给定的 Config 创建 HTTPPostAdapter。
func NewHTTPPostAdapter(cfg Config) *HTTPPostAdapter {
	apiHTTP := newHTTPAPIClient(cfg.URL, cfg.Token, cfg.apiTimeout())
	sender := newSender(apiHTTP)
	return &HTTPPostAdapter{
		config:  cfg,
		sender:  sender,
		apiHTTP: apiHTTP,
	}
}

// ── platform.Adapter ────────────────────────────────────────────────────────

// Platform 返回 "onebot"。
func (a *HTTPPostAdapter) Platform() string { return PlatformID }

// Sender 返回基于 HTTP 的 OneBot 消息发送器。
func (a *HTTPPostAdapter) Sender() platform.Sender { return a.sender }

// Capabilities 返回 OneBot V11 平台的功能集。
func (a *HTTPPostAdapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		MessageDelete: true,
		ThreadReply:   true,
		MentionAll:    true,
	}
}

// ── platform.BotIdentity ─────────────────────────────────────────────────────

// BotID 返回机器人的 QQ 号。
func (a *HTTPPostAdapter) BotID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.botID
}

// BotName 返回机器人的昵称。
func (a *HTTPPostAdapter) BotName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.botName
}

// IsRunning 当 HTTP 服务器处于活跃状态时返回 true。
func (a *HTTPPostAdapter) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// Start 开始监听 HTTP POST 事件。
// 阻塞直到 ctx 被取消。
func (a *HTTPPostAdapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
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
	a.handler = handler
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		cancel()
	}()

	listenAddr := a.config.ListenAddr
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	// 异步获取机器人身份信息（尽力而为）
	go a.fetchBotIdentity(cancelCtx)

	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handlePost)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: mux,
	}
	a.mu.Lock()
	a.server = srv
	a.mu.Unlock()

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("onebot http_post: listen %s: %w", listenAddr, err)
	}

	logger.Infof("[onebot.HTTPPostAdapter] Listening on %s", listenAddr)

	a.wg.Go(func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Error("[onebot.HTTPPostAdapter] Server error")
		}
	})

	<-cancelCtx.Done()

	shutCtx, shutCancel := stdctx.WithTimeout(stdctx.Background(), 5*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)

	a.wg.Wait()
	return nil
}

// Stop 关闭 HTTP 服务器。
func (a *HTTPPostAdapter) Stop(ctx stdctx.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	srv := a.server
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if srv != nil {
		return srv.Shutdown(ctx)
	}
	return nil
}

// fetchBotIdentity 调用 get_login_info 以填充 botID 和 botName。
func (a *HTTPPostAdapter) fetchBotIdentity(ctx stdctx.Context) {
	fetchCtx, cancel := stdctx.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	info, err := a.sender.GetLoginInfo(fetchCtx)
	if err != nil {
		return
	}
	a.mu.Lock()
	a.botID = strconv.FormatInt(info.UserID, 10)
	a.botName = info.Nickname
	a.mu.Unlock()
}

// ────────────────────────────────────────────────────────────────────────────
// HTTP 请求处理
// ────────────────────────────────────────────────────────────────────────────

// handlePost 处理单个 OneBot 事件 POST 请求。
func (a *HTTPPostAdapter) handlePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MB limit
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 若配置了密钥，则验证 HMAC-SHA1 签名
	if a.config.Secret != "" {
		sig := r.Header.Get("X-Signature")
		if !verifyHMACSHA1([]byte(a.config.Secret), body, sig) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			logger.Warn("[onebot.HTTPPostAdapter] Signature verification failed")
			return
		}
	}

	// 解析事件
	ev, err := parseEvent(body)
	if err != nil {
		logger.WithError(err).Debug("[onebot.HTTPPostAdapter] Failed to parse event")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if ev.Kind() == platform.EventKindUnknown {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 构建快速操作上下文，以便 handler 可以调度快速操作。
	quickOp := &quickOpContext{w: w}

	// 同步分发事件给 handler（HTTP 请求必须完成后快速操作才能写入响应体）。
	a.mu.RLock()
	handler := a.handler
	a.mu.RUnlock()
	if handler != nil {
		safeDispatch(handler, &quickOpEvent{Event: ev, qop: quickOp})
	}

	// 写入快速操作响应（若无快速操作则返回 204）
	if quickOp.hasOp() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(quickOp.encode())
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 快速操作
// ────────────────────────────────────────────────────────────────────────────

// quickOpContext 收集事件处理过程中设置的快速操作字段。
type quickOpContext struct {
	mu  sync.Mutex
	w   http.ResponseWriter
	ops map[string]any
}

func (q *quickOpContext) set(key string, val any) {
	q.mu.Lock()
	if q.ops == nil {
		q.ops = make(map[string]any)
	}
	q.ops[key] = val
	q.mu.Unlock()
}

func (q *quickOpContext) hasOp() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.ops) > 0
}

func (q *quickOpContext) encode() []byte {
	q.mu.Lock()
	defer q.mu.Unlock()
	b, _ := json.Marshal(q.ops)
	return b
}

// quickOpEvent 包装 platform.Event 并暴露快速操作设置器。
// 实现与内部事件相同的接口，以便 handler 可以直接使用。
type quickOpEvent struct {
	platform.Event
	qop *quickOpContext
}

// SetReply 设置回复快速操作。
func (e *quickOpEvent) SetReply(msg string) { e.qop.set("reply", msg) }

// SetAtSender 设置 at_sender 快速操作（仅限群消息）。
func (e *quickOpEvent) SetAtSender(v bool) { e.qop.set("at_sender", v) }

// SetDelete 请求撤回触发本次事件的消息。
func (e *quickOpEvent) SetDelete(v bool) { e.qop.set("delete", v) }

// SetKick 请求踢出发送者（仅限群消息）。
func (e *quickOpEvent) SetKick(v bool) { e.qop.set("kick", v) }

// SetBan 请求禁言发送者（仅限群消息）。
func (e *quickOpEvent) SetBan(v bool) { e.qop.set("ban", v) }

// SetBanDuration 设置禁言时长（秒）。
func (e *quickOpEvent) SetBanDuration(secs int) { e.qop.set("ban_duration", secs) }

// SetApprove 设置同意快速操作（请求事件）。
func (e *quickOpEvent) SetApprove(v bool) { e.qop.set("approve", v) }

// SetRemark 设置好友备注（仅限好友请求同意）。
func (e *quickOpEvent) SetRemark(remark string) { e.qop.set("remark", remark) }

// SetReason 设置拒绝原因（请求事件）。
func (e *quickOpEvent) SetReason(reason string) { e.qop.set("reason", reason) }

// GetQuickOpEvent 安全地从 platform.Event 中提取 *quickOpEvent。
// 若事件未被包装则返回 nil。
func GetQuickOpEvent(ev platform.Event) *quickOpEvent {
	if qoe, ok := ev.(*quickOpEvent); ok {
		return qoe
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// HMAC-SHA1 验签
// ────────────────────────────────────────────────────────────────────────────

// 编译期接口断言
var (
	_ platform.Adapter     = (*HTTPPostAdapter)(nil)
	_ platform.BotIdentity = (*HTTPPostAdapter)(nil)
)

// verifyHMACSHA1 验证 X-Signature 头部与请求体的 HMAC-SHA1 是否匹配。
// 头部格式为 "sha1=<hex摘要>"。
func verifyHMACSHA1(secret, body []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha1=") {
		return false
	}
	expected := strings.TrimPrefix(signature, "sha1=")
	mac := hmac.New(sha1.New, secret)
	mac.Write(body)
	actual := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(actual), []byte(expected))
}
