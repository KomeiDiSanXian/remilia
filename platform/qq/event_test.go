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
	if event.ID() != "evt001" {
		t.Errorf("ID: got %q, want evt001", event.ID())
	}
	if event.Kind() != platform.EventKindPrivateMessage {
		t.Errorf("Kind: got %q", event.Kind())
	}
	if platform.RawType(event) != dto.C2CMessageCreate {
		t.Errorf("RawType: got %q", platform.RawType(event))
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
	// D5：RawPayload 返回 nil（payload 在 populate 后已释放到对象池）
	if platform.RawPayload(event) != nil {
		t.Errorf("RawPayload should return nil after D5 pool optimization, got %T", platform.RawPayload(event))
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
		"group_name":   "test-group",
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
	if event.Chat().Name != "test-group" {
		t.Errorf("Chat.Name: got %q, want test-group", event.Chat().Name)
	}
}

func TestNewEvent_GuildMessage(t *testing.T) {
	payload := makePayload(dto.AtMessageCreate, map[string]any{
		"content":      "hello guild",
		"channel_id":   "chan001",
		"guild_id":     "guild001",
		"channel_name": "general",
		"timestamp":    "2026-03-09T13:00:00Z",
		"author": map[string]any{
			"id":       "author001",
			"username": "alice",
		},
	})

	event := qq.NewEvent(payload)

	if event.Kind() != platform.EventKindGuildMessage {
		t.Errorf("Kind: got %q, want GuildMessage", event.Kind())
	}
	if event.Content() != "hello guild" {
		t.Errorf("Content: got %q", event.Content())
	}
	if event.Sender().ID != "author001" {
		t.Errorf("Sender.ID: got %q", event.Sender().ID)
	}
	if event.Sender().DisplayName != "alice" {
		t.Errorf("Sender.DisplayName: got %q, want alice", event.Sender().DisplayName)
	}
	if !event.Chat().IsGroup {
		t.Error("Guild message chat should have IsGroup=true")
	}
	// channel_id 优先作为 Chat.ID
	if event.Chat().ID != "chan001" {
		t.Errorf("Chat.ID: got %q, want chan001 (channel_id)", event.Chat().ID)
	}
	// guild_id 保存在 ParentID
	if event.Chat().ParentID != "guild001" {
		t.Errorf("Chat.ParentID: got %q, want guild001 (guild_id)", event.Chat().ParentID)
	}
	if event.Chat().Name != "general" {
		t.Errorf("Chat.Name: got %q, want general", event.Chat().Name)
	}
	if event.Timestamp().IsZero() {
		t.Error("Timestamp should not be zero for valid RFC3339 input")
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
	// GroupMsgReject/GroupMsgReceive/C2CMsgReject/C2CMsgReceive 属于通知类事件
	for _, et := range []dto.EventType{
		dto.GroupMsgReject, dto.GroupMsgReceive,
		dto.C2CMsgReject, dto.C2CMsgReceive,
	} {
		payload := makePayload(et, map[string]any{})
		event := qq.NewEvent(payload)
		if event.Kind() != platform.EventKindNotice {
			t.Errorf("EventType %q kind: got %q, want Notice", et, event.Kind())
		}
	}
}

func TestNewEvent_NoticeGroupFields(t *testing.T) {
	// GroupMsgReject/GroupMsgReceive 是通知类事件，携带 group_openid 等字段
	for _, et := range []dto.EventType{dto.GroupMsgReject, dto.GroupMsgReceive} {
		payload := makePayload(et, map[string]any{
			"group_openid":     "group_001",
			"op_member_openid": "member_abc",
			"timestamp":        int64(1710000000),
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

func TestNewEvent_MemberJoinLeaveGroupFields(t *testing.T) {
	// GroupAddRobot/GroupDelRobot 是机器人加入/离开群组事件，映射为 MemberJoin/MemberLeave
	memberEvents := []struct {
		et   dto.EventType
		kind platform.EventKind
	}{
		{dto.GroupAddRobot, platform.EventKindMemberJoin},
		{dto.GroupDelRobot, platform.EventKindMemberLeave},
	}
	for _, tc := range memberEvents {
		payload := makePayload(tc.et, map[string]any{
			"group_openid":     "group_001",
			"op_member_openid": "member_abc",
			"timestamp":        int64(1710000000),
		})
		event := qq.NewEvent(payload)

		if event.Kind() != tc.kind {
			t.Errorf("[%s] Kind: got %q, want %q", tc.et, event.Kind(), tc.kind)
		}
		if !event.Chat().IsGroup {
			t.Errorf("[%s] Chat.IsGroup: want true", tc.et)
		}
		if event.Chat().ID != "group_001" {
			t.Errorf("[%s] Chat.ID: got %q, want group_001", tc.et, event.Chat().ID)
		}
		if event.Sender().ID != "member_abc" {
			t.Errorf("[%s] Sender.ID: got %q, want member_abc", tc.et, event.Sender().ID)
		}
		if event.Timestamp().IsZero() {
			t.Errorf("[%s] Timestamp: should not be zero for unix ts=1710000000", tc.et)
		}
	}
}

func TestNewEvent_MemberJoinLeaveUserFields(t *testing.T) {
	// FriendAdd/FriendDel 是好友关系变更（成员加入/离开）事件
	friendEvents := []struct {
		et   dto.EventType
		kind platform.EventKind
	}{
		{dto.FriendAdd, platform.EventKindMemberJoin},
		{dto.FriendDel, platform.EventKindMemberLeave},
	}
	for _, tc := range friendEvents {
		payload := makePayload(tc.et, map[string]any{
			"openid":    "user_xyz",
			"timestamp": int64(1710000000),
		})
		event := qq.NewEvent(payload)

		if event.Kind() != tc.kind {
			t.Errorf("[%s] Kind: got %q, want %q", tc.et, event.Kind(), tc.kind)
		}
		if event.Chat().IsGroup {
			t.Errorf("[%s] Chat.IsGroup: want false for user event", tc.et)
		}
		if event.Chat().ID != "user_xyz" {
			t.Errorf("[%s] Chat.ID: got %q, want user_xyz", tc.et, event.Chat().ID)
		}
		if event.Sender().ID != "user_xyz" {
			t.Errorf("[%s] Sender.ID: got %q, want user_xyz", tc.et, event.Sender().ID)
		}
		if event.Timestamp().IsZero() {
			t.Errorf("[%s] Timestamp: should not be zero for unix ts=1710000000", tc.et)
		}
	}
}

func TestNewEvent_NilDetail(t *testing.T) {
	// 无 Detail 时不应 panic，Kind 仍正确，字段保留零值
	cases := []struct {
		et   dto.EventType
		kind platform.EventKind
	}{
		{dto.GroupAddRobot, platform.EventKindMemberJoin},
		{dto.GroupDelRobot, platform.EventKindMemberLeave},
		{dto.FriendAdd, platform.EventKindMemberJoin},
		{dto.FriendDel, platform.EventKindMemberLeave},
		{dto.GroupMsgReject, platform.EventKindNotice},
		{dto.C2CMsgReject, platform.EventKindNotice},
	}
	for _, tc := range cases {
		p := &dto.Payload{ID: "evt-nil", Type: tc.et, Operation: dto.Dispatch, Detail: nil}
		event := qq.NewEvent(p)
		if event.Kind() != tc.kind {
			t.Errorf("[%s] Kind: got %q, want %q", tc.et, event.Kind(), tc.kind)
		}
		if event.Sender().ID != "" || event.Chat().ID != "" {
			t.Errorf("[%s] fields should be empty with nil detail", tc.et)
		}
	}
}

func TestNewEvent_NilPayload(t *testing.T) {
	event := qq.NewEvent(nil)
	if event.Kind() != platform.EventKindUnknown {
		t.Errorf("nil payload kind: got %q", event.Kind())
	}
	if event.ID() != "" {
		t.Errorf("nil payload ID: got %q, want empty", event.ID())
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
