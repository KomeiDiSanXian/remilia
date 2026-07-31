package sauce

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ascii2dItemHTML 模拟 ascii2d 改版后真实结果页的单个条目结构。
const ascii2dItemHTML = `<div class="row item-box">
<div class="col-xs-12 col-sm-12 col-md-4 col-xl-4 text-xs-center image-box">
<img loading="eager" src="/thumbnail/19/9/5/1995bf7c8da0d9f3a100fc7abf3d1fbe.jpg" alt="hash">
</div>
<div class="col-xs-12 col-sm-12 col-md-8 col-xl-8 info-box">
<div class="hash">1995bf7c8da0d9f3a100fc7abf3d1fbe</div>
<small class="text-muted">931x1315 JPEG 473.7KB</small>
<div class="pull-xs-right"></div>
<div class="detail-box gray-link">
<h6>
<img class="to-link-icon" src="/assets/pixiv-abc.ico" alt="Pixiv" width="14" height="14">
<a target="_blank" rel="noopener" href="https://www.pixiv.net/artworks/139121562">C107さむわんへるつ本</a>
<a target="_blank" rel="noopener" href="https://www.pixiv.net/users/8934942">ひらり</a>
<small>pixiv</small>
</h6>
</div>
</div>
</div>`

func TestParseASCII2DResults(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(ascii2dItemHTML))
	require.NoError(t, err)

	var node *html.Node
	visitNodes(doc, func(n *html.Node) {
		if n.DataAtom == atom.Div && getAttr(n, "class") == "row item-box" {
			node = n
		}
	})
	require.NotNil(t, node)

	r := extractASCII2DItem(node)
	require.NotNil(t, r)
	assert.Equal(t, "ASCII2D", r.Source)
	assert.Equal(t, "https://ascii2d.net/thumbnail/19/9/5/1995bf7c8da0d9f3a100fc7abf3d1fbe.jpg", r.Thumbnail)
	assert.Equal(t, "C107さむわんへるつ本", r.Title)
	assert.Equal(t, "ひらり", r.Author)
	assert.Equal(t, "Pixiv", r.SourceName)
	require.Len(t, r.ExtURLs, 2)
	assert.Equal(t, "https://www.pixiv.net/artworks/139121562", r.ExtURLs[0])
	assert.Equal(t, "https://www.pixiv.net/users/8934942", r.ExtURLs[1])
}

func TestExtractDetailBoxTwitter(t *testing.T) {
	const twitterItem = `<div class="detail-box gray-link">
<h6>
<img class="to-link-icon" src="/assets/twitter-abc.ico" alt="Twitter" width="14" height="14">
<a target="_blank" rel="noopener" href="https://twitter.com/wanwan_majin/status/2078787634122629628">2026.07.19</a>
<a target="_blank" rel="noopener" href="https://twitter.com/i/user/703046273604067329">wanwan_majin</a>
<small>twitter</small>
</h6>
</div>`

	doc, _ := html.Parse(strings.NewReader(twitterItem))
	var node *html.Node
	visitNodes(doc, func(n *html.Node) {
		if n.DataAtom == atom.Div && strings.Contains(getAttr(n, "class"), "detail-box") {
			node = n
		}
	})
	require.NotNil(t, node)

	r := &SearchResult{Source: "ASCII2D"}
	extractDetailBox(node, r)
	assert.Equal(t, "Twitter", r.SourceName)
	assert.Equal(t, "@wanwan_majin", r.Author)
	require.Len(t, r.ExtURLs, 2)
}

func TestExtractAscii2dHash(t *testing.T) {
	assert.Equal(t, "abc123", extractAscii2dHash("https://ascii2d.net/search/color/abc123"))
	assert.Equal(t, "abc123", extractAscii2dHash("https://ascii2d.net/search/bovw/abc123"))
	assert.Equal(t, "", extractAscii2dHash("https://ascii2d.net/"))
	assert.Equal(t, "", extractAscii2dHash("not a url"))
}

func TestMergeASCII2DModes(t *testing.T) {
	color := []SearchResult{
		{Source: "ASCII2D", ExtURLs: []string{"https://www.pixiv.net/artworks/1"}, Similarity: "90.00"},
	}
	feature := []SearchResult{
		{Source: "ASCII2D", ExtURLs: []string{"https://www.pixiv.net/artworks/1"}, Similarity: "92.00"}, // 重复
		{Source: "ASCII2D", ExtURLs: []string{"https://twitter.com/x/status/1"}, Similarity: "80.00"},   // 新增
	}
	merged := mergeASCII2DModes(color, feature, 10)
	assert.Len(t, merged, 2)
}

func TestNormalizeResultURL(t *testing.T) {
	key := func(u string) string { return normalizeResultURL(u) }
	assert.Equal(t, "pixiv.net/artworks/123", key("https://www.pixiv.net/artworks/123"))
	assert.Equal(t, "pixiv.net/artworks/123", key("//pixiv.net/artworks/123"))
	assert.Equal(t, "pixiv.net/artworks/123", key("https://pixiv.net/artworks/123?lang=zh"))
}
