package sauce

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// mediaOriginsStub 只实现 MediaOrigins —— 插件依赖的就是这么窄的接口，
// 不需要（也不应该拿到）能注册/注销适配器的 *platform.Registry。
type mediaOriginsStub []string

// MediaOrigins 实现 platform.MediaOriginSource。
func (s mediaOriginsStub) MediaOrigins() []string { return s }

// TestMediaOriginsIncludesPlatformSource 图片下载的受信来源 = 运维名单 +
// 适配器自声明；未注入平台来源时只用运维名单（cfg 缺失也不能 panic）。
func TestMediaOriginsIncludesPlatformSource(t *testing.T) {
	p := &Plugin{}
	assert.Empty(t, p.mediaOrigins())

	WithMediaOriginSource(mediaOriginsStub{"http://127.0.0.1:3000", "media.lan:8080"})(p)
	assert.Equal(t, []string{"http://127.0.0.1:3000", "media.lan:8080"}, p.mediaOrigins())
}
