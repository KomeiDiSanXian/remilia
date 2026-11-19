package wechat

// adapter_test.go — 长连接生命周期：订阅 → 收事件 → 被踢下线 → 断线重连。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// pushServer 是只负责"订阅 + 主动推帧"的假服务端。
type pushServer struct {
	mu    sync.Mutex
	subs  int
	onSub func(conn *websocket.Conn, n int)
	srv   *httptest.Server
}

func newPushServer(t *testing.T, onSub func(conn *websocket.Conn, n int)) (*pushServer, string) {
	t.Helper()
	ps := &pushServer{onSub: onSub}
	upgrader := websocket.Upgrader{}
	ps.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var frame Frame
			if err := jsonx.Unmarshal(data, &frame); err != nil {
				return
			}
			zero := 0
			switch frame.Cmd {
			case cmdSubscribe:
				ps.mu.Lock()
				ps.subs++
				n := ps.subs
				ps.mu.Unlock()
				_ = conn.WriteJSON(Frame{Headers: FrameHeaders{ReqID: frame.Headers.ReqID}, ErrCode: &zero})
				if ps.onSub != nil {
					go ps.onSub(conn, n)
				}
			case cmdPing:
				_ = conn.WriteJSON(Frame{Headers: FrameHeaders{ReqID: frame.Headers.ReqID}, ErrCode: &zero})
			}
		}
	}))
	t.Cleanup(ps.srv.Close)
	return ps, "ws" + strings.TrimPrefix(ps.srv.URL, "http")
}

// callbackFrame 构造一条下行消息回调帧。
func callbackFrame(t *testing.T, reqID, msgID, chatType, content string) []byte {
	t.Helper()
	cb := Callback{
		MsgID:    msgID,
		AIBotID:  "bot",
		ChatType: chatType,
		From:     From{UserID: "user-1"},
		MsgType:  msgTypeText,
		Text:     &TextBody{Content: content},
	}
	if chatType == chatTypeGroup {
		cb.ChatID = "chat-1"
	}
	body, err := jsonx.Marshal(cb)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	frame, err := jsonx.Marshal(Frame{
		Cmd:     cmdCallback,
		Headers: FrameHeaders{ReqID: reqID},
		Body:    body,
	})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return frame
}

// TestAdapterReceivesCallback 订阅后收到回调 → 事件带 req_id 回复授权。
func TestAdapterReceivesCallback(t *testing.T) {
	frames := [][]byte{callbackFrame(t, "req-1", "m1", chatTypeSingle, "hello")}
	_, url := newPushServer(t, func(conn *websocket.Conn, n int) {
		if n <= len(frames) {
			_ = conn.WriteMessage(websocket.TextMessage, frames[n-1])
		}
	})

	a, err := NewAdapter(Config{BotID: "bot", Secret: "secret", Endpoint: url})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	got := make(chan platform.Event, 1)
	ctx, cancel := t.Context(), func() {}
	started := make(chan struct{})
	go func() {
		close(started)
		_ = a.Start(ctx, func(e platform.Event) { got <- e })
	}()
	<-started

	// 等适配器连上并订阅（HealthDetail.subscribed）。
	waitFor(t, 3*time.Second, func() bool { return a.HealthDetail()["subscribed"] == true })

	select {
	case evt := <-got:
		if platform.Content(evt) != "hello" {
			t.Fatalf("正文 = %q", platform.Content(evt))
		}
		if id := evt.Chat().Tokens[TokenReplyReqID]; id != "req-1" {
			t.Fatalf("reply_req_id = %q，长连接回复依赖它", id)
		}
		if id := evt.Chat().Tokens[TokenMsgID]; id != "m1" {
			t.Fatalf("msgid = %q", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("超时：未收到事件")
	}

	cancel()
	_ = a.Stop(t.Context())
}

// TestAdapterStopsOnKick 收到 disconnected_event 后必须停止重连并返回致命错误。
//
// 否则两个实例会互相踢：A 重连踢掉 B，B 重连踢掉 A，消息随机丢失。
func TestAdapterStopsOnKick(t *testing.T) {
	_, url := newPushServer(t, func(conn *websocket.Conn, n int) {
		if n > 1 {
			return
		}
		cb := Callback{
			MsgID:    "m-kick",
			AIBotID:  "bot",
			ChatType: chatTypeSingle,
			From:     From{UserID: "u"},
			MsgType:  msgTypeEvent,
			Event:    &EventBody{EventType: eventTypeDisconnected},
		}
		body, _ := jsonx.Marshal(cb)
		frame, _ := jsonx.Marshal(Frame{
			Cmd:     cmdEventCallback,
			Headers: FrameHeaders{ReqID: "req-kick"},
			Body:    body,
		})
		_ = conn.WriteMessage(websocket.TextMessage, frame)
	})

	a, err := NewAdapter(Config{BotID: "bot", Secret: "secret", Endpoint: url})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- a.Start(t.Context(), func(platform.Event) {}) }()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("被踢下线应返回致命错误")
		}
		if !strings.Contains(err.Error(), "只允许一条长连接") {
			t.Fatalf("错误信息应说明单连接约束: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("超时：被踢下线后未停止重连")
	}

	detail := a.HealthDetail()
	if detail["kicked_by_new_connection"] != true {
		t.Fatalf("HealthDetail 应标记被踢: %+v", detail)
	}
}

// TestAdapterReconnects 连接断开后自动重连（订阅次数 ≥ 2）。
func TestAdapterReconnects(t *testing.T) {
	ps, url := newPushServer(t, func(conn *websocket.Conn, n int) {
		if n == 1 {
			// 订阅成功后立刻断开，触发重连。
			_ = conn.Close()
		}
	})

	a, err := NewAdapter(Config{
		BotID:          "bot",
		Secret:         "secret",
		Endpoint:       url,
		ReconnectDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	go func() { _ = a.Start(t.Context(), func(platform.Event) {}) }()
	waitFor(t, 5*time.Second, func() bool {
		ps.mu.Lock()
		defer ps.mu.Unlock()
		return ps.subs >= 2
	})
	_ = a.Stop(t.Context())
}

// waitFor 轮询直到条件成立或超时。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("超时：条件未成立")
}
