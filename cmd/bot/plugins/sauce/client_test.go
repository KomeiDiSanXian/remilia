package sauce

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDownloadImageTrustedMediaHost 回归：声明受信媒体来源（运维名单或平台
// 适配器自声明）后，回环/内网媒体主机（含明文 http）的图片可下载；
// 未声明时仍被拒绝。
func TestDownloadImageTrustedMediaHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	}))
	defer srv.Close()
	imageURL := srv.URL + "/a.png"

	trusted, err := newSauceDownloadClient(time.Second, "", []string{srv.URL})
	require.NoError(t, err)
	p := &Plugin{download: trusted}
	data, derr := p.downloadImage(context.Background(), imageURL, 1<<20)
	require.NoError(t, derr, "trusted media host must be downloadable")
	require.NotEmpty(t, data)

	strict, err := newSauceDownloadClient(time.Second, "", nil)
	require.NoError(t, err)
	_, derr = (&Plugin{download: strict}).downloadImage(context.Background(), imageURL, 1<<20)
	require.Error(t, derr, "unconfigured media host must stay blocked")
}

// TestDownloadImageRejectsBadProxy 代理地址写错时构造期就报错，避免运行期
// 全部请求失败。
func TestDownloadImageRejectsBadProxy(t *testing.T) {
	_, err := newSauceDownloadClient(time.Second, "://bad", nil)
	require.Error(t, err)
}
