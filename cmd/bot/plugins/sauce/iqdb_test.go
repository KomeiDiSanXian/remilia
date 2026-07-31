package sauce

import (
	"testing"

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
