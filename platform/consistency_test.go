package platform_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/discord"
	"github.com/KomeiDiSanXian/remilia/platform/milky"

	"github.com/KomeiDiSanXian/remilia/platform/satori"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testAdapters 构造所有真实平台的未启动适配器（仅用于接口/能力校验）。
//
// 注意：
//   - onebot 未连接时 Sender() 返回 platform.NoopSender（连接后才有真实 Sender），
//     其能力一致性由 platform/onebot 包内的白盒测试验证；
//   - telegram.NewAdapter 会立即调用 getMe 验证 token（真实网络请求），
//     此处不构造，其能力校验见各平台包内测试。
func testAdapters(t *testing.T) map[string]platform.Adapter {
	t.Helper()
	milkyA, err := milky.NewAdapter(milky.Config{BaseURL: "http://127.0.0.1:6700"})
	require.NoError(t, err)
	satoriA, err := satori.NewAdapter(satori.DefaultConfig("http://localhost:5140", "test", "1234567890"))
	require.NoError(t, err)
	discordA, err := discord.NewAdapter("test-token")
	require.NoError(t, err)

	return map[string]platform.Adapter{
		"milky":   milkyA,
		"satori":  satoriA,
		"discord": discordA,
	}
}

// TestCapabilitiesInterfaceConsistency 校验各平台 Capabilities() 的能力声明
// 与 Sender 实际实现的可选接口一致（P5 设计校验）。
//
// 规则：
//   - Capabilities.MessageDelete=true  ⇒ Sender 必须实现 platform.MessageDeleter
//   - Capabilities.MessageEdit=true    ⇒ Sender 必须实现 platform.MessageEditor
//   - Capabilities.Reactions=true      ⇒ Sender 必须实现 platform.ReactionSender
//   - Capabilities.TypingIndicator=true ⇒ Sender 必须实现 platform.TypingNotifier
//
// 反向不强制：实现了接口但声明为 false 是"能力声明保守"，允许。
func TestCapabilitiesInterfaceConsistency(t *testing.T) {
	for name, adapter := range testAdapters(t) {
		t.Run(name, func(t *testing.T) {
			sender := adapter.Sender()
			require.NotNil(t, sender, "Sender() 不应为 nil")
			caps := adapter.Capabilities()

			if caps.MessageDelete {
				assert.Implements(t, (*platform.MessageDeleter)(nil), sender,
					"Capabilities.MessageDelete=true 但未实现 MessageDeleter")
			}
			if caps.MessageEdit {
				assert.Implements(t, (*platform.MessageEditor)(nil), sender,
					"Capabilities.MessageEdit=true 但未实现 MessageEditor")
			}
			if caps.Reactions {
				assert.Implements(t, (*platform.ReactionSender)(nil), sender,
					"Capabilities.Reactions=true 但未实现 ReactionSender")
			}
			if caps.TypingIndicator {
				assert.Implements(t, (*platform.TypingNotifier)(nil), sender,
					"Capabilities.TypingIndicator=true 但未实现 TypingNotifier")
			}
		})
	}
}

// TestAllPlatformsProvideAPI 校验所有平台 Sender 都实现 APIProvider
// （平台 API 门面，见 platform/extension.go）。
func TestAllPlatformsProvideAPI(t *testing.T) {
	for name, adapter := range testAdapters(t) {
		t.Run(name, func(t *testing.T) {
			api := platform.GetPlatformAPI(adapter.Sender())
			assert.NotNil(t, api, "Sender 应实现 platform.APIProvider")
		})
	}
}
