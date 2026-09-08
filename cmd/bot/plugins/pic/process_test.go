// Package pic process_test.go — 下载限流与体积控制的测试。
package pic

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPNG 生成指定尺寸的纯色 PNG 字节。
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 200, G: 120, B: 60, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// TestDownloadImageOversizeRejects 超过 maxBytes 的图片应报错而非返回截断数据：
// chunked 响应不带 Content-Length，仅靠预检会漏判，必须读后判定。
func TestDownloadImageOversizeRejects(t *testing.T) {
	payload := bytes.Repeat([]byte{0xAB}, 1024)

	// chunked：无 Content-Length，靠读后判定拦截
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	c := &booruClient{httpClient: srv.Client()}
	_, err := c.downloadImage(context.Background(), srv.URL, "", 512)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "图片过大")

	// Content-Length 预检路径
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1024")
		_, _ = w.Write(payload)
	}))
	defer srv2.Close()

	_, err = c.downloadImage(context.Background(), srv2.URL, "", 512)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "图片过大")
}

// TestDownloadImageExactLimitOK 恰好等于 maxBytes 的图片应成功下载。
func TestDownloadImageExactLimitOK(t *testing.T) {
	payload := bytes.Repeat([]byte{0xAB}, 512)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	c := &booruClient{httpClient: srv.Client()}
	data, err := c.downloadImage(context.Background(), srv.URL, "", 512)
	require.NoError(t, err)
	assert.Equal(t, payload, data)
}

// TestProcessPicSemaphoreLimitsConcurrency 大量并发 processPic 时，
// 实际同时在途的"下载+压缩"不得超过全局信号量容量。
func TestProcessPicSemaphoreLimitsConcurrency(t *testing.T) {
	var cur, max int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		c := atomic.AddInt32(&cur, 1)
		for {
			old := atomic.LoadInt32(&max)
			if c <= old || atomic.CompareAndSwapInt32(&max, old, c) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond) // 拉长在途窗口，放大并发竞争
		atomic.AddInt32(&cur, -1)
		_, _ = w.Write(testPNG(t, 32, 32))
	}))
	defer srv.Close()

	c := &booruClient{httpClient: srv.Client()}
	p := &Plugin{client: c, cfg: &fakePicConfig{vals: map[string]any{
		"download_concurrency": 2,
	}}}
	assert.Equal(t, 2, p.downloadConcurrency(), "配置应生效")

	post := picPost{ID: 1, FileURL: srv.URL}
	const workers = 8
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			att := p.processPic(context.Background(), post, "")
			require.NotNil(t, att)
		}()
	}
	wg.Wait()

	assert.LessOrEqual(t, atomic.LoadInt32(&max), int32(2),
		"并发下载不得超过信号量容量")
}

// TestProcessPicDefaultConcurrency 未配置时默认并发 4。
func TestProcessPicDefaultConcurrency(t *testing.T) {
	p := &Plugin{}
	assert.Equal(t, 4, p.downloadConcurrency())

	p2 := &Plugin{cfg: &fakePicConfig{vals: map[string]any{"download_concurrency": 0}}}
	assert.Equal(t, 4, p2.downloadConcurrency(), "非法值回退默认")

	p3 := &Plugin{cfg: &fakePicConfig{vals: map[string]any{"download_concurrency": 99}}}
	assert.Equal(t, 16, p3.downloadConcurrency(), "上限钳制 16")
}

// TestProcessPicCompressesLargeDownload 大于发送阈值的图应压缩后返回，
// 而非整包原样带回（回归：大图曾经直接下载失败或原样发送）。
func TestProcessPicCompressesLargeDownload(t *testing.T) {
	// 512×512 噪声 PNG ≈ 300KB+，远大于 64KB 的发送阈值；
	// 噪声不可压缩，PNG 无损重编码必然超限，最终应转 JPEG
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	rng := rand.New(rand.NewSource(42))
	for y := range 512 {
		for x := range 512 {
			img.Set(x, y, color.RGBA{R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: 60, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	payload := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	c := &booruClient{httpClient: srv.Client()}
	p := &Plugin{client: c, cfg: &fakePicConfig{vals: map[string]any{
		"send_thumbnail_max_bytes": 128 * 1024,
	}}}

	att := p.processPic(context.Background(), picPost{ID: 7, FileURL: srv.URL}, "")
	require.NotNil(t, att)
	require.LessOrEqual(t, len(att.Data), 128*1024, "应压缩到发送阈值内")
	assert.Equal(t, "pic_7.jpg", att.Name, "压缩转 JPEG 后扩展名应更新")
}

// TestProcessPicCancelledWhileWaiting 信号量被占满时，context 取消应中止等待。
func TestProcessPicCancelledWhileWaiting(t *testing.T) {
	c := &booruClient{httpClient: http.DefaultClient}
	p := &Plugin{client: c, cfg: &fakePicConfig{vals: map[string]any{
		"download_concurrency": 1,
	}}}

	// 占满唯一的信号量槽位
	p.dlSemFor() <- struct{}{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	att := p.processPic(ctx, picPost{ID: 1, FileURL: "http://example.invalid/x.png"}, "")
	assert.Nil(t, att, "context 已取消时不应执行下载")

	<-p.dlSemFor() // 释放，供后续测试
}
