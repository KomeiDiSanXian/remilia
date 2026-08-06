package milky

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ────────────────────────────────────────────────────────────────────────────
// redactURLSecrets
// ────────────────────────────────────────────────────────────────────────────

func TestRedactURLSecrets(t *testing.T) {
	raw := "ws://127.0.0.1:6700/event?access_token=SECRET&foo=bar&token=T2"
	out := redactURLSecrets(raw)
	assert.NotContains(t, out, "SECRET")
	assert.NotContains(t, out, "T2")
	assert.Contains(t, out, "redacted")
	assert.Contains(t, out, "foo=bar")

	assert.Equal(t, "<url>", redactURLSecrets("://bad-url"))
	assert.Equal(t, "ws://x/event", redactURLSecrets("ws://x/event"))
}

// ────────────────────────────────────────────────────────────────────────────
// fetchBotIdentity / HealthDetail
// ────────────────────────────────────────────────────────────────────────────

func TestFetchBotIdentity(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	require.Empty(t, a.BotID())
	a.fetchBotIdentity(context.Background())

	assert.Equal(t, "10001", a.BotID())
	assert.Equal(t, "RemiliaBot", a.BotName())
}

func TestFetchBotIdentity_Failure(t *testing.T) {
	m := newMockMilkyServer(t)
	m.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	a := newTestAdapter(t, m)

	a.fetchBotIdentity(context.Background())
	assert.Empty(t, a.BotID(), "身份获取失败时不应设置 BotID")
}

func TestHealthDetail(t *testing.T) {
	m := newMockMilkyServer(t)
	a := newTestAdapter(t, m)

	detail := a.HealthDetail()
	assert.Equal(t, "websocket", detail["connection"])
	assert.NotContains(t, detail, "bot_identified")

	a.botID.Store("10001")
	detail = a.HealthDetail()
	assert.Equal(t, true, detail["bot_identified"])
}

// ────────────────────────────────────────────────────────────────────────────
// Start / eventLoop / readLoop / dial
// ────────────────────────────────────────────────────────────────────────────

// startWSAdapter 启动一个模拟 Milky 事件端点的 WebSocket 服务器。
// events 中的每条消息会在连接建立后逐条发送。
func startWSAdapter(t *testing.T, events []string, loginInfo string) (*Adapter, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			// HTTP API 路径：供 fetchBotIdentity 使用
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(loginInfo))
			return
		}
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for _, evt := range events {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(evt)); err != nil {
				return
			}
		}
		// 保持连接，直到客户端断开。
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	adapter, err := NewAdapter(Config{BaseURL: server.URL, AccessToken: "tok"})
	require.NoError(t, err)
	return adapter, server
}

func TestAdapter_Start_ReceivesEvents(t *testing.T) {
	msgEvent := `{"event_type":"message_receive","time":1700000000,"self_id":10001,"data":{
		"message_scene":"group","peer_id":555,"message_seq":1,"sender_id":1,"time":1700000000,
		"segments":[{"type":"text","data":{"text":"from ws"}}]}}`
	adapter, _ := startWSAdapter(t, []string{msgEvent}, `{"status":"ok","retcode":0,"data":{"uin":10001,"nickname":"Bot"}}`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	recv := make(chan platform.Event, 4)
	done := make(chan error, 1)
	go func() { done <- adapter.Start(ctx, func(e platform.Event) { recv <- e }) }()

	// 等待收到事件并验证 Bot 身份已加载。
	var got platform.Event
	select {
	case got = <-recv:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	assert.Equal(t, "from ws", platform.Content(got))
	assert.Equal(t, "555", got.Chat().ID)
	require.Eventually(t, func() bool { return adapter.BotID() == "10001" }, 3*time.Second, 20*time.Millisecond)
	assert.Equal(t, "Bot", adapter.BotName())
	assert.True(t, adapter.IsRunning())

	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
	assert.False(t, adapter.IsRunning())
}

func TestAdapter_Start_StopsOnSecondCall(t *testing.T) {
	adapter, _ := startWSAdapter(t, nil, `{"status":"ok","retcode":0,"data":{}}`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- adapter.Start(ctx, func(platform.Event) {}) }()

	// 等待运行后再次调用 Start：应直接返回 nil（幂等）。
	require.Eventually(t, func() bool { return adapter.IsRunning() }, 3*time.Second, 10*time.Millisecond)
	assert.NoError(t, adapter.Start(ctx, func(platform.Event) {}))

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}

func TestAdapter_Start_MaxReconnectExceeded(t *testing.T) {
	// 指向未监听的端口：dial 必然失败，超过 MaxReconnect 后 Start 返回错误。
	adapter, err := NewAdapter(Config{
		BaseURL:        "http://127.0.0.1:1",
		AccessToken:    "tok",
		ReconnectDelay: 10 * time.Millisecond,
		MaxReconnect:   2,
		DialTimeout:    200 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = adapter.Start(ctx, func(platform.Event) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeded max reconnect attempts")
}

func TestAdapter_Start_ContextCancelledWhileWaiting(t *testing.T) {
	adapter, err := NewAdapter(Config{
		BaseURL:        "http://127.0.0.1:1",
		ReconnectDelay: time.Hour, // 取消时必须立即返回，不等待退避
		DialTimeout:    100 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- adapter.Start(ctx, func(platform.Event) {}) }()

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after cancel during reconnect backoff")
	}
}

func TestAdapter_Stop_BeforeStart(t *testing.T) {
	adapter, err := NewAdapter(Config{BaseURL: "http://127.0.0.1:6700"})
	require.NoError(t, err)
	assert.NoError(t, adapter.Stop(context.Background()))
}

// ────────────────────────────────────────────────────────────────────────────
// dial
// ────────────────────────────────────────────────────────────────────────────

func TestDial_ErrorRedactsToken(t *testing.T) {
	adapter, err := NewAdapter(Config{BaseURL: "http://127.0.0.1:1", DialTimeout: 200 * time.Millisecond})
	require.NoError(t, err)

	wsURL := buildWSURL(adapter.cfg.BaseURL, "supersecret")
	_, err = adapter.dial(context.Background(), wsURL)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "supersecret")
	assert.Contains(t, err.Error(), "redacted")
}

func TestReadLoop_CancelledContext(t *testing.T) {
	// 通过真实 WS 连接验证 readLoop 在 ctx 取消后退出。
	msgEvent := `{"event_type":"bot_offline","time":1700000000,"self_id":10001,"data":{"reason":"test"}}`
	adapter, _ := startWSAdapter(t, []string{msgEvent}, `{"status":"ok","retcode":0,"data":{}}`)

	ctx, cancel := context.WithCancel(context.Background())
	eventCh := make(chan platform.Event, 4)
	done := make(chan error, 1)
	go func() { done <- adapter.Start(ctx, func(e platform.Event) { eventCh <- e }) }()

	select {
	case evt := <-eventCh:
		assert.Equal(t, platform.EventKindSystem, evt.Kind())
		assert.Equal(t, "test", platform.Content(evt))
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 并发安全
// ────────────────────────────────────────────────────────────────────────────

func TestAdapter_ConcurrentStartStop(t *testing.T) {
	adapter, _ := startWSAdapter(t, nil, `{"status":"ok","retcode":0,"data":{}}`)

	var wg sync.WaitGroup
	ctx := context.Background()
	done := make(chan error, 1)

	wg.Go(func() {
		done <- adapter.Start(ctx, func(platform.Event) {})
	})

	require.Eventually(t, func() bool { return adapter.IsRunning() }, 3*time.Second, 10*time.Millisecond)

	// 并发调用 Start 与 Stop，不应死锁或 panic。
	for range 5 {
		_ = adapter.Start(ctx, func(platform.Event) {})
	}
	assert.NoError(t, adapter.Stop(context.Background()))

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after stop")
	}
	wg.Wait()
}
