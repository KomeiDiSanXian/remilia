package pic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSuggestTestServer 构造 mock Moebooru /tag.json 服务器（https + 可信客户端）。
func newSuggestTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	client := srv.Client()
	client.Timeout = 5 * time.Second
	return srv, client
}

// ── 前缀与排序 ───────────────────────────────────────────────────────────

func TestSuggestPrefixes(t *testing.T) {
	assert.Equal(t, []string{"catx", "cat"}, suggestPrefixes("catx"))
	assert.Equal(t, []string{"abc"}, suggestPrefixes("abc"), "≤3 字符只有完整前缀")
	assert.Equal(t, []string{"東方project", "東方p"}, suggestPrefixes("東方project"), "按 rune 截断")
}

func TestRankSuggestions(t *testing.T) {
	pool := map[string]int{
		"cakes":    20, // dist 1
		"cart":     4,  // dist 2
		"card":     3,  // dist 2
		"cupcakes": 2,  // dist 4，超距剔除
		"cake":     9,  // 标签本身，剔除
	}
	suggs := rankSuggestions("cake", pool)
	require.Len(t, suggs, 3)
	assert.Equal(t, "cakes", suggs[0].Name, "距离最短排最前")
	assert.Equal(t, "cart", suggs[1].Name, "同距离按热度降序")
	assert.Equal(t, "card", suggs[2].Name)
}

func TestSuggestibleTag(t *testing.T) {
	assert.Equal(t, "catx", suggestibleTag(" CatX "))
	assert.Equal(t, "touhou", suggestibleTag("TOUHOU"))
	assert.Empty(t, suggestibleTag("rating:safe"))
	assert.Empty(t, suggestibleTag("-cat"))
	assert.Empty(t, suggestibleTag("a*"))
	assert.Empty(t, suggestibleTag(""))
}

func TestSuggestProbesOrder(t *testing.T) {
	probes := suggestProbes("")
	require.Len(t, probes, 2, "内置仅 Moebooru 两站支持探测")
	assert.Equal(t, "konachan", probes[0].Name)
	assert.Equal(t, "yandere", probes[1].Name)

	// 指定站点优先且不重复
	probes = suggestProbes("YANDERE")
	require.Len(t, probes, 2)
	assert.Equal(t, "yandere", probes[0].Name)
	assert.Equal(t, "konachan", probes[1].Name)

	// 指定不支持探测的站点 → 回退全部内置探测站
	probes = suggestProbes("gelbooru")
	require.Len(t, probes, 2)
	assert.Equal(t, "konachan", probes[0].Name)
}

// ── suggestTag 探测 ─────────────────────────────────────────────────────

// TestSuggestTagUnknown 验证未知标签：完整前缀 + 截断前缀两路探测，
// 相似候选按编辑距离排序（catx → cat* → cats）。
func TestSuggestTagUnknown(t *testing.T) {
	var calls atomic.Int32
	seen := make(map[string]int)
	srv, client := newSuggestTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Query().Get("name") {
		case "catx*":
			_, _ = w.Write([]byte(`[]`))
		case "cat*":
			seen["cat*"]++
			_, _ = w.Write([]byte(`[{"name":"cathedral","count":50},{"name":"cats","count":100}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	})
	defer srv.Close()

	c := &booruClient{httpClient: client}
	probes := []site{{Name: "konachan", Domain: srv.Listener.Addr().String(), Protocol: protocolMoebooru}}

	ok, known, suggs := c.suggestTag(context.Background(), probes, "catx")
	require.True(t, ok)
	assert.False(t, known)
	require.NotEmpty(t, suggs)
	assert.Equal(t, "cats", suggs[0].Name, "dist 1 应排最前（cathedral dist 6 被剔除）")
	assert.Equal(t, 2, int(calls.Load()), "完整前缀 + 截断前缀两路探测")
	assert.Equal(t, 1, seen["cat*"], "两路探测不会重复请求同一前缀")
}

// TestSuggestTagKnownEarlyStop 验证标签已收录时提前结束、不再探测后续站点。
func TestSuggestTagKnownEarlyStop(t *testing.T) {
	var site2Calls atomic.Int32
	srv1, client1 := newSuggestTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("name") {
		case "touhou*":
			_, _ = w.Write([]byte(`[{"name":"touhou","count":12345}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	})
	defer srv1.Close()
	srv2, _ := newSuggestTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		site2Calls.Add(1)
		_, _ = w.Write([]byte(`[]`))
	})
	defer srv2.Close()

	c := &booruClient{httpClient: client1}
	probes := []site{
		{Name: "konachan", Domain: srv1.Listener.Addr().String(), Protocol: protocolMoebooru},
		{Name: "yandere", Domain: srv2.Listener.Addr().String(), Protocol: protocolMoebooru},
	}

	ok, known, suggs := c.suggestTag(context.Background(), probes, "touhou")
	require.True(t, ok)
	assert.True(t, known, "候选中含完整标签应判定为已收录")
	assert.Empty(t, suggs, "已收录时不推荐自身")
	assert.Equal(t, int32(0), site2Calls.Load(), "已收录应提前结束，不探测下一站")
}

// TestSuggestTagFallsBackToNextSite 验证首站探测失败时换下一站。
func TestSuggestTagFallsBackToNextSite(t *testing.T) {
	srv1, _ := newSuggestTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	defer srv1.Close()
	srv2, _ := newSuggestTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"touhou","count":12345}]`))
	})
	defer srv2.Close()

	// 用与 srv2 配套的客户端（请求 srv1 会因证书不可信而失败，等价站点故障）
	c := &booruClient{httpClient: srv2.Client()}
	probes := []site{
		{Name: "konachan", Domain: srv1.Listener.Addr().String(), Protocol: protocolMoebooru},
		{Name: "yandere", Domain: srv2.Listener.Addr().String(), Protocol: protocolMoebooru},
	}

	ok, known, _ := c.suggestTag(context.Background(), probes, "touhou")
	require.True(t, ok, "首站失败应降级到下一站而非整体失败")
	assert.True(t, known)
}

// TestFetchMoebooruTagsEmptyBody 验证探测端点空响应不报错（返回 0 候选）。
func TestFetchMoebooruTagsEmptyBody(t *testing.T) {
	srv, client := newSuggestTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// 不写 body
	})
	defer srv.Close()

	c := &booruClient{httpClient: client}
	s := site{Name: "konachan", Domain: srv.Listener.Addr().String(), Protocol: protocolMoebooru}
	rows, err := c.fetchMoebooruTags(context.Background(), s, "cat")
	require.NoError(t, err)
	assert.Empty(t, rows)
}

// ── noMatchMessage 文案 ──────────────────────────────────────────────────

func TestNoMatchMessageUnknownTag(t *testing.T) {
	srv, client := newSuggestTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("name") {
		case "catx*", "cat*":
			_, _ = w.Write([]byte(`[{"name":"cats","count":100},{"name":"cart","count":4}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	})
	defer srv.Close()

	p := &Plugin{client: &booruClient{httpClient: client}}
	probes := []site{{Name: "konachan", Domain: srv.Listener.Addr().String(), Protocol: protocolMoebooru}}

	msg := p.noMatchMessage(context.Background(), []string{"CatX"}, probes)
	assert.Contains(t, msg, "没有找到匹配的图片")
	assert.Contains(t, msg, "「catx」未收录")
	assert.Contains(t, msg, "cats")
	assert.Contains(t, msg, "/pic cats", "单标签时给出可直接复制的替换命令")
}

func TestNoMatchMessageKnownAndUnknownTags(t *testing.T) {
	srv, client := newSuggestTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"touhou","count":12345}]`))
	})
	defer srv.Close()

	p := &Plugin{client: &booruClient{httpClient: client}}
	probes := []site{{Name: "konachan", Domain: srv.Listener.Addr().String(), Protocol: protocolMoebooru}}

	msg := p.noMatchMessage(context.Background(), []string{"touhou", "nonexistent_tag_xyz"}, probes)
	// 已收录的标签不报错；未收录且无相似候选时提示检查拼写
	assert.Contains(t, msg, "「nonexistent_tag_xyz」未收录，请检查拼写")
	assert.NotContains(t, msg, "touhou")
}

func TestNoMatchMessageProbeFailure(t *testing.T) {
	srv, client := newSuggestTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	defer srv.Close()

	p := &Plugin{client: &booruClient{httpClient: client}}
	probes := []site{{Name: "konachan", Domain: srv.Listener.Addr().String(), Protocol: protocolMoebooru}}

	msg := p.noMatchMessage(context.Background(), []string{"catx"}, probes)
	assert.Contains(t, msg, "没有找到匹配的图片")
	assert.NotContains(t, msg, "未收录", "探测全失败时不可断言标签有误")
}

func TestNoMatchMessageNoTagsOrProbes(t *testing.T) {
	p := &Plugin{client: &booruClient{httpClient: http.DefaultClient}}
	// 无标签（/pic 纯随机）→ 通用提示
	msg := p.noMatchMessage(context.Background(), nil, nil)
	assert.True(t, strings.HasPrefix(msg, "没有找到匹配的图片"), msg)
	// 有标签但无探测站点 → 通用提示
	msg = p.noMatchMessage(context.Background(), []string{"catx"}, nil)
	assert.True(t, strings.HasPrefix(msg, "没有找到匹配的图片"), msg)
}

func TestNoMatchMessageMultiTagsNoHint(t *testing.T) {
	srv, client := newSuggestTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"cats","count":20}]`))
	})
	defer srv.Close()

	p := &Plugin{client: &booruClient{httpClient: client}}
	probes := []site{{Name: "konachan", Domain: srv.Listener.Addr().String(), Protocol: protocolMoebooru}}

	// 多标签命中未知时不给单标签替换命令（组合不明确）
	msg := p.noMatchMessage(context.Background(), []string{"catx", "dog"}, probes)
	assert.Contains(t, msg, "「catx」未收录")
	assert.NotContains(t, msg, "试试：/pic")
}
