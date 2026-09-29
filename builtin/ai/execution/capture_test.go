package execution

import (
	"context"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCaptureSenderMerge 冻结命令通道的隔离 + 汇总语义：
//   - 每次命令调用使用独立捕获器，本次命令无输出时不会回填上一次命令的旧文本；
//   - Merge 汇总时文本仅在非空时覆盖（保留"最近一次非空回复"兜底），
//     附件按调用顺序累积。
func TestCaptureSenderMerge(t *testing.T) {
	turn := &CaptureSender{}

	first := &CaptureSender{}
	_, err := first.Send(context.Background(), platform.SendRequest{Message: platform.TextMessage("第一条")})
	require.NoError(t, err)
	turn.Merge(first)
	assert.Equal(t, "第一条", turn.CapturedText)

	// 本次命令无输出（独立捕获器为空）：不应覆盖回合级已捕获的文本。
	empty := &CaptureSender{}
	turn.Merge(empty)
	assert.Equal(t, "第一条", turn.CapturedText, "空捕获不应覆盖已有文本")

	second := &CaptureSender{}
	_, err = second.Send(context.Background(), platform.SendRequest{Message: platform.TextMessage("第二条")})
	require.NoError(t, err)
	turn.Merge(second)
	assert.Equal(t, "第二条", turn.CapturedText, "非空捕获应覆盖为最近一次回复")

	// 附件按调用顺序累积。
	withAtt := &CaptureSender{}
	_, err = withAtt.Send(context.Background(), platform.SendRequest{Message: platform.OutboundMessage{
		Text:        "带附件",
		Attachments: []platform.Attachment{{Kind: platform.AttachmentKindImage, URL: "https://example.com/a.png"}},
	}})
	require.NoError(t, err)
	turn.Merge(withAtt)
	require.Len(t, turn.CapturedAttachments, 1)
	assert.Equal(t, "带附件", turn.CapturedText)

	// Merge 必须拷贝附件切片：后续对来源的写入不得影响已并入的回合级捕获。
	_, err = withAtt.Send(context.Background(), platform.SendRequest{Message: platform.OutboundMessage{
		Text:        "再来一张",
		Attachments: []platform.Attachment{{Kind: platform.AttachmentKindImage, URL: "https://example.com/b.png"}},
	}})
	require.NoError(t, err)
	assert.Len(t, turn.CapturedAttachments, 1, "再次发送不应改变已并入的附件")
}

// TestCaptureSenderMergeNil 冻结 nil 安全语义（命令路径可能传入空捕获器）。
func TestCaptureSenderMergeNil(t *testing.T) {
	var nilSender *CaptureSender
	nilSender.Merge(&CaptureSender{}) // 不 panic 即可

	turn := &CaptureSender{}
	turn.Merge(nil)
	assert.Empty(t, turn.CapturedText)
}
