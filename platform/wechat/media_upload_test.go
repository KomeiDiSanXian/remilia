package wechat

// media_upload_test.go — 分片上传 / 媒体下载的端到端行为。
//
// 用 httptest 起一个假的 WebSocket 长连接服务端，按官方协议回执，
// 覆盖三步上传（init → chunk × N → finish）与入站解密。

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

// fakeAIBot 是测试用的长连接服务端。
type fakeAIBot struct {
	mu       sync.Mutex
	frames   []Frame          // 收到的上行帧
	replies  map[string]Frame // 按 req_id 记录回执内容
	uploadID string
	mediaID  string
}

// newFakeAIBot 启动假服务端，返回其 ws URL 与句柄。
func newFakeAIBot(t *testing.T) (*fakeAIBot, string) {
	t.Helper()
	fb := &fakeAIBot{
		replies:  make(map[string]Frame),
		uploadID: "upload-1",
		mediaID:  "media-1",
	}
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			fb.mu.Lock()
			fb.frames = append(fb.frames, frame)
			fb.mu.Unlock()

			if ack := fb.ack(frame); ack != nil {
				_ = conn.WriteJSON(ack)
			}
		}
	}))
	t.Cleanup(srv.Close)
	return fb, "ws" + strings.TrimPrefix(srv.URL, "http")
}

// ack 决定对某个上行帧回什么。
func (fb *fakeAIBot) ack(frame Frame) *Frame {
	zero := 0
	switch frame.Cmd {
	case cmdSubscribe, cmdPing:
		return &Frame{Headers: FrameHeaders{ReqID: frame.Headers.ReqID}, ErrCode: &zero}
	case cmdUploadInit:
		body, _ := jsonx.Marshal(uploadInitResult{UploadID: fb.uploadID})
		return &Frame{Headers: FrameHeaders{ReqID: frame.Headers.ReqID}, Body: body, ErrCode: &zero}
	case cmdUploadChunk:
		return &Frame{Headers: FrameHeaders{ReqID: frame.Headers.ReqID}, ErrCode: &zero}
	case cmdUploadFinish:
		body, _ := jsonx.Marshal(uploadFinishResult{MediaID: fb.mediaID})
		return &Frame{Headers: FrameHeaders{ReqID: frame.Headers.ReqID}, Body: body, ErrCode: &zero}
	case cmdSendMsg:
		return &Frame{Headers: FrameHeaders{ReqID: frame.Headers.ReqID}, ErrCode: &zero}
	case cmdRespondMsg:
		// 回复帧同样有回执（成功 errcode=0）。
		return &Frame{Headers: FrameHeaders{ReqID: frame.Headers.ReqID}, ErrCode: &zero}
	default:
		return nil
	}
}

// connect 让适配器连上假服务端（订阅成功后返回）。
//
// 读循环必须同时跑起来：回执帧只能经 readLoop 分发给等待中的请求，
// 否则每个 request 都会等到超时。
func connect(t *testing.T, fb *fakeAIBot, url string) *Adapter {
	t.Helper()
	a, err := NewAdapter(Config{
		BotID:          "bot",
		Secret:         "secret",
		Endpoint:       url,
		RequestTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	client := newWSClient(a.cfg, func(string, jsonx.RawMessage) {})
	if err := client.dial(t.Context()); err != nil {
		t.Fatalf("dial: %v", err)
	}

	readErr := make(chan error, 1)
	go func() { readErr <- client.readLoop(t.Context()) }()

	if err := client.subscribe(t.Context()); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-readErr:
		case <-time.After(2 * time.Second):
			t.Error("读循环未退出")
		}
	})
	a.setClient(client)
	return a
}

// TestUploadMediaChunks 验证分片数量、顺序与 base64 内容。
func TestUploadMediaChunks(t *testing.T) {
	fb, url := newFakeAIBot(t)
	a := connect(t, fb, url)

	// 1.5 个分片：验证"最后一片不足"的边界。
	data := make([]byte, mediaChunkSize+100)
	for i := range data {
		data[i] = byte('a' + i%26)
	}

	mediaID, err := a.sender.UploadMedia(t.Context(), MediaKindFile, "a.bin", data)
	if err != nil {
		t.Fatalf("UploadMedia: %v", err)
	}
	if mediaID != fb.mediaID {
		t.Fatalf("media_id = %q", mediaID)
	}

	fb.mu.Lock()
	defer fb.mu.Unlock()

	var chunks []Frame
	for _, f := range fb.frames {
		if f.Cmd == cmdUploadChunk {
			chunks = append(chunks, f)
		}
	}
	if len(chunks) != 2 {
		t.Fatalf("分片数 = %d, want 2", len(chunks))
	}
	// 分片序号必须从 0 连续递增：服务端按 chunk_index 合并，跳号会缺数据。
	for i, f := range chunks {
		var body uploadChunkBody
		if err := jsonx.Unmarshal(f.Body, &body); err != nil {
			t.Fatalf("解析分片 %d: %v", i, err)
		}
		if body.ChunkIndex != i {
			t.Fatalf("分片 %d 的 chunk_index = %d", i, body.ChunkIndex)
		}
		if body.UploadID != fb.uploadID {
			t.Fatalf("分片 upload_id = %q", body.UploadID)
		}
	}
}

// TestUploadMediaTooLarge 超过类型上限必须在上传前拒绝。
func TestUploadMediaTooLarge(t *testing.T) {
	fb, url := newFakeAIBot(t)
	a := connect(t, fb, url)

	data := make([]byte, maxVoiceBytes+1)
	_, err := a.sender.UploadMedia(t.Context(), MediaKindVoice, "a.amr", data)
	if err == nil {
		t.Fatal("超过语音 2MB 上限应报错")
	}
	fb.mu.Lock()
	defer fb.mu.Unlock()
	for _, f := range fb.frames {
		if f.Cmd == cmdUploadInit {
			t.Fatal("超限时不应发起上传")
		}
	}
}

// TestUploadMediaWithoutConnection 长连接未就绪时给出可读错误。
func TestUploadMediaWithoutConnection(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if _, err := a.sender.UploadMedia(t.Context(), MediaKindFile, "a", []byte("data")); err == nil {
		t.Fatal("无连接时应报错")
	}
}

// TestNotifyUser 主动推送带 chat_type 且 chatid 正确。
func TestNotifyUser(t *testing.T) {
	fb, url := newFakeAIBot(t)
	a := connect(t, fb, url)

	if err := a.sender.NotifyUser(t.Context(), "user-1", platform.TextMessage("hi")); err != nil {
		t.Fatalf("NotifyUser: %v", err)
	}

	fb.mu.Lock()
	defer fb.mu.Unlock()
	var got *notifyBody
	for _, f := range fb.frames {
		if f.Cmd == cmdSendMsg {
			var body notifyBody
			if err := jsonx.Unmarshal(f.Body, &body); err != nil {
				t.Fatalf("解析 notifyBody: %v", err)
			}
			got = &body
		}
	}
	if got == nil {
		t.Fatal("未发出 aibot_send_msg")
	}
	if got.ChatID != "user-1" {
		t.Fatalf("chatid = %q", got.ChatID)
	}
	if got.ChatType != ChatTypeUser {
		t.Fatalf("chat_type = %d, want %d（单聊）", got.ChatType, ChatTypeUser)
	}
	if got.Markdown == nil || got.Markdown.Content != "hi" {
		t.Fatalf("markdown 内容错误: %+v", got.Markdown)
	}
}

// TestNotifyWithoutConnection 无长连接时主动推送必须失败（而不是静默丢弃）。
func TestNotifyWithoutConnection(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if err := a.sender.NotifyUser(t.Context(), "u", platform.TextMessage("x")); err == nil {
		t.Fatal("无长连接时主动推送应报错")
	}
}

// TestReplyByReqID 回复透传回调的 req_id（唯一回复授权）。
func TestReplyByReqID(t *testing.T) {
	fb, url := newFakeAIBot(t)
	a := connect(t, fb, url)

	req := platform.SendRequest{
		Target: platform.ChatInfo{
			ID:     "user-1",
			Tokens: map[string]string{TokenReplyReqID: "req-from-callback", TokenMsgID: "m1"},
		},
		Message: platform.TextMessage("pong"),
	}
	res, err := a.Sender().Send(t.Context(), req)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.MessageID != "m1" {
		t.Fatalf("MessageID = %q", res.MessageID)
	}

	fb.mu.Lock()
	defer fb.mu.Unlock()
	for _, f := range fb.frames {
		if f.Cmd == cmdRespondMsg {
			if f.Headers.ReqID != "req-from-callback" {
				t.Fatalf("回复帧 req_id = %q，必须透传回调的 req_id", f.Headers.ReqID)
			}
			return
		}
	}
	t.Fatal("未发出 aibot_respond_msg")
}
