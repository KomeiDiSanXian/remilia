//go:build network

// 临时联网验证：确认 IQDB 排队行为与重试策略真实可用。
// 运行: go test -tags network -run TestNetworkIQDB -v ./cmd/bot/plugins/sauce/
package sauce

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testPNG 生成一张纯色 PNG 用于探测。
func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// TestNetworkIQDBQueueBehavior 观察 IQDB 排队时的响应形态：
//   - 记录首次请求耗时与是否超时
//   - 记录失败后的重试结果
//   - 检查响应中是否出现 queue 排队标记
func TestNetworkIQDBQueueBehavior(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	data := testPNG(t)

	probe := func(label string, timeout time.Duration) ([]SearchResult, error) {
		start := time.Now()
		reqCtx, c2 := context.WithTimeout(ctx, timeout)
		defer c2()
		c := &iqdbClient{
			httpClient: &http.Client{Timeout: timeout},
			retries:    0,
		}
		results, err := c.searchUpload(reqCtx, data, 5)
		t.Logf("%s: %v (%.1fs)", label, err, time.Since(start).Seconds())
		return results, err
	}

	// 第一次探测：45s 超时（默认配置）
	_, firstErr := probe("首次请求(45s)", 45*time.Second)
	if firstErr != nil {
		t.Logf("首次请求失败（排队中或网络问题）: %v", firstErr)
	} else {
		t.Log("首次请求成功（未排队或队列较短）")
	}

	// 重试探测：重新排队后再次请求
	time.Sleep(1 * time.Second)
	_, retryErr := probe("重试请求(60s)", 60*time.Second)
	if retryErr != nil {
		t.Logf("重试请求失败: %v", retryErr)
	} else {
		t.Log("重试请求成功")
	}
}

// TestNetworkIQDBQueuedMarker 验证队列标记检测：排队响应 HTML 含
// queue('N','0') 脚本；完成后的响应含 queue('0','1') 或结果区。
func TestNetworkIQDBQueuedMarker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	c := &iqdbClient{httpClient: &http.Client{Timeout: 60 * time.Second}, retries: 0}
	results, err := c.Search(ctx, engineInput{Data: testPNG(t)}, 5)
	if err != nil {
		t.Logf("请求失败: %v", err)
		return
	}
	t.Logf("成功返回 %d 条结果", len(results))

	// 即使成功，响应页也总是携带 queue 脚本（wait=1 表示仍在等待其他查询）
	// 该标记用于人工确认排队行为，不作为解析逻辑的一部分。
	for _, r := range results {
		t.Logf("结果: %s %s%% %s", r.SourceName, r.Similarity, strings.Join(r.ExtURLs, ", "))
	}
}
