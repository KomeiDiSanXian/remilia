package qq_test

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

func makePayload(t dto.EventType, detail any) *dto.Payload {
	raw, _ := json.Marshal(detail)
	return &dto.Payload{
		ID:        "evt001",
		Type:      t,
		Operation: dto.Dispatch,
		Detail:    raw,
	}
}

func TestNewEvent_C2C(t *testing.T) {
	payload := makePayload(dto.C2CMessageCreate, map[string]any{
		"id":        "msg001",
		"content":   "hello bot",
		"timestamp": "2026-03-09T13:00:00Z",
		"author": map[string]any{
			"id":          "u001",
			"user_openid": "openid_alice",
		},
	})

	event := qq.NewEvent(payload)

	if event.Platform() != qq.PlatformID {
		t.Errorf("Platform: got %q", event.Platform())
	}
	if event.Kind() != platform.EventKindPrivateMessage {
		t.Errorf("Kind: got %q", event.Kind())
	}
	if event.RawType() != string(dto.C2CMessageCreate) {
		t.Errorf("RawType: got %q", event.RawType())
	}
	if event.Content() != "hello bot" {
		t.Errorf("Content: got %q", event.Content())
	}
	if event.Sender().ID != "openid_alice" {
		t.Errorf("Sender.ID: got %q", event.Sender().ID)
	}
	if event.Chat().IsGroup {
		t.Error("C2C chat should not be group")
	}
	if event.Chat().ID != "openid_alice" {
		t.Errorf("Chat.ID: got %q", event.Chat().ID)
	}
	// RawPayload 应返回原始 *dto.Payload
	if _, ok := event.RawPayload().(*dto.Payload); !ok {
		t.Error("RawPayload should return *dto.Payload")
	}
	// Timestamp
	if event.Timestamp().IsZero() {
		t.Error("Timestamp should not be zero for valid RFC3339 input")
	}
}

func TestNewEvent_GroupAt(t *testing.T) {
	payload := makePayload(dto.GroupAtMessageCreate, map[string]any{
		"id":           "msg002",
		"content":      "/ping",
		"group_openid": "group001",
		"author": map[string]any{
			"id":            "u002",
			"member_openid": "member_openid_bob",
		},
	})

	event := qq.NewEvent(payload)

	if event.Kind() != platform.EventKindGroupMessage {
		t.Errorf("Kind: got %q, want GroupMessage", event.Kind())
	}
	if event.Content() != "/ping" {
		t.Errorf("Content: got %q", event.Content())
	}
	if event.Sender().ID != "member_openid_bob" {
		t.Errorf("Sender.ID: got %q", event.Sender().ID)
	}
	if !event.Chat().IsGroup {
		t.Error("Group chat should have IsGroup=true")
	}
	if event.Chat().ID != "group001" {
		t.Errorf("Chat.ID: got %q", event.Chat().ID)
	}
}

func TestNewEvent_SystemReady(t *testing.T) {
	payload := makePayload(dto.Ready, map[string]any{"version": "1.0"})
	event := qq.NewEvent(payload)
	if event.Kind() != platform.EventKindSystem {
		t.Errorf("Ready event kind: got %q", event.Kind())
	}
}

func TestNewEvent_Notice(t *testing.T) {
	for _, et := range []dto.EventType{
		dto.FriendAdd, dto.FriendDel,
		dto.GroupAddRobot, dto.GroupDelRobot,
	} {
		payload := makePayload(et, map[string]any{})
		event := qq.NewEvent(payload)
		if event.Kind() != platform.EventKindNotice {
			t.Errorf("EventType %q kind: got %q, want Notice", et, event.Kind())
		}
	}
}

func TestNewEvent_NoticeGroupFields(t *testing.T) {
	groupEvents := []dto.EventType{
		dto.GroupAddRobot,
		dto.GroupDelRobot,
		dto.GroupMsgReject,
		dto.GroupMsgReceive,
	}
	for _, et := range groupEvents {
		payload := makePayload(et, map[string]any{
			"group_openid":    "group_001",
			"op_member_openid": "member_abc",
			"timestamp":       int64(1710000000),
		})
		event := qq.NewEvent(payload)

		if event.Kind() != platform.EventKindNotice {
			t.Errorf("[%s] Kind: got %q, want Notice", et, event.Kind())
		}
		if !event.Chat().IsGroup {
			t.Errorf("[%s] Chat.IsGroup: want true", et)
		}
		if event.Chat().ID != "group_001" {
			t.Errorf("[%s] Chat.ID: got %q, want group_001", et, event.Chat().ID)
		}
		if event.Sender().ID != "member_abc" {
			t.Errorf("[%s] Sender.ID: got %q, want member_abc", et, event.Sender().ID)
		}
		if event.Timestamp().IsZero() {
			t.Errorf("[%s] Timestamp: should not be zero for unix ts=1710000000", et)
		}
	}
}

func TestNewEvent_NoticeUserFields(t *testing.T) {
	userEvents := []dto.EventType{
		dto.FriendAdd,
		dto.FriendDel,
		dto.C2CMsgReject,
		dto.C2CMsgReceive,
	}
	for _, et := range userEvents {
		payload := makePayload(et, map[string]any{
			"openid":    "user_xyz",
			"timestamp": int64(1710000000),
		})
		event := qq.NewEvent(payload)

		if event.Kind() != platform.EventKindNotice {
			t.Errorf("[%s] Kind: got %q, want Notice", et, event.Kind())
		}
		if event.Chat().IsGroup {
			t.Errorf("[%s] Chat.IsGroup: want false for user notice", et)
		}
		if event.Chat().ID != "user_xyz" {
			t.Errorf("[%s] Chat.ID: got %q, want user_xyz", et, event.Chat().ID)
		}
		if event.Sender().ID != "user_xyz" {
			t.Errorf("[%s] Sender.ID: got %q, want user_xyz", et, event.Sender().ID)
		}
		if event.Timestamp().IsZero() {
			t.Errorf("[%s] Timestamp: should not be zero for unix ts=1710000000", et)
		}
	}
}

func TestNewEvent_NoticeEmptyDetail(t *testing.T) {
	// 无 Detail 时不应 panic，字段保留零值
	for _, et := range []dto.EventType{dto.GroupAddRobot, dto.FriendAdd} {
		p := &dto.Payload{ID: "evt-nil", Type: et, Operation: dto.Dispatch, Detail: nil}
		event := qq.NewEvent(p)
		if event.Kind() != platform.EventKindNotice {
			t.Errorf("[%s] Kind: got %q", et, event.Kind())
		}
		if event.Sender().ID != "" || event.Chat().ID != "" {
			t.Errorf("[%s] fields should be empty with nil detail", et)
		}
	}
}

func TestNewEvent_NilPayload(t *testing.T) {
	event := qq.NewEvent(nil)
	if event.Kind() != platform.EventKindUnknown {
		t.Errorf("nil payload kind: got %q", event.Kind())
	}
	if event.Content() != "" {
		t.Error("nil payload content should be empty")
	}
}

func TestNewEvent_UnknownType(t *testing.T) {
	payload := makePayload("SOME_UNKNOWN_TYPE", map[string]any{})
	event := qq.NewEvent(payload)
	if event.Kind() != platform.EventKindUnknown {
		t.Errorf("Unknown type kind: got %q", event.Kind())
	}
}
