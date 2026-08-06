package qq_test

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/require"
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
		// group_name 不是官方 GROUP_AT_MESSAGE_CREATE 事件的字段，不应存在
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
	// GROUP_AT_MESSAGE_CREATE 事件不返回 group_name 字段，Chat.Name 应为空
	if event.Chat().Name != "" {
		t.Errorf("Chat.Name: got %q, want empty (group_name not in official API)", event.Chat().Name)
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
	// GroupMsgReject/GroupMsgReceive/C2CMsgReject/C2CMsgReceive 是消息权限变更事件
	for _, et := range []dto.EventType{
		dto.GroupMsgReject, dto.GroupMsgReceive,
		dto.C2CMsgReject, dto.C2CMsgReceive,
	} {
		payload := makePayload(et, map[string]any{})
		event := qq.NewEvent(payload)
		if event.Kind() != platform.EventKindMsgPermissionChange {
			t.Errorf("EventType %q kind: got %q, want MsgPermissionChange", et, event.Kind())
		}
	}
}

func TestNewEvent_NoticeGroupFields(t *testing.T) {
	// GroupMsgReject/GroupMsgReceive 是消息权限变更事件，携带 group_openid 等字段
	for _, et := range []dto.EventType{dto.GroupMsgReject, dto.GroupMsgReceive} {
		payload := makePayload(et, map[string]any{
			"group_openid":     "group_001",
			"op_member_openid": "member_abc",
			"timestamp":        int64(1710000000),
		})
		event := qq.NewEvent(payload)

		if event.Kind() != platform.EventKindMsgPermissionChange {
			t.Errorf("[%s] Kind: got %q, want MsgPermissionChange", et, event.Kind())
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
	// GroupAddRobot/GroupDelRobot 是机器人自身加入/离开群组事件，映射为 BotAdded/BotRemoved
	memberEvents := []struct {
		et   dto.EventType
		kind platform.EventKind
	}{
		{dto.GroupAddRobot, platform.EventKindBotAdded},
		{dto.GroupDelRobot, platform.EventKindBotRemoved},
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
	// FriendAdd/FriendDel 是好友关系变更事件，映射为 FriendAdded/FriendRemoved
	friendEvents := []struct {
		et   dto.EventType
		kind platform.EventKind
	}{
		{dto.FriendAdd, platform.EventKindFriendAdded},
		{dto.FriendDel, platform.EventKindFriendRemoved},
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
		{dto.GroupAddRobot, platform.EventKindBotAdded},
		{dto.GroupDelRobot, platform.EventKindBotRemoved},
		{dto.FriendAdd, platform.EventKindFriendAdded},
		{dto.FriendDel, platform.EventKindFriendRemoved},
		{dto.GroupMsgReject, platform.EventKindMsgPermissionChange},
		{dto.C2CMsgReject, platform.EventKindMsgPermissionChange},
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

// ── 互动事件（INTERACTION_CREATE）────────────────────────────────────────────

func TestNewEvent_Interaction_Button(t *testing.T) {
	// type=11 消息按钮：content 应为 button_data
	payload := makePayload(dto.InteractionCreate, map[string]any{
		"id":          "interact001",
		"type":        11,
		"scene":       "c2c",
		"chat_type":   2,
		"user_openid": "user_abc",
		"timestamp":   "2026-03-09T13:00:00Z",
		"data": map[string]any{
			"type": 11,
			"resolved": map[string]any{
				"button_data": "cmd_help",
				"button_id":   "btn_1",
				"user_id":     "",
			},
		},
		"version": 1,
	})

	event := qq.NewEvent(payload)

	if event.Kind() != platform.EventKindInteraction {
		t.Errorf("Kind: got %q, want Interaction", event.Kind())
	}
	// id 应被覆盖为 interaction body 的 id
	if event.ID() != "interact001" {
		t.Errorf("ID: got %q, want interact001", event.ID())
	}
	// 被动回复 event_id 应取事件最外层的 id（payload.ID，envelope id）
	if got := event.Chat().Tokens[qq.TokenEventID]; got != "evt001" {
		t.Errorf("TokenEventID: got %q, want evt001 (outermost event id)", got)
	}
	// content 应为 button_data（type=11）
	if event.Content() != "cmd_help" {
		t.Errorf("Content: got %q, want cmd_help (button_data for type=11)", event.Content())
	}
	if event.Sender().ID != "user_abc" {
		t.Errorf("Sender.ID: got %q, want user_abc", event.Sender().ID)
	}
	if event.Chat().IsGroup {
		t.Error("c2c interaction chat should not be group")
	}
}

func TestNewEvent_Interaction_QuickMenu(t *testing.T) {
	// type=12 单聊快捷菜单：content 应为 feature_id
	payload := makePayload(dto.InteractionCreate, map[string]any{
		"id":          "interact002",
		"type":        12,
		"scene":       "c2c",
		"chat_type":   2,
		"user_openid": "user_xyz",
		"timestamp":   "2026-03-09T13:00:00Z",
		"data": map[string]any{
			"type": 12,
			"resolved": map[string]any{
				"button_data": "",
				"feature_id":  "menu_feature_001",
			},
		},
		"version": 1,
	})

	event := qq.NewEvent(payload)

	if event.Kind() != platform.EventKindInteraction {
		t.Errorf("Kind: got %q, want Interaction", event.Kind())
	}
	// content 应为 feature_id（type=12 单聊快捷菜单）
	if event.Content() != "menu_feature_001" {
		t.Errorf("Content: got %q, want menu_feature_001 (feature_id for type=12)", event.Content())
	}
	if event.Sender().ID != "user_xyz" {
		t.Errorf("Sender.ID: got %q, want user_xyz", event.Sender().ID)
	}
}

func TestNewEvent_Interaction_GuildReplyToID(t *testing.T) {
	// 频道场景下 replyToID 应为 data.resolved.message_id
	payload := makePayload(dto.InteractionCreate, map[string]any{
		"id":         "interact003",
		"type":       11,
		"scene":      "guild",
		"chat_type":  0,
		"guild_id":   "guild001",
		"channel_id": "chan001",
		"timestamp":  "2026-03-09T13:00:00Z",
		"data": map[string]any{
			"type": 11,
			"resolved": map[string]any{
				"button_data": "action_data",
				"button_id":   "btn_2",
				"user_id":     "author001",
				"message_id":  "msg_origin_001",
			},
		},
		"version": 1,
	})

	event := qq.NewEvent(payload)

	replyEvent, ok := event.(interface{ ReplyToID() string })
	if !ok {
		t.Fatal("event should implement ReplyToID()")
	}
	// replyToID 应为被操作消息 ID
	if replyEvent.ReplyToID() != "msg_origin_001" {
		t.Errorf("ReplyToID: got %q, want msg_origin_001 (data.resolved.message_id)", replyEvent.ReplyToID())
	}
	if event.Content() != "action_data" {
		t.Errorf("Content: got %q, want action_data", event.Content())
	}
}

// ── 表情表态事件（MESSAGE_REACTION_ADD / REMOVE）──────────────────────────────

func TestNewEvent_MessageReaction_ReplyToID(t *testing.T) {
	for _, evType := range []dto.EventType{dto.MessageReactionAdd, dto.MessageReactionRemove} {
		payload := makePayload(evType, map[string]any{
			"user_id":    "user001",
			"channel_id": "chan001",
			"guild_id":   "guild001",
			"emoji": map[string]any{
				"id":   "277",
				"type": 1,
			},
			"target": map[string]any{
				"id":   "msg_reacted_001",
				"type": 0,
			},
		})

		event := qq.NewEvent(payload)

		if event.Kind() != platform.EventKindReaction {
			t.Errorf("[%s] Kind: got %q, want Reaction", evType, event.Kind())
		}
		if event.Content() != "277" {
			t.Errorf("[%s] Content: got %q, want 277 (emoji.id)", evType, event.Content())
		}
		if event.Sender().ID != "user001" {
			t.Errorf("[%s] Sender.ID: got %q, want user001", evType, event.Sender().ID)
		}
		if event.Chat().ID != "chan001" {
			t.Errorf("[%s] Chat.ID: got %q, want chan001", evType, event.Chat().ID)
		}
		if event.Chat().ParentID != "guild001" {
			t.Errorf("[%s] Chat.ParentID: got %q, want guild001", evType, event.Chat().ParentID)
		}
		replyEvent, ok := event.(interface{ ReplyToID() string })
		if !ok {
			t.Fatalf("[%s] event should implement ReplyToID()", evType)
		}
		// replyToID 应为被表态的消息 ID（target.id）
		if replyEvent.ReplyToID() != "msg_reacted_001" {
			t.Errorf("[%s] ReplyToID: got %q, want msg_reacted_001 (target.id)", evType, replyEvent.ReplyToID())
		}
	}
}

// TestNewEvent_GroupMessageCreate_QuoteMessage 测试 message_type=103 引用消息的 content 提取。
func TestNewEvent_GroupMessageCreate_QuoteMessage(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_q001",
		"content":      " ",
		"group_openid": "group_001",
		"message_type": 103,
		"author": map[string]any{
			"member_openid": "mem001",
			"username":      "Alice",
			"member_role":   "admin",
		},
		"timestamp": "2026-07-21T10:10:00+08:00",
		"msg_elements": []any{
			map[string]any{
				"content": "=== 消息 1 ===\n今天完成了学习计划",
			},
		},
		"message_scene": map[string]any{
			"source": "default",
			"ext":    []string{"msg_idx=IDX001"},
		},
	})

	event := qq.NewEvent(payload)
	if event.Kind() != platform.EventKindGroupMessage {
		t.Errorf("Kind: got %q, want %q", event.Kind(), platform.EventKindGroupMessage)
	}
	// 引用消息的 content 应从 msg_elements[0].content 提取
	wantContent := "=== 消息 1 ===\n今天完成了学习计划"
	if event.Content() != wantContent {
		t.Errorf("Content: got %q, want %q", event.Content(), wantContent)
	}
	if event.Chat().ID != "group_001" {
		t.Errorf("Chat.ID: got %q, want group_001", event.Chat().ID)
	}
}

// TestNewEvent_GroupMessageCreate_WithRole 测试群消息携带成员角色信息。
func TestNewEvent_GroupMessageCreate_WithRole(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_r001",
		"content":      "hello",
		"group_openid": "group_001",
		"message_type": 0,
		"author": map[string]any{
			"member_openid": "mem001",
			"username":      "Bob",
			"member_role":   "owner",
		},
		"timestamp": "2026-07-21T10:10:00+08:00",
	})

	event := qq.NewEvent(payload)
	if event.Sender().GroupRole != platform.GroupRoleOwner {
		t.Errorf("GroupRole: got %v, want %v", event.Sender().GroupRole, platform.GroupRoleOwner)
	}

	// 测试普通成员
	payload2 := makePayload(dto.GroupAtMessageCreate, map[string]any{
		"id":           "msg_r002",
		"content":      "@me hi",
		"group_openid": "group_001",
		"author": map[string]any{
			"member_openid": "mem002",
			"username":      "Charlie",
			"member_role":   "member",
		},
		"timestamp": "2026-07-21T10:11:00+08:00",
	})
	event2 := qq.NewEvent(payload2)
	if event2.Sender().GroupRole != platform.GroupRoleMember {
		t.Errorf("GroupRole: got %v, want %v", event2.Sender().GroupRole, platform.GroupRoleMember)
	}
}

// TestNewEvent_GroupMessageCreate_Mentions 测试 GROUP_MESSAGE_CREATE 的 @ 用户列表。
func TestNewEvent_GroupMessageCreate_Mentions(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_m001",
		"content":      "<@u001> <@u002> hello everyone",
		"group_openid": "group_001",
		"author": map[string]any{
			"member_openid": "mem001",
			"username":      "Alice",
		},
		"timestamp": "2026-07-21T10:10:00+08:00",
		"mentions": []any{
			map[string]any{"id": "u001", "username": "Bob", "bot": false},
			map[string]any{"id": "u002", "username": "Charlie", "bot": true, "is_you": true},
		},
	})

	event := qq.NewEvent(payload)
	mentionsEvent, ok := event.(interface{ Mentions() []platform.UserInfo })
	if !ok {
		t.Fatal("event should implement Mentions()")
	}
	mentions := mentionsEvent.Mentions()
	if len(mentions) != 2 {
		t.Fatalf("Mentions: got %d, want 2", len(mentions))
	}
	if mentions[0].ID != "u001" {
		t.Errorf("Mentions[0].ID: got %q, want u001", mentions[0].ID)
	}
	if mentions[1].IsBot != true {
		t.Errorf("Mentions[1].IsBot: want true")
	}
	if mentions[1].IsSelf != true {
		t.Errorf("Mentions[1].IsSelf: want true (is_you)")
	}
	// content 应已去除 <@id> 标记
	wantContent := "hello everyone"
	if event.Content() != wantContent {
		t.Errorf("Content: got %q, want %q", event.Content(), wantContent)
	}
}

// ── 结构化卡片（message_type=3，ark_data）─────────────────────────────────────

// TestNewEvent_GroupMessageCreate_ArkData 测试群聊结构化卡片：
// 卡片数据在 ark_data 字段，content 为空 → SegmentUnknown + Extra 保留原始。
func TestNewEvent_GroupMessageCreate_ArkData(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_ark001",
		"content":      "",
		"group_openid": "group_001",
		"message_type": 3,
		"author": map[string]any{
			"member_openid": "mem001",
			"username":      "Alice",
			"member_role":   "member",
		},
		"timestamp": "2026-07-21T10:10:00+08:00",
		"ark_data": map[string]any{
			"template_id": 3,
			"kv": []any{
				map[string]any{"key": "#title", "value": "卡片标题"},
			},
		},
	})

	event := qq.NewEvent(payload)
	if event.Kind() != platform.EventKindGroupMessage {
		t.Errorf("Kind: got %q, want %q", event.Kind(), platform.EventKindGroupMessage)
	}
	if event.Content() != "" {
		t.Errorf("卡片消息 Content 应为空: got %q", event.Content())
	}
	segs := event.Segments()
	if len(segs) != 1 || segs[0].Type != platform.SegmentUnknown {
		t.Fatalf("Segments: got %+v, want 1 个 SegmentUnknown（ark_data）", segs)
	}
	raw, ok := segs[0].Extra[qq.ExtraKeyArkData].(string)
	if !ok || raw == "" {
		t.Errorf("Extra[%q] 应保留 ark_data 原始 JSON", qq.ExtraKeyArkData)
	}
}

// TestNewEvent_C2CMessageCreate_ArkData 测试单聊结构化卡片（C2C 同样收 message_type=3）。
func TestNewEvent_C2CMessageCreate_ArkData(t *testing.T) {
	payload := makePayload(dto.C2CMessageCreate, map[string]any{
		"id":           "msg_ark002",
		"content":      "",
		"author":       map[string]any{"user_openid": "user001"},
		"timestamp":    "2026-07-21T10:10:00+08:00",
		"message_type": 3,
		"ark_data": map[string]any{
			"template_id": 3,
			"kv": []any{
				map[string]any{"key": "#title", "value": "单聊卡片"},
			},
		},
	})

	event := qq.NewEvent(payload)
	if event.Kind() != platform.EventKindPrivateMessage {
		t.Errorf("Kind: got %q, want %q", event.Kind(), platform.EventKindPrivateMessage)
	}
	segs := event.Segments()
	if len(segs) != 1 || segs[0].Type != platform.SegmentUnknown {
		t.Fatalf("Segments: got %+v, want 1 个 SegmentUnknown（ark_data）", segs)
	}
	if _, ok := segs[0].Extra[qq.ExtraKeyArkData].(string); !ok {
		t.Errorf("Extra[%q] 应保留 ark_data 原始 JSON", qq.ExtraKeyArkData)
	}
}

// TestNewEvent_GroupMessageCreate_TextStillParsed 常规文本消息不受 ark_data 分支影响。
func TestNewEvent_GroupMessageCreate_TextStillParsed(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_t001",
		"content":      "hello",
		"group_openid": "group_001",
		"message_type": 0,
		"author": map[string]any{
			"member_openid": "mem001",
			"username":      "Alice",
			"member_role":   "member",
		},
		"timestamp": "2026-07-21T10:10:00+08:00",
	})
	event := qq.NewEvent(payload)
	if event.Content() != "hello" {
		t.Errorf("Content: got %q, want hello", event.Content())
	}
}

// ── 引用消息（message_type=103）实测样本（2026-08 报文核验） ────────────────

// TestNewEvent_GroupMessageCreate_QuoteRealSample 覆盖真实报文结构：
//   - 引用目标 ID 在外层 message_scene.ext 的 ref_msg_idx=REFIDX_...（非 msg_elements）
//   - msg_elements[0] 为被引用消息（content + attachments）
//   - 外层 content 为回复者正文（含 <@id> 占位符，@ 交错解析）
//   - parallel_message.msg_nodes 为并行消息形态
func TestNewEvent_GroupMessageCreate_QuoteRealSample(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_quote_real",
		"content":      " <@A1> <@B2> 123<@A1> ",
		"group_openid": "group_001",
		"message_type": 103,
		"author": map[string]any{
			"member_openid": "mem001",
			"username":      "月莫法师",
			"member_role":   "owner",
		},
		"timestamp": "2026-08-06T18:00:48+08:00",
		"mentions": []any{
			map[string]any{"id": "A1", "username": "蕾米莉亚", "bot": true, "is_you": true},
			map[string]any{"id": "B2", "username": "蕾米莉亚二号", "bot": false, "is_you": false},
		},
		"message_scene": map[string]any{
			"source": "default",
			"ext": []string{
				"ref_msg_idx=REFIDX_7NQsHTV9A8ulz+4Tiu3qSg==",
				"msg_idx=REFIDX_6QtGfkqC/dO0WqCl1YSM4A==",
				"auth_token=uvjlPc7jnD-YG2d8r4iqpU0UMGKUu40IhEuEX3Sujp4X_p95PZCSiSfRz_2uD4D9DlWIWy1tSJDkn1HX-JxweHj7BQFHbxdXEDRyrjsx4Zg9CuCdOzfZGkCU318",
			},
		},
		"msg_elements": []any{
			map[string]any{
				"msg_idx":      "REFIDX_7NQsHTV9A8ulz+4Tiu3qSg==",
				"message_type": 103,
				"content":      " ",
				"attachments": []any{
					map[string]any{
						"url":          "https://multimedia.nt.qq.com.cn/download?appid=1407&fileid=EhRZQ6kypMVpjThZht4dkMs0ta9krBjIlQkg_woo94SF4dWJlgMyBHByb2RQgL2jAVoQV71_juPXS3w4id2EZuYpSnoCMIOCAQJneg&rkey=CAMSOLgthq-6lGU_Xf8W8f4lldlTW7oMRCc-24LzIkTfrL4B03eCtrJlCFgspVXL00XUl60dSAGz82aA&spec=0",
						"filename":     "53e8d4a7d3d620ceecc51d651ed9f7a0.png",
						"width":        700,
						"height":       1099,
						"size":         150216,
						"content_type": "image/png",
						"content":      "",
					},
				},
			},
		},
		"parallel_message": map[string]any{
			"msg_nodes": []any{
				map[string]any{"message_type": 7, "content": "[图片] "},
			},
		},
	})

	event := qq.NewEvent(payload)

	// reply 段：引用标识来自 message_scene.ext 的 ref_msg_idx
	segs := event.Segments()
	require.True(t, len(segs) >= 3, "Segments: got %+v", segs)
	if segs[0].Type != platform.SegmentReply || segs[0].ReplyToID != "REFIDX_7NQsHTV9A8ulz+4Tiu3qSg==" {
		t.Errorf("Segments[0]: got %+v, want SegmentReply(REFIDX_7NQsHTV9A8ulz+4Tiu3qSg==)", segs[0])
	}
	if _, ok := segs[0].Extra["raw_quote"].(string); !ok {
		t.Error("reply 段应携带 raw_quote（被引用消息原始 JSON，同平台转发还原）")
	}
	// forward 段：parallel_message.msg_nodes
	if segs[1].Type != platform.SegmentForward {
		t.Errorf("Segments[1]: got %+v, want SegmentForward（parallel_message）", segs[1])
	}

	// 正文 @ 交错：text(" "), at(A), text(" "), at(B), text(" 123"), at(A), text(" ")
	body := segs[2:]
	if body[0].Type != platform.SegmentText || body[1].Type != platform.SegmentAt || body[1].UserID != "A1" {
		t.Errorf("正文开头: got %+v, want [text, at(A1), ...]", body[:2])
	}
	if body[2].Type != platform.SegmentText || body[3].Type != platform.SegmentAt || body[3].UserID != "B2" {
		t.Errorf("正文中段: got %+v, want [text, at(B2), ...]", body[2:4])
	}

	// Content：@ 剥离后仅剩正文文本（TrimSpace）
	if event.Content() != "123" {
		t.Errorf("Content: got %q, want %q", event.Content(), "123")
	}

	// Mentions：payload 结构化 @（含自身 IsSelf）
	mentions := platform.GetMentions(event)
	require.Len(t, mentions, 2)
	if mentions[0].ID != "A1" || !mentions[0].IsSelf {
		t.Errorf("Mentions[0]: got %+v, want A1 IsSelf=true", mentions[0])
	}
	if mentions[1].ID != "B2" || mentions[1].IsSelf {
		t.Errorf("Mentions[1]: got %+v, want B2 IsSelf=false", mentions[1])
	}
}

// TestNewEvent_GroupMessageCreate_QuotePlainTextFallback 纯文本引用（正文为空）：
// 被引用文本经 msg_elements[0].content 兜底为正文。
func TestNewEvent_GroupMessageCreate_QuotePlainTextFallback(t *testing.T) {
	payload := makePayload(dto.GroupMessageCreate, map[string]any{
		"id":           "msg_quote_plain",
		"content":      " ",
		"group_openid": "group_001",
		"message_type": 103,
		"author": map[string]any{
			"member_openid": "mem001",
			"username":      "Alice",
			"member_role":   "member",
		},
		"timestamp": "2026-07-21T10:10:00+08:00",
		"message_scene": map[string]any{
			"source": "default",
			"ext":    []string{"ref_msg_idx=REFIDX_plain", "msg_idx=REFIDX_x"},
		},
		"msg_elements": []any{
			map[string]any{
				"content": "被引用的旧消息文本",
			},
		},
	})

	event := qq.NewEvent(payload)
	segs := event.Segments()
	require.True(t, len(segs) >= 2, "Segments: got %+v", segs)
	if segs[0].Type != platform.SegmentReply || segs[0].ReplyToID != "REFIDX_plain" {
		t.Errorf("Segments[0]: got %+v, want SegmentReply(REFIDX_plain)", segs[0])
	}
	if event.Content() != "被引用的旧消息文本" {
		t.Errorf("Content: got %q, want 被引用的旧消息文本（正文空时兜底）", event.Content())
	}
}
