package qq

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseForwardAttachment 覆盖 [附件N] 行字段解析：大小换算、
// 含空格文件名（按字段键切片而非空白分词）、无效行丢弃。
func TestParseForwardAttachment(t *testing.T) {
	att, ok := parseForwardAttachment("类型:图片 文件名:AA.png 尺寸:600x503 大小:30.8KB URL:https://multimedia.nt.qq.com.cn/download?appid=1406&fileid=E&spec=0")
	require.True(t, ok)
	assert.Equal(t, platform.AttachmentKindImage, att.Kind)
	assert.Equal(t, "AA.png", att.Name)
	assert.Equal(t, 600, att.Width)
	assert.Equal(t, 503, att.Height)
	assert.Equal(t, 31539, att.Size) // 30.8KB ≈ 30.8*1024
	assert.Equal(t, "https://multimedia.nt.qq.com.cn/download?appid=1406&fileid=E&spec=0", att.URL)

	att, ok = parseForwardAttachment("类型:文件 文件名:bin.dat 大小:1024B")
	require.True(t, ok)
	assert.Equal(t, 1024, att.Size)

	att, ok = parseForwardAttachment("类型:文件 文件名:bin.dat 大小:2MB")
	require.True(t, ok)
	assert.Equal(t, 2*1024*1024, att.Size)

	att, ok = parseForwardAttachment("类型:语音 文件名:silk")
	require.True(t, ok)
	assert.Equal(t, platform.AttachmentKindAudio, att.Kind)
	assert.Equal(t, "silk", att.Name)

	// 文件名含空格：字段值延伸到下一个已知键
	att, ok = parseForwardAttachment("类型:文件 文件名:my report.pdf 大小:1KB")
	require.True(t, ok)
	assert.Equal(t, "my report.pdf", att.Name)
	assert.Equal(t, platform.AttachmentKindFile, att.Kind)
	assert.Equal(t, 1024, att.Size)

	// 无文件名且无 URL：丢弃
	_, ok = parseForwardAttachment("大小:abc")
	assert.False(t, ok)

	_, ok = parseForwardAttachment("")
	assert.False(t, ok)
}
