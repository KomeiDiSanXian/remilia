package sauce

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// iqdbResultsHTML 模拟 IQDB 真实结果页结构（多服务 + 无匹配）。
const iqdbResultsHTML = `<html><body>
<div id='pages' class='pages'>
<div><table><tr><th>Your image</th></tr><tr><td class='image'><img src='/thu/thu_da3b9e69.jpg' alt="[IMG]" width='150' height='107'></td></tr></table></div>
<div><table><tr><th>Best match</th></tr><tr><td class='image'><a href="//konachan.com/post/show/162973"><img src='/konachan/4/d/b/4db69f9f17b811561b32f1487540e12e.jpg' alt="Rating: s Score: 111 Tags: aya_(star) brown_hair grass night" title="Rating: s Score: 111 Tags: aya_(star) brown_hair grass night" width='150' height='107'></a></td></tr><tr><td><img alt="icon" src="/icon/konachan.ico" class="service-icon">Konachan</td></tr><tr><td>1000&times;715 [Safe]</td></tr><tr><td>97% similarity</td></tr></table></div>
<div><table><tr><th>Additional match</th></tr><tr><td class='image'><a href="http://www.zerochan.net/1544382"><img src='/zerochan/c/4/5/c45cd51dbdbe871fdb71771a56dcad65.jpg' alt="Rating: s Tags: Female, Long Hair" width='150' height='104'></a></td></tr><tr><td><img alt="icon" src="/icon/zerochan.ico" class="service-icon">Zerochan</td></tr><tr><td>1000&times;694 [Safe]</td></tr><tr><td>90% similarity</td></tr></table></div>
<div class="nomatch"><table><tr><th>No relevant matches</th></tr><tr><td>Could not find your image on any of the selected services.</td></tr></table></div>
</div>
</body></html>`

func TestParseIQDBResults(t *testing.T) {
	results := parseIQDBResults([]byte(iqdbResultsHTML))
	require.Len(t, results, 2)

	first := results[0]
	assert.Equal(t, "IQDB", first.Source)
	assert.Equal(t, "97.00", first.Similarity)
	assert.Equal(t, "Konachan", first.SourceName)
	assert.Equal(t, "https://konachan.com/post/show/162973", first.ExtURLs[0])
	assert.Equal(t, "https://iqdb.org/konachan/4/d/b/4db69f9f17b811561b32f1487540e12e.jpg", first.Thumbnail)
	assert.Equal(t, "aya_(star) brown_hair grass night", first.Title)

	second := results[1]
	assert.Equal(t, "Zerochan", second.SourceName)
	assert.Equal(t, "90.00", second.Similarity)
}

func TestParseIQDBResultsNoMatch(t *testing.T) {
	const noMatch = `<html><body>
<div id='pages' class='pages'>
<div><table><tr><th>Your image</th></tr><tr><td class='image'><img src='/thu/thu_9b113d48.jpg'></td></tr></table></div>
<div class="nomatch"><table><tr><th>No relevant matches</th></tr></table></div>
</div>
</body></html>`
	results := parseIQDBResults([]byte(noMatch))
	assert.Empty(t, results)
}

func TestParseIQDBResultsInvalidHTML(t *testing.T) {
	assert.Nil(t, parseIQDBResults([]byte("<not html")))
}

func TestCleanIQDBAlt(t *testing.T) {
	assert.Equal(t, "aya_(star) brown_hair", cleanIQDBAlt("Rating: s Score: 111 Tags: aya_(star) brown_hair"))
	assert.Equal(t, "plain text", cleanIQDBAlt("plain text"))
}

func TestSourceNameFromHost(t *testing.T) {
	assert.Equal(t, "Danbooru", sourceNameFromHost("https://danbooru.donmai.us/posts/1"))
	assert.Equal(t, "Konachan", sourceNameFromHost("https://konachan.com/post/show/1"))
	assert.Equal(t, "Yande.re", sourceNameFromHost("https://yande.re/post/show/1"))
	assert.Equal(t, "Gelbooru", sourceNameFromHost("https://gelbooru.com/index.php"))
	assert.Equal(t, "Sankaku", sourceNameFromHost("https://chan.sankakucomplex.com/post/show/1"))
}

func TestIQDBRetryOnTransientFailure(t *testing.T) {
	// 第一次请求 503（排队满载），重试后成功 → 应返回结果
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(iqdbResultsHTML))
	}))
	defer srv.Close()

	c := &iqdbClient{httpClient: &http.Client{Timeout: 5 * time.Second}, retries: 1}
	orig := iqdbEndpoint
	iqdbEndpoint = srv.URL + "/"
	defer func() { iqdbEndpoint = orig }()

	results, err := c.Search(context.Background(), engineInput{Data: []byte("img")}, 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, int32(2), calls.Load())
}

func TestIQDBNoRetryOnClientError(t *testing.T) {
	// 4xx 错误不重试（重试次数无效）
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := &iqdbClient{httpClient: &http.Client{Timeout: 5 * time.Second}, retries: 3}
	orig := iqdbEndpoint
	iqdbEndpoint = srv.URL + "/"
	defer func() { iqdbEndpoint = orig }()

	_, err := c.Search(context.Background(), engineInput{Data: []byte("img")}, 10)
	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load())
}

func TestIQDBRetryExhausted(t *testing.T) {
	// 持续 5xx 时按重试次数耗尽后返回最后一次错误
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := &iqdbClient{httpClient: &http.Client{Timeout: 5 * time.Second}, retries: 2}
	orig := iqdbEndpoint
	iqdbEndpoint = srv.URL + "/"
	defer func() { iqdbEndpoint = orig }()

	_, err := c.Search(context.Background(), engineInput{Data: []byte("img")}, 10)
	require.Error(t, err)
	assert.Equal(t, int32(3), calls.Load()) // 1 次初始 + 2 次重试
}

func TestIQDBRetryableErrorWrapping(t *testing.T) {
	// 传输错误（超时/连接失败）包装为 iqdbRetryableError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL + "/"
	srv.Close() // 立即关闭 → 连接失败

	c := &iqdbClient{httpClient: &http.Client{Timeout: 5 * time.Second}, retries: 0}
	orig := iqdbEndpoint
	iqdbEndpoint = url
	defer func() { iqdbEndpoint = orig }()

	_, err := c.Search(context.Background(), engineInput{Data: []byte("img")}, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "请求失败")
}

func TestIQDBQueuedPosition(t *testing.T) {
	// 高峰期排队页：连续回传多个进度标记，取最后一个
	body := `<script type='text/javascript'>queue('1335','0');</script>
<script type='text/javascript'>queue('1329','0');</script>
<script type='text/javascript'>queue('1323','0');</script>`
	pos, wait, found := iqdbQueuedPosition([]byte(body))
	assert.True(t, found)
	assert.Equal(t, 1323, pos)
	assert.Equal(t, 0, wait)

	// 正在处理：position=0, wait=1
	pos, wait, found = iqdbQueuedPosition([]byte(`queue('0','1');`))
	assert.True(t, found)
	assert.Equal(t, 0, pos)
	assert.Equal(t, 1, wait)

	// 无标记
	_, _, found = iqdbQueuedPosition([]byte("<html>no queue</html>"))
	assert.False(t, found)
}

func TestIQDBQueuedOrNil(t *testing.T) {
	// 仍在队列中 → 报排队错误
	err := iqdbQueuedOrNil([]byte(`queue('1313','0');`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "排队")

	// 正在处理（position=0）→ 视为正常无匹配
	assert.Nil(t, iqdbQueuedOrNil([]byte(`queue('0','1');`)))

	// 无标记 → nil
	assert.Nil(t, iqdbQueuedOrNil([]byte("<html></html>")))
}

func TestIQDBSearchQueuedResponse(t *testing.T) {
	// 长排队响应页（无结果区 + 排队标记）→ 流式读取时快速失败，明确报排队错误
	const queuedHTML = `<html><body>
<script type='text/javascript'>queue('1212','0');</script>
<div id='queue'>...</div>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(queuedHTML))
	}))
	defer srv.Close()

	c := &iqdbClient{httpClient: &http.Client{Timeout: 5 * time.Second}, retries: 0}
	orig := iqdbEndpoint
	iqdbEndpoint = srv.URL + "/"
	defer func() { iqdbEndpoint = orig }()

	start := time.Now()
	_, err := c.Search(context.Background(), engineInput{Data: []byte("img")}, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "排队")
	// 长排队应在窗口内快速失败，而不是等满超时
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestIQDBSearchQueuedRetries(t *testing.T) {
	// 长排队 → 按配置重试一次；重试拿到新队列位置
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`<html><body><script type='text/javascript'>queue('1300','0');</script></body></html>`))
			return
		}
		_, _ = w.Write([]byte(iqdbResultsHTML))
	}))
	defer srv.Close()

	c := &iqdbClient{httpClient: &http.Client{Timeout: 5 * time.Second}, retries: 1}
	orig := iqdbEndpoint
	iqdbEndpoint = srv.URL + "/"
	defer func() { iqdbEndpoint = orig }()

	results, err := c.Search(context.Background(), engineInput{Data: []byte("img")}, 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, int32(2), calls.Load())
}

func TestIQDBSearchShortQueueWaits(t *testing.T) {
	// 短排队（position ≤ 阈值）：不应快速失败，继续读取直到响应完成
	const shortQueueHTML = `<html><body>
<script type='text/javascript'>queue('5','0');</script>
<div id='pages' class='pages'>
<div><table><tr><th>Your image</th></tr></table></div>
<div><table><tr><th>Best match</th></tr><tr><td class='image'><a href="//konachan.com/post/show/1"><img src='/thu/thu_x.jpg' alt="Tags: aya brown_hair"></a></td></tr><tr><td>Konachan</td></tr><tr><td>95% similarity</td></tr></table></div>
</div>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(shortQueueHTML))
	}))
	defer srv.Close()

	c := &iqdbClient{httpClient: &http.Client{Timeout: 5 * time.Second}, retries: 0}
	orig := iqdbEndpoint
	iqdbEndpoint = srv.URL + "/"
	defer func() { iqdbEndpoint = orig }()

	results, err := c.Search(context.Background(), engineInput{Data: []byte("img")}, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "95.00", results[0].Similarity)
}

func TestIQDBSearchMidStreamDisconnect(t *testing.T) {
	// 排队中途断流（响应只写了一半连接即断开）：
	// 应包装为可重试错误，重试后拿到完整结果（等价浏览器刷新重新提交）
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// 第一次：写完短排队标记后立即断开连接（模拟中途断流）
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server does not support hijack")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("hijack failed: %v", err)
			}
			// 写一个声明 Content-Length 大于实际内容的响应（短排队标记 +
			// 截断的 HTML），客户端读取时遇到 unexpected EOF → 模拟中途断流
			partial := "<html><body><script type='text/javascript'>queue('5','0');</script>"
			_, _ = conn.Write([]byte(fmt.Sprintf(
				"HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: %d\r\n\r\n%s",
				len(partial)+4096, partial)))
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte(iqdbResultsHTML))
	}))
	defer srv.Close()

	c := &iqdbClient{httpClient: &http.Client{Timeout: 5 * time.Second}, retries: 1}
	orig := iqdbEndpoint
	iqdbEndpoint = srv.URL + "/"
	defer func() { iqdbEndpoint = orig }()

	results, err := c.Search(context.Background(), engineInput{Data: []byte("img")}, 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, int32(2), calls.Load())
}
