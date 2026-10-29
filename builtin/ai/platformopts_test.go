package ai

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	"github.com/stretchr/testify/assert"
)

// mediaOriginsStub 只实现 MediaOrigins —— 插件依赖的就是这么窄的接口，
// 不需要（也不应该拿到）能注册/注销适配器的 *platform.Registry。
type mediaOriginsStub []string

// MediaOrigins 实现 platform.MediaOriginSource。
func (s mediaOriginsStub) MediaOrigins() []string { return s }

// TestAttachmentOriginsMergesPlatformSource 附录来源与运维名单合并，且不修改
// 调用方传入的切片（配置对象可能被多处读取）。
func TestAttachmentOriginsMergesPlatformSource(t *testing.T) {
	configured := []string{"127.0.0.1:3000"}

	got := attachmentOrigins(configured, mediaOriginsStub{"media.lan:8080"})
	assert.Equal(t, []string{"127.0.0.1:3000", "media.lan:8080"}, got)
	assert.Equal(t, []string{"127.0.0.1:3000"}, configured, "不得就地修改配置切片")

	// 未注入平台来源（测试/独立使用）时只保留运维名单。
	assert.Equal(t, []string{"127.0.0.1:3000"}, attachmentOrigins(configured, nil))
	assert.Empty(t, attachmentOrigins(nil, nil))
}

// TestWithMediaOriginSourceAppliesToPlugin 注入只读视图后 Setup 期能取到来源。
func TestWithMediaOriginSourceAppliesToPlugin(t *testing.T) {
	p := &Plugin{}
	WithMediaOriginSource(mediaOriginsStub{"http://127.0.0.1:3000"})(p)

	if assert.NotNil(t, p.mediaSource) {
		assert.Equal(t, []string{"http://127.0.0.1:3000"}, p.mediaSource.MediaOrigins())
	}
}

// TestWithDataDirAppliesToPlugin 持久化目录可注入，默认值与仓库其他插件一致。
func TestWithDataDirAppliesToPlugin(t *testing.T) {
	p := &Plugin{dataDir: defaultDataDir}
	WithDataDir("/var/lib/remilia")(p)
	assert.Equal(t, "/var/lib/remilia", p.dataDir)
	assert.Equal(t, "data", defaultDataDir)
}

// TestWithHistoryAppliesToPlugin 消息历史可注入，插件不再只能依赖包级单例。
func TestWithHistoryAppliesToPlugin(t *testing.T) {
	history := messagelog.New(16)
	p := &Plugin{}
	WithHistory(history)(p)
	assert.Same(t, history, p.history)
}
