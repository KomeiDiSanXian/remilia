package wechat_test

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform/wechat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAdapter 构造与基本接口。
func TestNewAdapter(t *testing.T) {
	adapter, err := wechat.NewAdapter(wechat.Config{BotID: "bot-1", Secret: "secret"})
	require.NoError(t, err)
	require.NotNil(t, adapter)

	assert.Equal(t, "wechat", adapter.Platform())
	assert.False(t, adapter.IsRunning())
	assert.NotNil(t, adapter.Sender())
	assert.Equal(t, "bot-1", adapter.BotID())
	assert.Empty(t, adapter.BotName(), "企业微信没有机器人资料接口，名称应为空")
}

// TestWechatCapabilities 能力声明（长连接模式）。
func TestWechatCapabilities(t *testing.T) {
	adapter, err := wechat.NewAdapter(wechat.Config{BotID: "bot", Secret: "s"})
	require.NoError(t, err)

	caps := adapter.Capabilities()
	assert.True(t, caps.Markdown)
	assert.True(t, caps.Buttons)
	assert.False(t, caps.MultiAttachment)
	assert.False(t, caps.MessageEdit)
	assert.False(t, caps.MessageDelete)
	assert.False(t, caps.Embeds)
	assert.True(t, caps.FileUpload, "长连接支持分片上传素材")
	assert.False(t, caps.GuildSupport)
	assert.False(t, caps.Reactions)
	assert.False(t, caps.ThreadReply)
	assert.False(t, caps.TypingIndicator)
	assert.False(t, caps.MentionAll)
	assert.False(t, caps.VoiceChannel)
	assert.Equal(t, 20480, caps.MaxTextLength)
	assert.Equal(t, 20, caps.MaxAttachmentMB)
}

// TestWechatWebhookCapabilities URL 回调模式没有素材上传接口。
func TestWechatWebhookCapabilities(t *testing.T) {
	adapter, err := wechat.NewAdapter(wechat.Config{
		BotID:          "bot",
		Mode:           wechat.ModeWebhook,
		Token:          "t",
		EncodingAESKey: "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG",
		Webhook:        wechat.WebhookConfig{ListenAddr: ":0"},
	})
	require.NoError(t, err)
	assert.False(t, adapter.Capabilities().FileUpload)
}

// TestWechatConfigValidation 配置校验必须早于任何网络行为。
func TestWechatConfigValidation(t *testing.T) {
	_, err := wechat.NewAdapter(wechat.Config{})
	require.Error(t, err, "缺少 bot_id")

	_, err = wechat.NewAdapter(wechat.Config{BotID: "bot"})
	require.Error(t, err, "ws 模式缺少 secret")

	_, err = wechat.NewAdapter(wechat.Config{
		BotID: "bot", Mode: wechat.ModeWebhook, Token: "t",
	})
	require.Error(t, err, "webhook 模式缺少 encoding_aes_key")
}

// TestWechatStopReturnsNil 未启动时停止应安全返回。
func TestWechatStopReturnsNil(t *testing.T) {
	adapter, err := wechat.NewAdapter(wechat.Config{BotID: "bot", Secret: "s"})
	require.NoError(t, err)
	assert.NoError(t, adapter.Stop(context.Background()))
}

// TestWechatHealthDetail 健康详情反映接入方式。
func TestWechatHealthDetail(t *testing.T) {
	adapter, err := wechat.NewAdapter(wechat.Config{BotID: "bot", Secret: "s"})
	require.NoError(t, err)

	detail := adapter.HealthDetail()
	assert.Equal(t, wechat.ModeWS, detail["connection"])
	assert.Equal(t, "bot", detail["bot_id"])
	assert.Equal(t, false, detail["subscribed"], "未启动时不应报告已订阅")
}

// TestWechatModeConstants 接入方式常量与配置解析一致。
func TestWechatModeConstants(t *testing.T) {
	assert.Equal(t, "ws", wechat.ModeWS)
	assert.Equal(t, "webhook", wechat.ModeWebhook)
}
