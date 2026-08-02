package pic

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Gelbooru 系协议解析 ─────────────────────────────────────────────────

// TestParseGelbooruObjectResponse 覆盖 gelbooru.com 的对象包装格式。
// 响应样本取自真实 API 响应（2026-08）。
func TestParseGelbooruObjectResponse(t *testing.T) {
	body := `{"attributes":{"limit":2},"post":[
		{"id":972129,"score":12,"rating":"safe","tags":"2girls bikini blue_eyes","file_url":"https://img4.gelbooru.com/images/1b/04/1b04f5bcafb1d5a7e9fedf89c971f526.jpeg","source":"","owner":"spotless","creator_id":20788},
		{"id":972130,"score":7,"rating":"questionable","tags":"cat","file_url":"https://img4.gelbooru.com/images/1b/04/xx.jpeg","source":"https://twitter.com/a","owner":"","creator_id":0}
	]}`
	s, _ := findSite("gelbooru")
	posts, err := parseGelbooruPosts([]byte(body))
	require.NoError(t, err)
	require.Len(t, posts, 2)

	post := gelbooruToPost(posts[0], s)
	assert.Equal(t, 972129, post.ID)
	assert.Equal(t, RatingSafe, post.Rating)
	assert.Equal(t, "https://img4.gelbooru.com/images/1b/04/1b04f5bcafb1d5a7e9fedf89c971f526.jpeg", post.FileURL)
	assert.Equal(t, "spotless", post.Author)
	assert.Equal(t, 12, post.Score)
	assert.Equal(t, "Gelbooru", post.SiteName)
	assert.Equal(t, []string{"2girls", "bikini", "blue_eyes"}, post.Tags)

	// owner 为空时从 artist: 标签提取兜底
	post2 := gelbooruToPost(posts[1], s)
	assert.Equal(t, "", post2.Author)
}

// TestParseRule34FlatArray 覆盖 api.rule34.xxx 的扁平数组格式。
// 响应样本取自真实 API 响应（2026-08）：与 safebooru 同为数组，
// score 为 JSON 数字，owner 字段存在。
func TestParseRule34FlatArray(t *testing.T) {
	body := `[{"id":18296074,"rating":"explicit","tags":"1girl ai_generated bikini","file_url":"https://api-cdn.rule34.xxx/images/3239/7cddc3fe5ca34dde6fe7227b50155270.jpeg","source":"","owner":"aztecsas","score":0,"comment_count":0}]`
	s, _ := findSite("rule34")
	posts, err := parseGelbooruPosts([]byte(body))
	require.NoError(t, err)
	require.Len(t, posts, 1)

	post := gelbooruToPost(posts[0], s)
	assert.Equal(t, 18296074, post.ID)
	assert.Equal(t, RatingExplicit, post.Rating)
	assert.Equal(t, "https://api-cdn.rule34.xxx/images/3239/7cddc3fe5ca34dde6fe7227b50155270.jpeg", post.FileURL)
	assert.Equal(t, "aztecsas", post.Author)
	assert.Equal(t, 0, post.Score)
	assert.Equal(t, "rule34.xxx", post.SiteName)
}

// TestParseGelbooruFlatArray 覆盖 safebooru.org 的扁平数组格式。
func TestParseGelbooruFlatArray(t *testing.T) {
	body := `[{"id":7011159,"rating":"safe","file_url":"https://safebooru.org/images/841/xxx.jpeg","source":"","owner":"konbanwa","score":0,"tags":"1girl arms_up"},{"id":7011160,"rating":"safe","file_url":"https://safebooru.org/images/841/yyy.jpeg","source":"https://twitter.com/a","owner":"","score":5,"tags":"cat"}]`
	s, _ := findSite("safebooru")
	posts, err := parseGelbooruPosts([]byte(body))
	require.NoError(t, err)
	require.Len(t, posts, 2)

	post := gelbooruToPost(posts[0], s)
	assert.Equal(t, 7011159, post.ID)
	assert.Equal(t, "konbanwa", post.Author)
	assert.Equal(t, "Safebooru", post.SiteName)

	// owner 为空 + 无 artist: 标签 → Author 为空
	assert.Equal(t, "", gelbooruToPost(posts[1], s).Author)
}

// ── Moebooru 协议解析 ───────────────────────────────────────────────────

// TestParseMoebooruResponse 覆盖 konachan.net / yande.re 的 post.json 数组格式。
func TestParseMoebooruResponse(t *testing.T) {
	body := `[
		{"id":406789,"rating":"s","tags":"aliasing blush brown_hair","file_url":"https://konachan.net/image/eb/68/eb681f831ed4ad947690933ce46b3e69.jpg","source":"https://www.pixiv.net/en/artworks/147869115","author":"BattlequeenYume","score":3},
		{"id":406788,"rating":"q","tags":"test","file_url":"https://konachan.net/image/xx.jpg","source":"","author":"","score":8}
	]`
	var raws []moebooruRawPost
	require.NoError(t, json.Unmarshal([]byte(body), &raws))
	require.Len(t, raws, 2)

	s, _ := findSite("konachan")
	posts := make([]picPost, 0, len(raws))
	for _, raw := range raws {
		posts = append(posts, picPost{
			ID:       raw.ID,
			Rating:   parseMoebooruRating(raw.Rating),
			Tags:     strings.Fields(raw.Tags),
			FileURL:  raw.FileURL,
			Source:   raw.Source,
			Author:   raw.Author,
			Score:    raw.Score,
			SiteName: s.DisplayName,
		})
	}

	assert.Equal(t, 406789, posts[0].ID)
	assert.Equal(t, RatingSafe, posts[0].Rating) // 单字母 s → safe
	assert.Equal(t, "BattlequeenYume", posts[0].Author)
	assert.Equal(t, "Konachan", posts[0].SiteName)
	assert.Equal(t, RatingQuestionable, posts[1].Rating)
}

// TestPickRandomPosts 验证本地随机选取逻辑。
func TestPickRandomPosts(t *testing.T) {
	posts := make([]picPost, 10)
	for i := range posts {
		posts[i] = picPost{ID: i}
	}

	// count 小于池大小：随机选 count 张且不重复
	picked := pickRandomPosts(posts, 3)
	require.Len(t, picked, 3)
	seen := map[int]bool{}
	for _, p := range picked {
		assert.False(t, seen[p.ID], "不应重复")
		seen[p.ID] = true
	}

	// 多次选取应产生不同组合（池 10 选 3 有 120 种组合，几乎必变）
	first := pickRandomPosts(posts, 3)
	different := false
	for i := 0; i < 10; i++ {
		next := pickRandomPosts(posts, 3)
		if next[0].ID != first[0].ID || next[1].ID != first[1].ID {
			different = true
			break
		}
	}
	assert.True(t, different, "本地随机应产生不同结果")

	// count 大于等于池大小：返回全部
	assert.Len(t, pickRandomPosts(posts, 10), 10)
	assert.Len(t, pickRandomPosts(posts, 99), 10)

	// 空池
	assert.Nil(t, pickRandomPosts(nil, 3))
	assert.Nil(t, pickRandomPosts(posts, 0))
}

// TestRandomTag 验证各协议的服务端随机排序 meta-tag（依据官方文档实测）。
func TestRandomTag(t *testing.T) {
	assert.Equal(t, "sort:random", randomTag(protocolGelbooru))
	assert.Equal(t, "order:random", randomTag(protocolMoebooru))
}

func TestSearchTags(t *testing.T) {
	gelbooru, _ := findSite("gelbooru")
	konachan, _ := findSite("konachan")
	yandere, _ := findSite("yandere")
	safebooru, _ := findSite("safebooru")

	// Gelbooru 系：用户标签 + rating + sort:random
	assert.Equal(t, "cat rating:general sort:random",
		searchTags(gelbooru, []string{"cat"}, rng(RatingSafe)))
	assert.Equal(t, "cat rating:explicit sort:random",
		searchTags(gelbooru, []string{"cat"}, rng(RatingExplicit)))
	// gelbooru 区间 [safe..questionable]：排除 explicit
	assert.Equal(t, "cat -rating:explicit sort:random",
		searchTags(gelbooru, []string{"cat"}, rngRange(RatingSafe, RatingQuestionable)))
	// Moebooru：order:random；konachan.net 为 SFW 镜像整站 safe，不附加过滤
	assert.Equal(t, "cat order:random",
		searchTags(konachan, []string{"cat"}, rng(RatingSafe)))
	// yande.re 区间 [safe..questionable]：排除 explicit
	assert.Equal(t, "cat -rating:explicit order:random",
		searchTags(yandere, []string{"cat"}, rngRange(RatingSafe, RatingQuestionable)))
	// 全区间（all）不附加 rating 标签
	assert.Equal(t, "cat sort:random",
		searchTags(gelbooru, []string{"cat"}, rngRange(RatingSafe, RatingExplicit)))
	// 无用户标签时仍包含 rating 与随机 meta-tag
	assert.Equal(t, "rating:general sort:random", searchTags(gelbooru, nil, rng(RatingSafe)))
	// 全区间时只剩随机 meta-tag
	assert.Equal(t, "sort:random", searchTags(gelbooru, nil, rngRange(RatingSafe, RatingExplicit)))
	// safebooru 整站仅 safe 内容，不附加 rating 过滤（新旧评级并存）
	assert.Equal(t, "cat sort:random", searchTags(safebooru, []string{"cat"}, rng(RatingSafe)))
}

func TestRandomPoolSize(t *testing.T) {
	assert.Equal(t, 10, randomPoolSize(1))
	assert.Equal(t, 10, randomPoolSize(3))
	assert.Equal(t, 12, randomPoolSize(4))
}

// TestRedactTransportError 验证传输错误中的认证凭据被脱敏。
func TestRedactTransportError(t *testing.T) {
	raw := `Get "https://gelbooru.com/index.php?page=dapi&s=post&q=index&json=1&limit=3&random=1&tags=rating%3Asafe&user_id=2027759&api_key=supersecretkey": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`
	_ = raw
	uerr := &url.Error{Op: "Get", URL: "https://gelbooru.com/index.php?page=dapi&s=post&q=index&json=1&limit=3&random=1&tags=rating%3Asafe&user_id=2027759&api_key=supersecretkey", Err: context.DeadlineExceeded}

	redacted := redactTransportError(uerr)
	assert.NotContains(t, redacted.Error(), "supersecretkey")
	assert.NotContains(t, redacted.Error(), "2027759")
	// <redacted> 经 URL 编码为 %3Credacted%3E
	assert.Contains(t, redacted.Error(), "redacted")

	// errors.As 判定能力保留
	var target *url.Error
	require.True(t, errors.As(redacted, &target))

	// 无凭据参数的 URL 原样返回（同一实例）
	plain := &url.Error{Op: "Get", URL: "https://safebooru.org/index.php?page=dapi&tags=cat", Err: context.DeadlineExceeded}
	assert.Same(t, plain, redactTransportError(plain))

	// 非 *url.Error 类型原样返回
	generic := errors.New("boom")
	assert.Same(t, generic, redactTransportError(generic))
}

func TestParseMoebooruRating(t *testing.T) {
	assert.Equal(t, RatingSafe, parseMoebooruRating("s"))
	assert.Equal(t, RatingQuestionable, parseMoebooruRating("q"))
	assert.Equal(t, RatingExplicit, parseMoebooruRating("e"))
	assert.Equal(t, RatingSafe, parseMoebooruRating("safe"))
	assert.Equal(t, RatingSafe, parseMoebooruRating("")) // 未知回退
}
