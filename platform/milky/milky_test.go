package milky

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ────────────────────────────────────────────────────────────────────────────
// Config tests
// ────────────────────────────────────────────────────────────────────────────

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig("http://localhost:6700")
	assert.Equal(t, "http://localhost:6700", cfg.BaseURL)
	assert.Equal(t, "", cfg.AccessToken)
	assert.Equal(t, 0, cfg.WorkerCount)
	assert.Equal(t, 128, cfg.EventBufferSize)
	assert.Equal(t, 3*time.Second, cfg.ReconnectDelay)
	assert.Equal(t, 0, cfg.MaxReconnect)
	assert.Equal(t, 10*time.Second, cfg.DialTimeout)
	assert.Equal(t, 15*time.Second, cfg.APITimeout)
}

func TestSetDefaults(t *testing.T) {
	t.Parallel()
	t.Run("fills zero values", func(t *testing.T) {
		var cfg Config
		cfg.setDefaults()
		assert.Equal(t, 128, cfg.EventBufferSize)
		assert.Equal(t, 3*time.Second, cfg.ReconnectDelay)
		assert.Equal(t, 10*time.Second, cfg.DialTimeout)
		assert.Equal(t, 15*time.Second, cfg.APITimeout)
	})

	t.Run("preserves non-zero values", func(t *testing.T) {
		cfg := Config{
			EventBufferSize: 256,
			ReconnectDelay:  5 * time.Second,
			DialTimeout:     20 * time.Second,
			APITimeout:      30 * time.Second,
		}
		cfg.setDefaults()
		assert.Equal(t, 256, cfg.EventBufferSize)
		assert.Equal(t, 5*time.Second, cfg.ReconnectDelay)
		assert.Equal(t, 20*time.Second, cfg.DialTimeout)
		assert.Equal(t, 30*time.Second, cfg.APITimeout)
	})

	t.Run("handles negative values", func(t *testing.T) {
		cfg := Config{
			EventBufferSize: -1,
			ReconnectDelay:  -time.Second,
		}
		cfg.setDefaults()
		assert.Equal(t, 128, cfg.EventBufferSize)
		assert.Equal(t, 3*time.Second, cfg.ReconnectDelay)
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Adapter construction
// ────────────────────────────────────────────────────────────────────────────

func TestNewAdapter(t *testing.T) {
	t.Parallel()
	t.Run("valid config returns adapter", func(t *testing.T) {
		adapter, err := NewAdapter(Config{BaseURL: "http://localhost:6700"})
		require.NoError(t, err)
		require.NotNil(t, adapter)
	})

	t.Run("empty base URL returns error", func(t *testing.T) {
		adapter, err := NewAdapter(Config{})
		assert.Error(t, err)
		assert.Nil(t, adapter)
		assert.Contains(t, err.Error(), "BaseURL")
	})
}

func TestAdapterPlatform(t *testing.T) {
	t.Parallel()
	adapter, err := NewAdapter(Config{BaseURL: "http://localhost:6700"})
	require.NoError(t, err)
	assert.Equal(t, "milky", adapter.Platform())
}

func TestAdapterCapabilities(t *testing.T) {
	t.Parallel()
	adapter, err := NewAdapter(Config{BaseURL: "http://localhost:6700"})
	require.NoError(t, err)

	caps := adapter.Capabilities()
	assert.False(t, caps.Markdown)
	assert.False(t, caps.Buttons)
	assert.False(t, caps.MultiAttachment)
	assert.False(t, caps.MessageEdit)
	assert.True(t, caps.MessageDelete)
	assert.False(t, caps.Embeds)
	assert.True(t, caps.FileUpload)
	assert.False(t, caps.GuildSupport)
	assert.True(t, caps.Reactions)
	assert.True(t, caps.ThreadReply)
	assert.False(t, caps.TypingIndicator)
	assert.True(t, caps.MentionAll)
	assert.False(t, caps.VoiceChannel)
}

func TestAdapterCapabilitiesHas(t *testing.T) {
	t.Parallel()
	adapter, err := NewAdapter(Config{BaseURL: "http://localhost:6700"})
	require.NoError(t, err)

	caps := adapter.Capabilities()
	assert.True(t, caps.Has(platform.CapMessageDelete))
	assert.True(t, caps.Has(platform.CapFileUpload))
	assert.True(t, caps.Has(platform.CapReactions))
	assert.True(t, caps.Has(platform.CapThreadReply))
	assert.True(t, caps.Has(platform.CapMentionAll))
	assert.False(t, caps.Has(platform.CapMarkdown))
	assert.False(t, caps.Has(platform.CapButtons|platform.CapEmbeds))
}

func TestAdapterIsRunning(t *testing.T) {
	t.Parallel()
	adapter, err := NewAdapter(Config{BaseURL: "http://localhost:6700"})
	require.NoError(t, err)
	assert.False(t, adapter.IsRunning())
}

func TestAdapterSender(t *testing.T) {
	t.Parallel()
	adapter, err := NewAdapter(Config{BaseURL: "http://localhost:6700"})
	require.NoError(t, err)
	sender := adapter.Sender()
	require.NotNil(t, sender)
}

func TestAdapterImplementsPlatformInterfaces(t *testing.T) {
	t.Parallel()
	adapter, err := NewAdapter(Config{BaseURL: "http://localhost:6700"})
	require.NoError(t, err)

	var _ platform.Adapter = adapter
	var _ platform.RecoverableAdapter = adapter
	var _ platform.BotIdentity = adapter
}

func TestAdapterSenderImplementsOptionalInterfaces(t *testing.T) {
	t.Parallel()
	adapter, err := NewAdapter(Config{BaseURL: "http://localhost:6700"})
	require.NoError(t, err)
	sender := adapter.Sender()

	_, ok := sender.(platform.MessageDeleter)
	assert.True(t, ok)

	_, ok = sender.(platform.GroupManager)
	assert.True(t, ok)

	_, ok = sender.(platform.AutoModerator)
	assert.True(t, ok)

	_, ok = sender.(platform.InvitationHandler)
	assert.True(t, ok)

	_, ok = sender.(platform.ReactionSender)
	assert.True(t, ok)
}

// ────────────────────────────────────────────────────────────────────────────
// BotIdentity initial state
// ────────────────────────────────────────────────────────────────────────────

func TestAdapterBotIdentityInitialState(t *testing.T) {
	t.Parallel()
	adapter, err := NewAdapter(Config{BaseURL: "http://localhost:6700"})
	require.NoError(t, err)
	assert.Equal(t, "", adapter.BotID())
	assert.Equal(t, "", adapter.BotName())
}

// ────────────────────────────────────────────────────────────────────────────
// OnDisconnect
// ────────────────────────────────────────────────────────────────────────────

func TestAdapterOnDisconnect(t *testing.T) {
	t.Parallel()
	adapter, err := NewAdapter(Config{BaseURL: "http://localhost:6700"})
	require.NoError(t, err)

	unregister := adapter.OnDisconnect(func(err error) {})
	require.NotNil(t, unregister)

	unregister()

	unregister = adapter.OnDisconnect(nil)
	require.NotNil(t, unregister)
	unregister()
}

// ────────────────────────────────────────────────────────────────────────────
// PlatformID constant
// ────────────────────────────────────────────────────────────────────────────

func TestPlatformID(t *testing.T) {
	assert.Equal(t, "milky", PlatformID)
}

// ────────────────────────────────────────────────────────────────────────────
// Scene constants
// ────────────────────────────────────────────────────────────────────────────

func TestSceneConstants(t *testing.T) {
	assert.Equal(t, "group", sceneGroup)
	assert.Equal(t, "friend", sceneFriend)
	assert.Equal(t, "temp", sceneTemp)
	assert.Equal(t, "group", SceneGroup)
	assert.Equal(t, "friend", SceneFriend)
	assert.Equal(t, "temp", SceneTemp)
}

// ────────────────────────────────────────────────────────────────────────────
// buildWSURL
// ────────────────────────────────────────────────────────────────────────────

func TestBuildWSURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		baseURL     string
		accessToken string
		want        string
	}{
		{"http no token", "http://127.0.0.1:6700", "", "ws://127.0.0.1:6700/event"},
		{"https no token", "https://example.com", "", "wss://example.com/event"},
		{"http with trailing slash", "http://localhost:6700/", "", "ws://localhost:6700/event"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildWSURL(tt.baseURL, tt.accessToken)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// chatInfoFromScene（ChatInfo.ID 语义对齐：纯数字 peerID + IsGroup/IsDM/Tokens）
// ────────────────────────────────────────────────────────────────────────────

func TestChatInfoFromScene(t *testing.T) {
	t.Parallel()
	t.Run("group", func(t *testing.T) {
		ci := chatInfoFromScene("group", 12345)
		assert.Equal(t, "12345", ci.ID)
		assert.True(t, ci.IsGroup)
		assert.False(t, ci.IsDM)
		assert.Equal(t, "group", ci.Tokens[TokenMessageScene])
	})

	t.Run("friend", func(t *testing.T) {
		ci := chatInfoFromScene("friend", 98765)
		assert.Equal(t, "98765", ci.ID)
		assert.False(t, ci.IsGroup)
		assert.False(t, ci.IsDM)
		assert.Equal(t, "friend", ci.Tokens[TokenMessageScene])
	})

	t.Run("temp treated as DM", func(t *testing.T) {
		ci := chatInfoFromScene("temp", 55555)
		assert.Equal(t, "55555", ci.ID)
		assert.False(t, ci.IsGroup)
		assert.True(t, ci.IsDM)
		assert.Equal(t, "temp", ci.Tokens[TokenMessageScene])
	})

	t.Run("empty scene keeps nil tokens", func(t *testing.T) {
		ci := chatInfoFromScene("", 12345)
		assert.Equal(t, "12345", ci.ID)
		assert.Nil(t, ci.Tokens)
	})
}

// decodeChatID 兼容旧缓存格式（"scene:peerID"）。
func TestDecodeChatID(t *testing.T) {
	t.Parallel()
	t.Run("legacy scene format", func(t *testing.T) {
		scene, peerID, ok := decodeChatID("group:12345")
		assert.True(t, ok)
		assert.Equal(t, "group", scene)
		assert.Equal(t, int64(12345), peerID)
	})

	t.Run("plain number without scene", func(t *testing.T) {
		scene, peerID, ok := decodeChatID("12345")
		assert.True(t, ok)
		assert.Equal(t, "", scene)
		assert.Equal(t, int64(12345), peerID)
	})

	t.Run("invalid returns false", func(t *testing.T) {
		_, _, ok := decodeChatID("not-a-number")
		assert.False(t, ok)

		_, _, ok = decodeChatID("group:not-a-number")
		assert.False(t, ok)

		_, _, ok = decodeChatID(":")
		assert.False(t, ok)
	})
}

// ────────────────────────────────────────────────────────────────────────────
// parseUin
// ────────────────────────────────────────────────────────────────────────────

func TestParseUIN(t *testing.T) {
	t.Parallel()
	t.Run("plain number", func(t *testing.T) {
		n, err := parseUin("12345", "test")
		assert.NoError(t, err)
		assert.Equal(t, int64(12345), n)
	})

	t.Run("with scene prefix", func(t *testing.T) {
		n, err := parseUin("group:12345", "test")
		assert.NoError(t, err)
		assert.Equal(t, int64(12345), n)
	})

	t.Run("invalid string", func(t *testing.T) {
		_, err := parseUin("abc", "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid")
	})

	t.Run("negative number", func(t *testing.T) {
		n, err := parseUin("-12345", "test")
		assert.NoError(t, err)
		assert.Equal(t, int64(-12345), n)
	})
}

func TestParseUINExported(t *testing.T) {
	t.Parallel()
	n, err := ParseUIN("12345")
	assert.NoError(t, err)
	assert.Equal(t, int64(12345), n)

	n, err = ParseUIN("group:12345")
	assert.NoError(t, err)
	assert.Equal(t, int64(12345), n)

	_, err = ParseUIN("invalid")
	assert.Error(t, err)
}

func TestFormatUIN(t *testing.T) {
	assert.Equal(t, "12345", FormatUIN(12345))
	assert.Equal(t, "0", FormatUIN(0))
	assert.Equal(t, "-1", FormatUIN(-1))
}

// ────────────────────────────────────────────────────────────────────────────
// parseInviteID
// ────────────────────────────────────────────────────────────────────────────

func TestParseInviteID(t *testing.T) {
	t.Parallel()
	t.Run("valid format", func(t *testing.T) {
		gid, seq, err := parseInviteID("123:456")
		assert.NoError(t, err)
		assert.Equal(t, int64(123), gid)
		assert.Equal(t, int64(456), seq)
	})

	t.Run("missing colon", func(t *testing.T) {
		_, _, err := parseInviteID("invalid")
		assert.Error(t, err)
	})

	t.Run("non-numeric parts", func(t *testing.T) {
		_, _, err := parseInviteID("abc:def")
		assert.Error(t, err)
	})

	t.Run("only first part", func(t *testing.T) {
		_, _, err := parseInviteID("123:")
		assert.Error(t, err)
	})
}

// ────────────────────────────────────────────────────────────────────────────
// SendResult
// ────────────────────────────────────────────────────────────────────────────

func TestSendResult(t *testing.T) {
	t.Parallel()
	now := time.Now()
	r := &SendResult{MessageSeq: 12345, SentAt: now}
	assert.Equal(t, int64(12345), r.MessageSeq)
	assert.Equal(t, now, r.SentAt)

	zero := &SendResult{}
	assert.Equal(t, int64(0), zero.MessageSeq)
	assert.True(t, zero.SentAt.IsZero())
}

// ────────────────────────────────────────────────────────────────────────────
// minDuration
// ────────────────────────────────────────────────────────────────────────────

func TestMinDuration(t *testing.T) {
	t.Parallel()
	assert.Equal(t, time.Second, minDuration(time.Second, 2*time.Second))
	assert.Equal(t, time.Second, minDuration(2*time.Second, time.Second))
	assert.Equal(t, time.Duration(0), minDuration(0, time.Second))
	assert.Equal(t, time.Duration(0), minDuration(time.Second, 0))
}

// ────────────────────────────────────────────────────────────────────────────
// OutgoingSegment conversion
// ────────────────────────────────────────────────────────────────────────────

func TestConvertOutgoingSegment(t *testing.T) {
	t.Parallel()

	t.Run("TextSegment", func(t *testing.T) {
		seg := convertOutgoingSegment(&TextSegment{Text: "hello"})
		require.NotNil(t, seg)
		assert.Equal(t, "text", seg.Type)
		assert.Equal(t, "hello", seg.Data.Text)
	})

	t.Run("MentionSegment", func(t *testing.T) {
		seg := convertOutgoingSegment(&MentionSegment{UserID: 12345})
		require.NotNil(t, seg)
		assert.Equal(t, "mention", seg.Type)
		assert.Equal(t, int64(12345), seg.Data.UserID)
	})

	t.Run("ReplySegment", func(t *testing.T) {
		seg := convertOutgoingSegment(&ReplySegment{MessageSeq: 999})
		require.NotNil(t, seg)
		assert.Equal(t, "reply", seg.Type)
		assert.Equal(t, int64(999), seg.Data.MessageSeq)
	})

	t.Run("ImageSegment default sub_type", func(t *testing.T) {
		seg := convertOutgoingSegment(&ImageSegment{URI: "https://example.com/img.jpg"})
		require.NotNil(t, seg)
		assert.Equal(t, "image", seg.Type)
		assert.Equal(t, "https://example.com/img.jpg", seg.Data.URI)
		assert.Equal(t, "normal", seg.Data.SubType)
	})

	t.Run("ImageSegment sticker sub_type", func(t *testing.T) {
		seg := convertOutgoingSegment(&ImageSegment{URI: "https://example.com/sticker.webp", SubType: "sticker"})
		require.NotNil(t, seg)
		assert.Equal(t, "image", seg.Type)
		assert.Equal(t, "sticker", seg.Data.SubType)
	})

	t.Run("RecordSegment", func(t *testing.T) {
		seg := convertOutgoingSegment(&RecordSegment{URI: "https://example.com/audio.mp3"})
		require.NotNil(t, seg)
		assert.Equal(t, "record", seg.Type)
		assert.Equal(t, "https://example.com/audio.mp3", seg.Data.URI)
	})

	t.Run("VideoSegment", func(t *testing.T) {
		seg := convertOutgoingSegment(&VideoSegment{URI: "https://example.com/vid.mp4"})
		require.NotNil(t, seg)
		assert.Equal(t, "video", seg.Type)
		assert.Equal(t, "https://example.com/vid.mp4", seg.Data.URI)
	})

	t.Run("FaceSegment", func(t *testing.T) {
		seg := convertOutgoingSegment(&FaceSegment{FaceID: "21", IsLarge: false})
		require.NotNil(t, seg)
		assert.Equal(t, "face", seg.Type)
		assert.Equal(t, "21", seg.Data.FaceID)
		assert.False(t, seg.Data.IsLarge)
	})

	t.Run("FaceSegment large", func(t *testing.T) {
		seg := convertOutgoingSegment(&FaceSegment{FaceID: "301", IsLarge: true})
		require.NotNil(t, seg)
		assert.True(t, seg.Data.IsLarge)
	})

	t.Run("MentionAllSegment", func(t *testing.T) {
		seg := convertOutgoingSegment(&MentionAllSegment{})
		require.NotNil(t, seg)
		assert.Equal(t, "mention_all", seg.Type)
	})

	t.Run("LightAppSegment", func(t *testing.T) {
		seg := convertOutgoingSegment(&LightAppSegment{JSONPayload: `{"app":"test"}`})
		require.NotNil(t, seg)
		assert.Equal(t, "light_app", seg.Type)
		assert.Equal(t, `{"app":"test"}`, seg.Data.JSONPayload)
	})

	t.Run("ForwardSegment", func(t *testing.T) {
		seg := convertOutgoingSegment(&ForwardSegment{
			Messages: []ForwardEntry{
				{UserID: 1, SenderName: "Alice", Text: "hi"},
				{UserID: 2, SenderName: "Bob", Text: "hello"},
			},
			Title: "Chat",
		})
		require.NotNil(t, seg)
		assert.Equal(t, "forward", seg.Type)
		assert.Len(t, seg.Data.ForwardMessages, 2)
		assert.Equal(t, int64(1), seg.Data.ForwardMessages[0].UserID)
		assert.Equal(t, "Alice", seg.Data.ForwardMessages[0].SenderName)
		assert.Equal(t, "Chat", seg.Data.Title)
	})

	t.Run("ForwardSegment with complex segments", func(t *testing.T) {
		seg := convertOutgoingSegment(&ForwardSegment{
			Messages: []ForwardEntry{
				{
					UserID: 1, SenderName: "Alice",
					Segments: []OutgoingSegment{&ImageSegment{URI: "https://example.com/img.jpg"}},
				},
			},
		})
		require.NotNil(t, seg)
		assert.Len(t, seg.Data.ForwardMessages, 1)
		assert.Len(t, seg.Data.ForwardMessages[0].Segments, 1)
		assert.Equal(t, "image", seg.Data.ForwardMessages[0].Segments[0].Type)
	})

	t.Run("nil for unknown type", func(t *testing.T) {
		result := convertOutgoingSegment(nil)
		assert.Nil(t, result)
	})
}

// ────────────────────────────────────────────────────────────────────────────
// buildWireSegments
// ────────────────────────────────────────────────────────────────────────────

func TestBuildWireSegments(t *testing.T) {
	t.Parallel()
	segs := buildWireSegments([]OutgoingSegment{
		&TextSegment{Text: "hello"},
		&FaceSegment{FaceID: "21"},
		&MentionSegment{UserID: 10001},
	})
	assert.Len(t, segs, 3)
	assert.Equal(t, "text", segs[0].Type)
	assert.Equal(t, "face", segs[1].Type)
	assert.Equal(t, "mention", segs[2].Type)
}

func TestBuildWireSegmentsEmpty(t *testing.T) {
	t.Parallel()
	segs := buildWireSegments(nil)
	assert.Empty(t, segs)

	segs = buildWireSegments([]OutgoingSegment{})
	assert.Empty(t, segs)
}

// ────────────────────────────────────────────────────────────────────────────
// MessageExtra / ApplyExtra / extractExtra
// ────────────────────────────────────────────────────────────────────────────

func TestMessageExtraRoundTrip(t *testing.T) {
	t.Parallel()
	msg := platform.TextMessage("hello")
	extra := MessageExtra{Scene: SceneTemp, Segments: []OutgoingSegment{&FaceSegment{FaceID: "21"}}}
	msg = ApplyExtra(msg, extra)

	extracted := extractExtra(msg)
	assert.Equal(t, SceneTemp, extracted.Scene)
	require.Len(t, extracted.Segments, 1)
	faceSeg, ok := extracted.Segments[0].(*FaceSegment)
	require.True(t, ok)
	assert.Equal(t, "21", faceSeg.FaceID)
}

func TestMessageExtraEmpty(t *testing.T) {
	t.Parallel()
	msg := platform.TextMessage("hello")
	extracted := extractExtra(msg)
	assert.Equal(t, "", extracted.Scene)
	assert.Nil(t, extracted.Segments)
}

func TestMessageExtraNoExtraKey(t *testing.T) {
	t.Parallel()
	msg := platform.TextMessage("hello").WithExtra("other_key", "value")
	extracted := extractExtra(msg)
	assert.Equal(t, "", extracted.Scene)
	assert.Nil(t, extracted.Segments)
}

// ────────────────────────────────────────────────────────────────────────────
// Token constants
// ────────────────────────────────────────────────────────────────────────────

func TestTokenConstants(t *testing.T) {
	assert.Equal(t, "milky_message_scene", TokenMessageScene)
	assert.Equal(t, "milky_friend_uid", TokenFriendUID)
	assert.Equal(t, "milky_notification_seq", TokenNotificationSeq)
	assert.Equal(t, "milky_notification_type", TokenNotificationType)
	assert.Equal(t, "milky_invitation_seq", TokenInvitationSeq)
}

// ────────────────────────────────────────────────────────────────────────────
// Attachment meta types
// ────────────────────────────────────────────────────────────────────────────

func TestImageSegmentMeta(t *testing.T) {
	m := &ImageSegmentMeta{ResourceID: "res-123", SubType: "normal"}
	assert.Equal(t, "res-123", m.ResourceID)
	assert.Equal(t, "normal", m.SubType)
}

func TestRecordSegmentMeta(t *testing.T) {
	m := &RecordSegmentMeta{ResourceID: "res-456", Duration: 30}
	assert.Equal(t, "res-456", m.ResourceID)
	assert.Equal(t, 30, m.Duration)
}

func TestVideoSegmentMeta(t *testing.T) {
	m := &VideoSegmentMeta{ResourceID: "res-789", Duration: 120}
	assert.Equal(t, "res-789", m.ResourceID)
	assert.Equal(t, 120, m.Duration)
}

func TestFileSegmentMeta(t *testing.T) {
	m := &FileSegmentMeta{FileID: "fid-1", FileName: "doc.pdf", FileSize: 1024, FileHash: "abc123"}
	assert.Equal(t, "fid-1", m.FileID)
	assert.Equal(t, "doc.pdf", m.FileName)
	assert.Equal(t, int64(1024), m.FileSize)
	assert.Equal(t, "abc123", m.FileHash)
}

func TestFaceSegmentMeta(t *testing.T) {
	m := &FaceSegmentMeta{FaceID: "21", IsLarge: true}
	assert.Equal(t, "21", m.FaceID)
	assert.True(t, m.IsLarge)
}

func TestMarketFaceSegmentMeta(t *testing.T) {
	m := &MarketFaceSegmentMeta{EmojiPackageID: 1, EmojiID: "e123", Key: "k", Summary: "sum", URL: "url"}
	assert.Equal(t, 1, m.EmojiPackageID)
	assert.Equal(t, "e123", m.EmojiID)
}

func TestLightAppSegmentMeta(t *testing.T) {
	m := &LightAppSegmentMeta{AppName: "test", JSONPayload: "{}"}
	assert.Equal(t, "test", m.AppName)
	assert.Equal(t, "{}", m.JSONPayload)
}

func TestXMLSegmentMeta(t *testing.T) {
	m := &XMLSegmentMeta{ServiceID: 1, XMLPayload: "<msg/>"}
	assert.Equal(t, 1, m.ServiceID)
	assert.Equal(t, "<msg/>", m.XMLPayload)
}

// ────────────────────────────────────────────────────────────────────────────
// Segment interface compliance (compile-time)
// ────────────────────────────────────────────────────────────────────────────

var _ OutgoingSegment = (*TextSegment)(nil)
var _ OutgoingSegment = (*MentionSegment)(nil)
var _ OutgoingSegment = (*ReplySegment)(nil)
var _ OutgoingSegment = (*ImageSegment)(nil)
var _ OutgoingSegment = (*RecordSegment)(nil)
var _ OutgoingSegment = (*VideoSegment)(nil)
var _ OutgoingSegment = (*FaceSegment)(nil)
var _ OutgoingSegment = (*MentionAllSegment)(nil)
var _ OutgoingSegment = (*LightAppSegment)(nil)
var _ OutgoingSegment = (*ForwardSegment)(nil)
