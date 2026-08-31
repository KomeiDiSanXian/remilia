package sauce

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/netguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewSauceDownloadClient_ProxyUsesGuardedTransport 回归测试：配置代理时
// DialContext 拨的是代理地址（如 127.0.0.1:7890），下载客户端必须经由
// netguard.GuardTransport 安装代理感知守卫，而不是裸的 netguard.DialContext，
// 否则报 "connection to non-public address blocked"。
func TestNewSauceDownloadClient_ProxyUsesGuardedTransport(t *testing.T) {
	old := sauceTransport
	t.Cleanup(func() { sauceTransport = old })
	require.NoError(t, initSauceTransport("http://127.0.0.1:7890"))

	client := newSauceDownloadClient(time.Second)
	tr, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.NotEqual(t, reflect.ValueOf(netguard.DialContext).Pointer(),
		reflect.ValueOf(tr.DialContext).Pointer(),
		"proxied download transport must not install bare netguard DialContext")
}

// TestNewSauceDownloadClient_DirectKeepsDialGuard 直连场景必须保留
// 公网拨号守卫（DNS 重绑定防线）。
func TestNewSauceDownloadClient_DirectKeepsDialGuard(t *testing.T) {
	old := sauceTransport
	t.Cleanup(func() { sauceTransport = old })
	require.NoError(t, initSauceTransport(""))
	sauceTransport.Proxy = nil // 显式置空，避免测试进程环境代理干扰判定

	client := newSauceDownloadClient(time.Second)
	tr, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	_, err := tr.DialContext(context.Background(), "tcp", "127.0.0.1:9")
	require.ErrorContains(t, err, "non-public address blocked",
		"direct download transport must keep the public-address dial guard")
}
