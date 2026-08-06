package onebot

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// TestSenderCapabilitiesConsistency 校验 onebot 的能力声明与 Sender 实现一致
// （平台级一致性测试的 onebot 补充：未连接时 ForwardWSAdapter.Sender()
// 返回 NoopSender，因此这里用白盒构造真实 Sender 验证）。
func TestSenderCapabilitiesConsistency(t *testing.T) {
	m := &mockAPIClient{data: map[string]string{}}
	sender := newSender(m)
	caps := onebotCapabilities()

	if caps.MessageDelete {
		var _ platform.MessageDeleter = sender
	}
	if caps.MessageEdit {
		assert.Fail(t, "Capabilities.MessageEdit=true 但 onebot 未实现 MessageEditor")
	}
	if caps.Reactions {
		assert.Fail(t, "Capabilities.Reactions=true 但 onebot 未实现 ReactionSender")
	}
	if caps.TypingIndicator {
		assert.Fail(t, "Capabilities.TypingIndicator=true 但 onebot 未实现 TypingNotifier")
	}

	// 平台 API 门面。
	var _ platform.APIProvider = sender
	// 标准可选接口（当前声明与实现均齐全）。
	var _ platform.GroupManager = sender
	var _ platform.GroupSettings = sender
	var _ platform.MessageHistoryProvider = sender
	var _ platform.AnnouncementManager = sender
	var _ platform.SessionNotifier = sender
	var _ platform.InvitationHandler = sender
	var _ platform.AutoModerator = sender

}
