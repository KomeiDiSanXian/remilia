package qq_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/testbot"
)

// fakeEventSource 是 qq.EventSource 的测试替身。
type fakeEventSource struct {
	ch chan *dto.Payload
}

func (f *fakeEventSource) EventStream() <-chan *dto.Payload { return f.ch }

// TestAdapter_AcksInteractionEvent 验证收到 INTERACTION_CREATE（按钮回调）后，
// 适配器必须调用 RespondInteraction 回应，否则 QQ 客户端会一直 loading 直到
// 超时并显示"请求第三方失败"。
func TestAdapter_AcksInteractionEvent(t *testing.T) {
	api := testbot.NewMockAPI()
	src := &fakeEventSource{ch: make(chan *dto.Payload, 8)}
	adapter := qq.NewAdapter(src, api)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan platform.Event, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = adapter.Start(ctx, func(e platform.Event) { got <- e })
	}()

	detail, _ := json.Marshal(map[string]any{
		"id":                 "interact-123",
		"type":               11,
		"scene":              "group",
		"chat_type":          1,
		"group_openid":       "group-1",
		"group_member_openid": "member-1",
		"timestamp":          "2026-03-09T13:00:00Z",
		"data": map[string]any{
			"type": 11,
			"resolved": map[string]any{
				"button_data": "about:help",
			},
		},
	})
	src.ch <- &dto.Payload{ID: "evt001", Type: dto.InteractionCreate, Operation: dto.Dispatch, Detail: detail}

	select {
	case e := <-got:
		if e.Kind() != platform.EventKindInteraction {
			t.Fatalf("Kind: got %q, want INTERACTION", e.Kind())
		}
		if e.Content() != "about:help" {
			t.Errorf("Content: got %q, want about:help", e.Content())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event dispatch")
	}

	// 必须使用事件体 id（interaction_id）回应，code=0 表示成功
	deadline := time.After(5 * time.Second)
	for {
		inters := api.Interactions()
		if len(inters) > 0 {
			if inters[0].ID != "interact-123" {
				t.Errorf("RespondInteraction ID: got %q, want interact-123", inters[0].ID)
			}
			if inters[0].Code != 0 {
				t.Errorf("RespondInteraction code: got %d, want 0", inters[0].Code)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("RespondInteraction was not called for interaction event")
		case <-time.After(20 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("adapter did not stop")
	}
}

// TestAdapter_NoAckForNormalMessage 验证普通消息事件不会触发互动回应。
func TestAdapter_NoAckForNormalMessage(t *testing.T) {
	api := testbot.NewMockAPI()
	src := &fakeEventSource{ch: make(chan *dto.Payload, 8)}
	adapter := qq.NewAdapter(src, api)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = adapter.Start(ctx, func(platform.Event) { got <- struct{}{} })
	}()

	detail, _ := json.Marshal(map[string]any{
		"id":      "msg001",
		"content": "hello",
	})
	src.ch <- &dto.Payload{ID: "evt002", Type: dto.C2CMessageCreate, Operation: dto.Dispatch, Detail: detail}

	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event dispatch")
	}
	if n := len(api.Interactions()); n != 0 {
		t.Errorf("RespondInteraction called %d times for non-interaction event", n)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("adapter did not stop")
	}
}
