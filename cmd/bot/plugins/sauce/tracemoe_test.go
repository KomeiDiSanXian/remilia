package sauce

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// traceMoeJSON 模拟 trace.moe API 真实响应（截取结构）。
const traceMoeJSON = `{"quota":100,"quotaUsed":0,"frameCount":1696168352,"error":"","result":[
{"anilist":{"id":1565,"type":"ANIME","title":{"native":"ポケットモンスター","romaji":"Pocket Monsters Diamond & Pearl","chinese":"宝可梦","english":"Pokemon"},"format":"TV","siteUrl":"https://anilist.co/anime/1565"},"filename":"[Subs] Pocket_Monsters_-_01.mkv","episode":1,"from":192.0,"to":195.0,"similarity":0.932,"video":"https://media.trace.moe/video/1.mp4","image":"https://media.trace.moe/image/1.jpg"},
{"anilist":{"id":1565,"title":{"native":"ポケットモンスター","romaji":"Pocket Monsters","chinese":"","english":"Pokemon"},"siteUrl":"https://anilist.co/anime/1565"},"filename":"ep02.mkv","episode":2,"from":0.5,"to":3.2,"similarity":0.42,"video":"","image":""}
]}`

func TestUnmarshalTraceMoeAndConvert(t *testing.T) {
	var data traceMoeResponse
	require.NoError(t, json.Unmarshal([]byte(traceMoeJSON), &data))
	assert.Empty(t, data.Error)
	require.Len(t, data.Result, 2)

	r := toTraceMoeResult(data.Result[0])
	assert.Equal(t, "TraceMoe", r.Source)
	assert.Equal(t, "93.20", r.Similarity)
	assert.Equal(t, "宝可梦", r.Title)
	assert.Equal(t, "1", r.Episode)
	assert.Equal(t, "03:12", r.Timestamp)
	assert.Equal(t, "https://media.trace.moe/image/1.jpg", r.Thumbnail)
	assert.Equal(t, "https://media.trace.moe/video/1.mp4", r.VideoURL)
	assert.Equal(t, []string{"https://anilist.co/anime/1565"}, r.ExtURLs)
	assert.Equal(t, "Trace.moe", r.SourceName)
}

func TestTraceMoeMinSimilarityFilter(t *testing.T) {
	var data traceMoeResponse
	require.NoError(t, json.Unmarshal([]byte(traceMoeJSON), &data))

	var kept []SearchResult
	for _, item := range data.Result {
		if item.Similarity < 0.75 {
			continue
		}
		kept = append(kept, toTraceMoeResult(item))
	}
	assert.Len(t, kept, 1)
	assert.Equal(t, "93.20", kept[0].Similarity)
}

func TestTraceMoeEpisode(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`1`, "1"},
		{`"SP"`, "SP"},
		{`[1,2]`, "1"},
		{`null`, ""},
		{``, ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, traceMoeEpisode(json.RawMessage(c.raw)), "raw=%s", c.raw)
	}
}

func TestFormatTraceMoeTime(t *testing.T) {
	assert.Equal(t, "03:12", formatTraceMoeTime(192))
	assert.Equal(t, "00:00", formatTraceMoeTime(0))
	assert.Equal(t, "01:02:03", formatTraceMoeTime(3723))
	assert.Equal(t, "00:00", formatTraceMoeTime(-5))
}
