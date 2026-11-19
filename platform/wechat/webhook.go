package wechat

// webhook.go — URL 回调（短连接）模式。
//
// 企业微信把回调**加密**后 POST 到本端：
//
//	POST /wechat/aibot?msg_signature=..&timestamp=..&nonce=..
//	{"encrypt": "..."}
//
// 与长连接相比，这个模式多了三样东西、少了多半能力：
//
//	+ 需要公网可达的 URL 与 Token/EncodingAESKey
//	+ 回复走回调里的 response_url（一次性、1 小时有效）
//	- 没有主动推送（aibot_send_msg 只在长连接存在）
//	- 没有素材上传，因此发不了图片/文件
//
// 处理函数只做"验签 → 解密 → 入队 → 立刻 200"，业务在 worker 里跑。
// 企业微信 5 秒收不到响应就断连并重试三次，同步跑 handler 必然超时重投。

import (
	stdctx "context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// maxWebhookBodyBytes 限制单次回调体积（加密后的报文远小于此值）。
const maxWebhookBodyBytes = 4 << 20

// webhookEnvelope 是回调的外层信封。
type webhookEnvelope struct {
	Encrypt string `json:"encrypt"`
}

// webhookAdapter 承载 URL 回调模式的 HTTP 服务端与事件 worker。
//
// 它不是一个独立的 platform.Adapter：Adapter 在 webhook 模式下把 Start
// 委托给它，复用同一个 Sender 与 BotIdentity。
type webhookAdapter struct {
	cfg    Config
	crypt  *WXCrypt
	sender *wechatSender

	eventCh chan platform.Event
	workers sync.WaitGroup

	mu      sync.RWMutex
	server  *http.Server
	running bool
	cancel  stdctx.CancelFunc

	// responseTokens 保存最近一次回调的 response_url，让"事件里只有 chatid
	// 没有 response_url"的场景也能回复一次。
	responseMu     sync.Mutex
	responseTokens map[string]string
}

// newWebhookAdapter 创建 URL 回调适配器。
func newWebhookAdapter(cfg Config, crypt *WXCrypt, sender *wechatSender) *webhookAdapter {
	return &webhookAdapter{
		cfg:            cfg,
		crypt:          crypt,
		sender:         sender,
		eventCh:        make(chan platform.Event, cfg.EventBuffer),
		responseTokens: make(map[string]string),
	}
}

// Start 启动 HTTP 服务与事件 worker，阻塞到 ctx 取消。
func (w *webhookAdapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
	cancelCtx, cancel := stdctx.WithCancel(ctx)
	w.mu.Lock()
	w.cancel = cancel
	w.running = true
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
		cancel()
	}()

	mux := http.NewServeMux()
	mux.HandleFunc(w.cfg.Webhook.Path, w.handleCallback)
	srv := &http.Server{
		Addr:              w.cfg.Webhook.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	w.mu.Lock()
	w.server = srv
	w.mu.Unlock()

	for range w.cfg.WorkerCount {
		w.workers.Add(1)
		go w.worker(cancelCtx, handler)
	}

	logger.WithFields(logger.Fields{
		"platform": PlatformID,
		"addr":     w.cfg.Webhook.ListenAddr,
		"path":     w.cfg.Webhook.Path,
	}).Info("[wechat.Webhook] 回调服务已启动")

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	var runErr error
	select {
	case <-cancelCtx.Done():
	case err := <-serveErr:
		runErr = err
		logger.WithError(err).Error("[wechat.Webhook] HTTP 服务停止")
	}

	shutCtx, shutCancel := stdctx.WithTimeout(stdctx.Background(), w.cfg.Webhook.ShutdownTimeout)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil && !errors.Is(err, stdctx.DeadlineExceeded) {
		logger.WithError(err).Warn("[wechat.Webhook] 关闭 HTTP 服务时出错")
	}
	w.workers.Wait()
	return runErr
}

// Stop 关闭 HTTP 服务。
func (w *webhookAdapter) Stop(ctx stdctx.Context) error {
	w.mu.Lock()
	cancel := w.cancel
	srv := w.server
	w.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if srv != nil {
		return srv.Shutdown(ctx)
	}
	return nil
}

// IsRunning 报告回调服务是否在运行。
func (w *webhookAdapter) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

// worker 消费事件队列。
func (w *webhookAdapter) worker(ctx stdctx.Context, handler func(platform.Event)) {
	defer w.workers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-w.eventCh:
			if !ok {
				return
			}
			platform.SafeDispatch(handler, evt)
		}
	}
}

// handleCallback 处理 URL 回调：GET 做 URL 验证，POST 收消息。
func (w *webhookAdapter) handleCallback(rw http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	msgSig := query.Get("msg_signature")
	timestamp := query.Get("timestamp")
	nonce := query.Get("nonce")

	switch r.Method {
	case http.MethodGet:
		echo := query.Get("echostr")
		plain, err := w.crypt.VerifyURL(msgSig, timestamp, nonce, echo)
		if err != nil {
			logger.WithError(err).Warn("[wechat.Webhook] URL 验证失败")
			http.Error(rw, "invalid signature", http.StatusForbidden)
			return
		}
		// 必须原样返回明文：不加引号、不带 BOM、不带换行。
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(rw, plain)

	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodyBytes))
		if err != nil {
			http.Error(rw, "read body failed", http.StatusInternalServerError)
			return
		}
		var env webhookEnvelope
		if err := jsonx.Unmarshal(body, &env); err != nil {
			http.Error(rw, "invalid body", http.StatusBadRequest)
			return
		}
		plain, err := w.crypt.DecryptMessage(msgSig, timestamp, nonce, env.Encrypt)
		if err != nil {
			// 验签失败统一 403：企业微信不会重试非法来源，
			// 而 5xx 会让它按"处理失败"重投三次。
			logger.WithError(err).Warn("[wechat.Webhook] 回调验签/解密失败")
			http.Error(rw, "invalid signature", http.StatusForbidden)
			return
		}

		var cb Callback
		if err := jsonx.Unmarshal([]byte(plain), &cb); err != nil {
			http.Error(rw, "invalid payload", http.StatusBadRequest)
			return
		}
		w.rememberResponseURL(&cb)
		if evt := newEvent(&cb, w.cfg.BotID); evt != nil {
			attachResponseToken(evt, &cb)
			select {
			case w.eventCh <- evt:
			case <-time.After(time.Second):
				// 队列满：宁可丢事件也不能拖到 5 秒超时——超时会触发重投，
				// 重投又在队列满时同样阻塞，最终把回调服务拖死。
				logger.WithFields(logger.Fields{"platform": PlatformID, "msgid": cb.MsgID}).
					Warn("[wechat.Webhook] 事件队列已满，丢弃本次回调")
			}
		}
		// 立刻 200：被动回复走 response_url，不在 HTTP 响应里加密回包。
		rw.WriteHeader(http.StatusOK)

	default:
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// rememberResponseURL 记住会话最近一次的 response_url。
//
// 用途：response_url 只能用一次，而某些场景（如插件延时回复）拿到的事件
// 上下文里没有该字段；记录下来让后续同一会话的回复仍有机会用它。
func (w *webhookAdapter) rememberResponseURL(cb *Callback) {
	if cb.ResponseURL == "" {
		return
	}
	key := cb.ChatID
	if key == "" {
		key = cb.From.UserID
	}
	if key == "" {
		return
	}
	w.responseMu.Lock()
	w.responseTokens[key] = cb.ResponseURL
	w.responseMu.Unlock()
}

// responseURLFor 取会话最近一次的 response_url。
func (w *webhookAdapter) responseURLFor(chatID string) string {
	if chatID == "" {
		return ""
	}
	w.responseMu.Lock()
	defer w.responseMu.Unlock()
	return w.responseTokens[chatID]
}

// attachResponseToken 把一次性回复地址写入事件的 ChatInfo.Tokens。
func attachResponseToken(evt platform.Event, cb *Callback) {
	we, ok := evt.(*wechatEvent)
	if !ok {
		return
	}
	if we.chat.Tokens == nil {
		we.chat.Tokens = make(map[string]string, 2)
	}
	if cb.ResponseURL != "" {
		we.chat.Tokens[TokenResponseURL] = cb.ResponseURL
	}
	if cb.MsgID != "" {
		we.chat.Tokens[TokenMsgID] = cb.MsgID
	}
}
