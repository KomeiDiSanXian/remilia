package context

import (
	stdctx "context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiProviderSender 实现 platform.Sender + platform.APIProvider（测试用）。
type apiProviderSender struct {
	api any
}

func (s *apiProviderSender) Send(_ stdctx.Context, _ platform.SendRequest) (platform.SendResult, error) {
	return platform.SendResult{}, nil
}
func (s *apiProviderSender) PlatformAPI() any { return s.api }

// TestGetPlatformAPIAs_Generic 验证 Context 泛型方法。
func TestGetPlatformAPIAs_Generic(t *testing.T) {
	type fakeAPI struct{ name string }
	fake := &fakeAPI{name: "fake"}
	ctx := NewContextFromEvent(nil, &apiProviderSender{api: fake})

	got, ok := ctx.GetPlatformAPIAs[*fakeAPI]()
	require.True(t, ok)
	assert.Same(t, fake, got)

	// 类型不匹配。
	_, ok = ctx.GetPlatformAPIAs[platform.Sender]()
	assert.False(t, ok)

	// nil Context。
	var nilCtx *Context
	_, ok = nilCtx.GetPlatformAPIAs[*fakeAPI]()
	assert.False(t, ok)
}
