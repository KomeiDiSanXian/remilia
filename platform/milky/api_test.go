package milky

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToMessageSegment(t *testing.T) {
	in := incomingSegment{
		Type: "text",
		Data: incomingSegmentData{Text: "hello"},
	}
	out := toMessageSegment(in)
	assert.Equal(t, "text", out.Type)
	assert.Equal(t, "hello", out.Data.Text)
}

func TestToMessageSegments(t *testing.T) {
	segs := toMessageSegments([]incomingSegment{
		{Type: "text", Data: incomingSegmentData{Text: "a"}},
		{Type: "face", Data: incomingSegmentData{FaceID: "21"}},
	})
	require.Len(t, segs, 2)
	assert.Equal(t, "a", segs[0].Data.Text)
	assert.Equal(t, "21", segs[1].Data.FaceID)
}

func TestToMessageSegments_Nil(t *testing.T) {
	result := toMessageSegments(nil)
	assert.Empty(t, result)
}

func TestToMessage(t *testing.T) {
	m := toMessage(incomingMessage{
		MessageScene: "group",
		PeerID:       123,
		MessageSeq:   456,
		SenderID:     789,
		Time:         1000,
		Segments: []incomingSegment{
			{Type: "text", Data: incomingSegmentData{Text: "hi"}},
		},
	})
	assert.Equal(t, "group", m.Scene)
	assert.Equal(t, int64(123), m.PeerID)
	assert.Equal(t, int64(456), m.MessageSeq)
	require.Len(t, m.Segments, 1)
	assert.Equal(t, "hi", m.Segments[0].Data.Text)
}

func TestToFriendInfo(t *testing.T) {
	f := toFriendInfo(friendInfoJSON{
		UserID:   1001,
		Nickname: "Alice",
		Sex:      "female",
		QID:      "qid1",
		Remark:   "my friend",
		Category: friendCategoryJSON{CategoryID: 1, CategoryName: "好友"},
	})
	assert.Equal(t, int64(1001), f.UserID)
	assert.Equal(t, "Alice", f.Nickname)
	assert.Equal(t, "my friend", f.Remark)
	assert.Equal(t, 1, f.Category.CategoryID)
}

func TestToGroupInfo(t *testing.T) {
	g := toGroupInfo(groupInfoJSON{
		GroupID:        555,
		GroupName:      "Test Group",
		MemberCount:    100,
		MaxMemberCount: 200,
		Remark:         "remark",
		CreatedTime:    1000,
	})
	assert.Equal(t, int64(555), g.GroupID)
	assert.Equal(t, "Test Group", g.GroupName)
	assert.Equal(t, 100, g.MemberCount)
}

func TestToGroupMemberInfo(t *testing.T) {
	m := toGroupMemberInfo(groupMemberInfoJSON{
		UserID:   1001,
		GroupID:  555,
		Nickname: "Bob",
		Card:     "Bobby",
		Role:     "admin",
	})
	assert.Equal(t, int64(1001), m.UserID)
	assert.Equal(t, "admin", m.Role)
}

func TestApiError_Error(t *testing.T) {
	err := &apiError{Endpoint: "send_message", Retcode: -400, Message: "bad request"}
	assert.Equal(t, "milky API error [send_message] retcode=-400: bad request", err.Error())
}

func TestNewMilkyClient(t *testing.T) {
	client := newMilkyClient(Config{BaseURL: "http://localhost:6700"})
	assert.NotNil(t, client)
	assert.Equal(t, "http://localhost:6700", client.baseURL)
	assert.Equal(t, "", client.accessToken)
}

func TestNewMilkyClient_WithToken(t *testing.T) {
	client := newMilkyClient(Config{BaseURL: "http://localhost:6700", AccessToken: "secret123"})
	assert.Equal(t, "secret123", client.accessToken)
}

func TestNewMilkyClient_ZeroTimeout(t *testing.T) {
	client := newMilkyClient(Config{BaseURL: "http://localhost:6700"})
	assert.NotNil(t, client.httpClient)
}

func TestParseUin(t *testing.T) {
	n, err := parseUin("12345", "test")
	require.NoError(t, err)
	assert.Equal(t, int64(12345), n)
}

func TestParseUin_WithScenePrefix(t *testing.T) {
	n, err := parseUin("group:12345", "test")
	require.NoError(t, err)
	assert.Equal(t, int64(12345), n)
}

func TestParseUin_Invalid(t *testing.T) {
	_, err := parseUin("abc", "test")
	assert.Error(t, err)
}

func TestWrapSendError_Nil(t *testing.T) {
	assert.Nil(t, wrapSendError(nil, "chat1", "group"))
}

func TestWrapSendError_ApiError(t *testing.T) {
	apiErr := &apiError{Endpoint: "send", Retcode: -403, Message: "perm denied"}
	err := wrapSendError(apiErr, "chat1", "group")
	assert.Error(t, err)
	se, ok := err.(*platform.SendError)
	assert.True(t, ok)
	assert.Equal(t, platform.SendErrPermDenied, se.Code)
}

func TestWrapSendError_ApiError400(t *testing.T) {
	apiErr := &apiError{Endpoint: "send", Retcode: -400, Message: "invalid"}
	err := wrapSendError(apiErr, "chat1", "group")
	se, ok := err.(*platform.SendError)
	assert.True(t, ok)
	assert.Equal(t, platform.SendErrInvalidTarget, se.Code)
}

func TestWrapSendError_GenericError(t *testing.T) {
	err := wrapSendError(assert.AnError, "chat1", "group")
	se, ok := err.(*platform.SendError)
	assert.True(t, ok)
	assert.Equal(t, platform.SendErrNetworkError, se.Code)
}

func TestFileAttachments(t *testing.T) {
	atts := []platform.Attachment{
		{Kind: platform.AttachmentKindImage, URL: "https://ex.com/img.jpg"},
		{Kind: platform.AttachmentKindFile, URL: "https://ex.com/file.pdf"},
		{Kind: platform.AttachmentKindAudio, URL: "https://ex.com/audio.mp3"},
		{Kind: platform.AttachmentKindFile, URL: "https://ex.com/doc.txt"},
	}
	result := fileAttachments(atts)
	require.Len(t, result, 2)
	assert.Equal(t, "https://ex.com/file.pdf", result[0].URL)
	assert.Equal(t, "https://ex.com/doc.txt", result[1].URL)
}

func TestFileAttachments_Nil(t *testing.T) {
	assert.Nil(t, fileAttachments(nil))
	assert.Empty(t, fileAttachments([]platform.Attachment{}))
}

func TestBuildForwardSegment(t *testing.T) {
	seg := buildForwardSegment(&ForwardSegment{
		Messages: []ForwardEntry{
			{UserID: 1, SenderName: "Alice", Text: "hi"},
			{UserID: 2, SenderName: "Bob", Segments: []OutgoingSegment{&TextSegment{Text: "hello"}}},
		},
		Title: "Chat",
	})
	assert.Equal(t, "forward", seg.Type)
	require.Len(t, seg.Data.ForwardMessages, 2)
	assert.Equal(t, "hi", seg.Data.ForwardMessages[0].Segments[0].Data.Text)
	assert.Equal(t, "hello", seg.Data.ForwardMessages[1].Segments[0].Data.Text)
}

func TestBuildOutgoingSegments_ReplyAndMentions(t *testing.T) {
	msg := platform.TextMessage("hello").WithReply("5000").WithMentions("12345")
	segs := buildOutgoingSegments(msg)
	require.Len(t, segs, 3)
	assert.Equal(t, "reply", segs[0].Type)
	assert.Equal(t, "mention", segs[1].Type)
	assert.Equal(t, "text", segs[2].Type)
	assert.Equal(t, "hello", segs[2].Data.Text)
}

func TestBuildOutgoingSegments_MarkdownFallback(t *testing.T) {
	msg := platform.MarkdownMessage("**bold**")
	segs := buildOutgoingSegments(msg)
	require.Len(t, segs, 1)
	assert.Equal(t, "text", segs[0].Type)
	assert.Equal(t, "**bold**", segs[0].Data.Text)
}

func TestBuildOutgoingSegments_ImageAttachment(t *testing.T) {
	msg := platform.TextMessage("pic").WithAttachments(
		platform.Attachment{URL: "https://ex.com/img.jpg", Kind: platform.AttachmentKindImage},
	)
	segs := buildOutgoingSegments(msg)
	require.Len(t, segs, 2)
	assert.Equal(t, "image", segs[1].Type)
	assert.Equal(t, "https://ex.com/img.jpg", segs[1].Data.URI)
}

func TestBuildOutgoingSegments_FileAttachmentSkipped(t *testing.T) {
	msg := platform.TextMessage("text").WithAttachments(
		platform.Attachment{URL: "https://ex.com/file.pdf", Kind: platform.AttachmentKindFile},
	)
	segs := buildOutgoingSegments(msg)
	require.Len(t, segs, 1)
	assert.Equal(t, "text", segs[0].Type)
}

func TestBuildOutgoingSegments_Base64Data(t *testing.T) {
	msg := platform.TextMessage("pic").WithAttachments(
		platform.Attachment{Data: []byte("fakeimage"), Kind: platform.AttachmentKindImage, Name: "img.png"},
	)
	segs := buildOutgoingSegments(msg)
	require.Len(t, segs, 2)
	assert.Equal(t, "image", segs[1].Type)
	assert.Contains(t, segs[1].Data.URI, "base64://")
}

func TestBuildOutgoingSegments_Extra(t *testing.T) {
	msg := platform.TextMessage("hello")
	msg = ApplyExtra(msg, MessageExtra{Segments: []OutgoingSegment{&FaceSegment{FaceID: "21"}}})
	segs := buildOutgoingSegments(msg)
	require.Len(t, segs, 2)
	assert.Equal(t, "face", segs[1].Type)
}

func TestBuildOutgoingSegments_Empty(t *testing.T) {
	segs := buildOutgoingSegments(platform.TextMessage(""))
	assert.Empty(t, segs)
}

func TestBuildOutgoingSegments_BadReplyID(t *testing.T) {
	msg := platform.TextMessage("hello").WithReply("not-a-number")
	segs := buildOutgoingSegments(msg)
	require.Len(t, segs, 1)
	assert.Equal(t, "text", segs[0].Type)
}

func TestBuildOutgoingSegments_BadMention(t *testing.T) {
	msg := platform.TextMessage("hello").WithMentions("not-a-number")
	segs := buildOutgoingSegments(msg)
	require.Len(t, segs, 1)
	assert.Equal(t, "text", segs[0].Type)
}

func TestConvertOutgoingSegment_UnknownType(t *testing.T) {
	result := convertOutgoingSegment(nil)
	assert.Nil(t, result)

	result = convertOutgoingSegment(&struct{ OutgoingSegment }{})
	assert.Nil(t, result)
}

func TestConvertOutgoingSegment_MentionAll(t *testing.T) {
	seg := convertOutgoingSegment(&MentionAllSegment{})
	require.NotNil(t, seg)
	assert.Equal(t, "mention_all", seg.Type)
}

func TestConvertOutgoingSegment_LightApp(t *testing.T) {
	seg := convertOutgoingSegment(&LightAppSegment{JSONPayload: `{"app":"test"}`})
	require.NotNil(t, seg)
	assert.Equal(t, "light_app", seg.Type)
}

func TestSegmentConstants(t *testing.T) {
	assert.Equal(t, "group", sceneGroup)
	assert.Equal(t, "friend", sceneFriend)
	assert.Equal(t, "temp", sceneTemp)
}

func TestLoginInfo(t *testing.T) {
	info := LoginInfo{Uin: 12345, Nickname: "Bot"}
	assert.Equal(t, int64(12345), info.Uin)
	assert.Equal(t, "Bot", info.Nickname)
}

func TestImplInfo(t *testing.T) {
	info := ImplInfo{ImplName: "Milky", ImplVersion: "1.0", QQProtocolVersion: "11", QQProtocolType: "Android", MilkyVersion: "2.0"}
	assert.Equal(t, "Milky", info.ImplName)
	assert.Equal(t, "1.0", info.ImplVersion)
}

func TestUserProfile(t *testing.T) {
	p := UserProfile{Nickname: "User", Age: 25, Sex: "male", Level: 10}
	assert.Equal(t, 25, p.Age)
	assert.Equal(t, 10, p.Level)
}
