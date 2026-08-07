package sauce

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestAnimeTraceServer 构造返回固定 JSON 的 AnimeTrace mock 服务器。
func newTestAnimeTraceServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestAnimeTraceSearchSuccess(t *testing.T) {
	body := `{"code":0,"ai":false,"trace_id":"t1","data":[
		{"box":[0.1,0.2,0.3,0.4],"box_id":"b1","not_confident":false,
		 "character":[{"work":"ご注文はうさぎですか？","character":"保登心愛"},
		              {"work":"Clover Day's","character":"鷹倉杏鈴"}]},
		{"box":[0.5,0.6,0.7,0.8],"box_id":"b2","not_confident":true,
		 "character":[{"work":"恋×シンアイ彼女","character":"小鞠ゆい"}]}
	]}`
	srv := newTestAnimeTraceServer(t, body, http.StatusOK)
	defer srv.Close()

	c := newAnimeTraceClient(&http.Client{Timeout: 5 * time.Second})
	orig := animeTraceEndpoint
	animeTraceEndpoint = srv.URL + "/v1/search"
	defer func() { animeTraceEndpoint = orig }()

	results, err := c.Search(context.Background(), engineInput{Data: []byte("fake-image")}, 10, false)
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, "AnimeTrace", results[0].Source)
	assert.Equal(t, "ご注文はうさぎですか？", results[0].Title)
	assert.Equal(t, "保登心愛", results[0].Author)
}

func TestAnimeTraceSearchFilterLowConfidence(t *testing.T) {
	body := `{"code":0,"data":[
		{"not_confident":false,"character":[{"work":"A","character":"a"}]},
		{"not_confident":true,"character":[{"work":"B","character":"b"}]}
	]}`
	srv := newTestAnimeTraceServer(t, body, http.StatusOK)
	defer srv.Close()

	c := newAnimeTraceClient(&http.Client{Timeout: 5 * time.Second})
	orig := animeTraceEndpoint
	animeTraceEndpoint = srv.URL + "/v1/search"
	defer func() { animeTraceEndpoint = orig }()

	results, err := c.Search(context.Background(), engineInput{Data: []byte("fake-image")}, 10, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "A", results[0].Title)
}

func TestAnimeTraceSearchError(t *testing.T) {
	srv := newTestAnimeTraceServer(t, `{"code":17702,"message":""}`, http.StatusOK)
	defer srv.Close()

	c := newAnimeTraceClient(&http.Client{Timeout: 5 * time.Second})
	orig := animeTraceEndpoint
	animeTraceEndpoint = srv.URL + "/v1/search"
	defer func() { animeTraceEndpoint = orig }()

	_, err := c.Search(context.Background(), engineInput{Data: []byte("fake-image")}, 10, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "服务器繁忙")
}

func TestAnimeTraceSearchNoData(t *testing.T) {
	c := newAnimeTraceClient(nil)
	_, err := c.Search(context.Background(), engineInput{}, 10, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "图片字节")
}

func TestAnimeTraceErrorText(t *testing.T) {
	assert.Equal(t, "图片大小过大", animeTraceErrorText(17701, ""))
	assert.Equal(t, "服务器繁忙，请稍后重试", animeTraceErrorText(17702, ""))
	assert.Equal(t, "API 维护中", animeTraceErrorText(17704, ""))
	assert.Equal(t, "图片格式不支持", animeTraceErrorText(17705, ""))
	assert.Equal(t, "图片中人物数量超过限制", animeTraceErrorText(17708, ""))
	assert.Equal(t, "图片下载失败", animeTraceErrorText(17722, ""))
	assert.Equal(t, "已达到本次使用上限", animeTraceErrorText(17728, ""))
	assert.Equal(t, "服务利用人数过多，请稍后重试", animeTraceErrorText(17731, ""))
	assert.Equal(t, "状态码 42", animeTraceErrorText(42, ""))
	assert.Equal(t, "自定义消息", animeTraceErrorText(42, "自定义消息"))
}
