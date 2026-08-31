package netguard

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startLocalListener 在回环地址上开一个监听器并返回其地址（用作代理地址）。
func startLocalListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func TestGuardTransport_Nil(t *testing.T) {
	assert.Nil(t, GuardTransport(nil))
}

// TestGuardTransport_ExplicitProxyDialAllowed 显式代理配置：拨号到代理地址
// 必须放行（此前会被公网校验误伤，报 non-public address blocked）。
func TestGuardTransport_ExplicitProxyDialAllowed(t *testing.T) {
	proxyAddr := startLocalListener(t)
	proxyURL, err := url.Parse("http://" + proxyAddr)
	require.NoError(t, err)

	guarded := GuardTransport(&http.Transport{Proxy: http.ProxyURL(proxyURL)})
	conn, err := guarded.DialContext(context.Background(), "tcp", proxyAddr)
	require.NoError(t, err, "dial to configured proxy must be allowed")
	_ = conn.Close()
}

// TestGuardTransport_EnvProxyDialAllowed 环境变量代理：即使 tr.Proxy 为空，
// 也应识别 HTTP(S)_PROXY/ALL_PROXY 配置并放行对应代理地址的拨号。
func TestGuardTransport_EnvProxyDialAllowed(t *testing.T) {
	proxyAddr := startLocalListener(t)
	t.Setenv("HTTPS_PROXY", "http://"+proxyAddr)

	guarded := GuardTransport(&http.Transport{})
	conn, err := guarded.DialContext(context.Background(), "tcp", proxyAddr)
	require.NoError(t, err, "dial to environment proxy must be allowed")
	_ = conn.Close()
}

// TestGuardTransport_DirectTargetStillBlocked 直连场景行为不变：非公网目标仍被拦截。
func TestGuardTransport_DirectTargetStillBlocked(t *testing.T) {
	guarded := GuardTransport(&http.Transport{})
	_, err := guarded.DialContext(context.Background(), "tcp", "127.0.0.1:9")
	require.ErrorContains(t, err, "non-public address blocked")
}

// TestGuardTransport_ProxyAllowlistDoesNotOpenHole 代理白名单不得放行
// 白名单之外的非公网地址（防止把白名单变成 SSRF 后门）。
func TestGuardTransport_ProxyAllowlistDoesNotOpenHole(t *testing.T) {
	proxyURL, err := url.Parse("http://127.0.0.1:7890")
	require.NoError(t, err)

	guarded := GuardTransport(&http.Transport{Proxy: http.ProxyURL(proxyURL)})
	_, err = guarded.DialContext(context.Background(), "tcp", "127.0.0.1:9")
	require.ErrorContains(t, err, "non-public address blocked",
		"non-proxy loopback address must stay blocked")
}
