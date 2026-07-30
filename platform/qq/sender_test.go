package qq

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func newTestChat() platform.ChatInfo {
	return platform.ChatInfo{
		ID:      "target_001",
		IsGroup: true,
	}
}

func TestBuildDTOMessage_Card(t *testing.T) {
	msg := platform.TextMessage("")
	msg = ApplyExtra(msg, MessageExtra{
		Card: &dto.Card{
			Type: "tuwen",
			Content: dto.CardContent{
				Title:       "Test Title",
				Description: "Test Description",
				PicURL:      "https://example.com/pic.png",
				URL:         "https://example.com",
			},
		},
	})
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, newTestChat())

	assert.EqualValues(t, 8, dtoMsg.Type, "msg_type should be CardMessage (8)")
	assert.NotNil(t, dtoMsg.Card, "Card should not be nil")
	assert.Equal(t, "tuwen", dtoMsg.Card.Type)
	assert.Equal(t, "Test Title", dtoMsg.Card.Content.Title)
	assert.Equal(t, "https://example.com", dtoMsg.Card.Content.URL)
}

func TestBuildDTOMessage_InputNotify(t *testing.T) {
	msg := platform.TextMessage("")
	msg = ApplyExtra(msg, MessageExtra{
		InputNotify: &dto.InputNotify{
			InputType:   1,
			InputSecond: 30,
		},
	})
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, newTestChat())

	assert.EqualValues(t, 6, dtoMsg.Type, "msg_type should be InputNotifyMsg (6)")
	assert.NotNil(t, dtoMsg.InputNotify, "InputNotify should not be nil")
	assert.Equal(t, 1, dtoMsg.InputNotify.InputType)
	assert.Equal(t, 30, dtoMsg.InputNotify.InputSecond)
}

func TestBuildDTOMessage_MarkdownTemplate(t *testing.T) {
	msg := platform.MarkdownMessage("# Hello")
	msg = ApplyExtra(msg, MessageExtra{
		MarkdownTemplateID: "tmpl_001",
		MarkdownParams: []dto.MarkdownParam{
			{Key: "name", Values: []string{"Alice"}},
		},
	})
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, newTestChat())

	assert.EqualValues(t, 2, dtoMsg.Type, "msg_type should be MarkdownMessage (2)")
	assert.NotNil(t, dtoMsg.Markdown)
	assert.Equal(t, "# Hello", dtoMsg.Markdown.Content)
	assert.Equal(t, "tmpl_001", dtoMsg.Markdown.CustomTemplateID)
	assert.Len(t, dtoMsg.Markdown.Params, 1)
	assert.Equal(t, "name", dtoMsg.Markdown.Params[0].Key)
}

func TestBuildDTOMessage_ArkPriority(t *testing.T) {
	msg := platform.TextMessage("")
	msg = ApplyExtra(msg, MessageExtra{
		Ark: &Ark{TemplateID: 23},
		Card: &dto.Card{
			Type:    "tuwen",
			Content: dto.CardContent{Title: "Card", Description: "desc"},
		},
	})
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, newTestChat())

	assert.EqualValues(t, 3, dtoMsg.Type, "Ark (3) should take priority over Card (8)")
	assert.NotNil(t, dtoMsg.Ark)
	assert.Nil(t, dtoMsg.Card)
}

func TestBuildDTOMessage_IsWakeup(t *testing.T) {
	msg := platform.TextMessage("wakeup msg").WithReply("msg_001")
	msg = ApplyExtra(msg, MessageExtra{
		IsWakeup: true,
		MsgSeq:   42,
	})
	chat := platform.ChatInfo{
		ID:      "user_001",
		IsGroup: false,
	}
	s := &qqSender{}
	dtoMsg := s.buildDTOMessage(msg, chat)

	assert.True(t, dtoMsg.IsWakeup, "IsWakeup should be true")
	assert.Equal(t, uint64(42), dtoMsg.MessageSeq)
	// 召回消息应清除 msg_id 和 event_id
	assert.Empty(t, string(dtoMsg.MessageID), "MessageID should be empty for wakeup")
	assert.Empty(t, string(dtoMsg.EventID), "EventID should be empty for wakeup")
}

func TestBuildGuildDTOMessage_MarkdownTemplate(t *testing.T) {
	msg := platform.MarkdownMessage("# Channel")
	msg = ApplyExtra(msg, MessageExtra{
		MarkdownTemplateID: "tmpl_chan",
	})
	chat := platform.ChatInfo{
		ID:       "chan_001",
		ParentID: "guild_001",
	}
	s := &qqSender{}
	guildMsg := s.buildGuildDTOMessage(msg, chat)

	assert.NotNil(t, guildMsg.Markdown)
	assert.Equal(t, "# Channel", guildMsg.Markdown.Content)
	assert.Equal(t, "tmpl_chan", guildMsg.Markdown.CustomTemplateID)
}

func TestConvertButtons_Extra(t *testing.T) {
	buttons := []platform.Button{
		{
			ID:    "btn_cmd",
			Label: "Run",
			Style: platform.ButtonStylePrimary,
			Extra: &ButtonExtra{
				Enter:  true,
				Reply:  true,
				Anchor: 0,
			},
		},
		{
			ID:    "btn_link",
			Label: "Go",
			Style: platform.ButtonStyleLink,
			URL:   "https://example.com",
		},
	}

	kb := convertButtons(buttons)
	assert.NotNil(t, kb)
	assert.NotNil(t, kb.Content)
	assert.Len(t, kb.Content.Rows, 2, "two buttons with Row=0 should each be on their own row")

	// 第一个按钮应有 Enter/Reply
	firstRow := kb.Content.Rows[0]
	assert.Len(t, firstRow.Buttons, 1)
	btn0 := firstRow.Buttons[0]
	assert.Equal(t, "btn_cmd", btn0.ID)
	assert.True(t, btn0.Action.Enter, "ButtonExtra.Enter should be true")
	assert.True(t, btn0.Action.Reply, "ButtonExtra.Reply should be true")
	assert.Equal(t, 0, btn0.Action.Anchor)

	// 第二个按钮无 Extra，不应设置 enter/reply
	secondRow := kb.Content.Rows[1]
	btn1 := secondRow.Buttons[0]
	assert.Equal(t, "btn_link", btn1.ID)
	assert.False(t, btn1.Action.Enter, "link button should not have Enter")
	assert.Equal(t, 0, btn1.Action.Type, "link button action type should be 0 (jump)")
}

func TestConvertButtons_RowGrouping(t *testing.T) {
	buttons := []platform.Button{
		{ID: "a", Label: "A", Row: 1},
		{ID: "b", Label: "B", Row: 1},
		{ID: "c", Label: "C", Row: 0},
		{ID: "d", Label: "D", Row: 2},
	}
	kb := convertButtons(buttons)
	assert.NotNil(t, kb)
	assert.Len(t, kb.Content.Rows, 3)
	// Row 1 应有两个按钮在同一行
	assert.Len(t, kb.Content.Rows[0].Buttons, 2)
	assert.Equal(t, "a", kb.Content.Rows[0].Buttons[0].ID)
	assert.Equal(t, "b", kb.Content.Rows[0].Buttons[1].ID)
	// Row 0 的 c 独占一行
	assert.Len(t, kb.Content.Rows[1].Buttons, 1)
	assert.Equal(t, "c", kb.Content.Rows[1].Buttons[0].ID)
	// Row 2 的 d 独占一行
	assert.Len(t, kb.Content.Rows[2].Buttons, 1)
	assert.Equal(t, "d", kb.Content.Rows[2].Buttons[0].ID)
}
