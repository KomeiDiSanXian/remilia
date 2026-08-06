package platform

import (
	stdctx "context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiProviderStub 模拟平台 Sender 实现 APIProvider。
type apiProviderStub struct {
	api any
}

func (s *apiProviderStub) PlatformAPI() any { return s.api }
func (s *apiProviderStub) Send(stdctx.Context, SendRequest) (SendResult, error) {
	return SendResult{}, nil
}

// 编译期接口实现检查。
var _ Sender = (*apiProviderStub)(nil)
var _ APIProvider = (*apiProviderStub)(nil)

// plainSender 是不实现 APIProvider 的普通 Sender。
type plainSender struct{}

func (s *plainSender) Send(stdctx.Context, SendRequest) (SendResult, error) {
	return SendResult{}, nil
}

var _ Sender = (*plainSender)(nil)

func TestGetPlatformAPI_WithProvider(t *testing.T) {
	api := &struct{ name string }{"fake-api"}
	sender := &apiProviderStub{api: api}

	got := GetPlatformAPI(sender)
	require.NotNil(t, got)
	assert.Same(t, api, got)
}

func TestGetPlatformAPI_WithoutProvider(t *testing.T) {
	assert.Nil(t, GetPlatformAPI(&plainSender{}))
}

func TestGetPlatformAPI_NilSender(t *testing.T) {
	assert.Nil(t, GetPlatformAPI(nil))
}

func TestGetPlatformAPIAs_WithProvider(t *testing.T) {
	type fakeAPI struct{ name string }
	api := &fakeAPI{name: "fake"}
	sender := &apiProviderStub{api: api}

	got, ok := GetPlatformAPIAs[*fakeAPI](sender)
	require.True(t, ok)
	assert.Same(t, api, got)
}

func TestGetPlatformAPIAs_TypeMismatch(t *testing.T) {
	other := &struct{ x int }{1}
	sender := &apiProviderStub{api: other}

	got, ok := GetPlatformAPIAs[*apiIfaceImpl](sender)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestGetPlatformAPIAs_InterfaceTarget(t *testing.T) {
	// T 为接口类型：句柄动态类型实现该接口即可（如 openapi.OpenAPI）。
	type apiIface interface{ Ping() }
	impl := &apiIfaceImpl{}
	sender := &apiProviderStub{api: impl}

	got, ok := GetPlatformAPIAs[apiIface](sender)
	require.True(t, ok)
	assert.Same(t, impl, got)
}

// apiIfaceImpl 实现 TestGetPlatformAPIAs_InterfaceTarget 中声明的 apiIface。
type apiIfaceImpl struct{}

func (apiIfaceImpl) Ping() {}

func TestGetPlatformAPIAs_NoProvider(t *testing.T) {
	got, ok := GetPlatformAPIAs[*apiProviderStub](&plainSender{})
	assert.False(t, ok)
	assert.Nil(t, got)
}
