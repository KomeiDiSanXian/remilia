// forward.go — 合并转发记录的统一结构与文本渲染。
//
// SegmentForward 段 / reply 段的结构化转发载荷（*ForwardRecord）由各平台
// 适配器归一化产出（QQ 官方 API 的扁平渲染文本、milky/onebot 的转发节点等），
// 存储键见 SegmentExtraForwardNodes / SegmentExtraQuotedForward。下游（AI
// 提示词、消息记录、跨平台降级摘要）通过本文件的类型与 ForwardRecordText
// 消费，无需依赖具体平台包。
package platform

import (
	"fmt"
	"strings"
)

// ForwardNodeKind 合并转发条目的语义类型。
type ForwardNodeKind string

const (
	// ForwardKindQuote 引用条目：Related 为被引用消息的渲染（通常 1 条）。
	ForwardKindQuote ForwardNodeKind = "quote"
	// ForwardKindRecord 合并转发条目：Related 为嵌套转发记录的消息列表。
	ForwardKindRecord ForwardNodeKind = "forward_record"
)

// ForwardNode 合并转发记录中的单条子消息（或嵌套引用/嵌套记录条目）。
type ForwardNode struct {
	// Sender 子消息发送者（多数平台仅能提供昵称，ID 可能不可得）。
	Sender UserInfo
	// Segments 子消息内容段（text/image/audio/video/file，保序）。
	Segments []Segment
	// Kind 条目语义类型：""（普通消息）或 ForwardKindQuote / ForwardKindRecord。
	Kind ForwardNodeKind
	// Related 关联子条目：Kind=ForwardKindQuote 时为被引用消息的渲染
	// （通常 1 条），Kind=ForwardKindRecord 时为嵌套转发记录的消息列表。
	Related []ForwardNode
}

// ForwardRecord 一条合并转发记录。
type ForwardRecord struct {
	// Title 记录标题（如 "XX和YY的聊天记录"），可能为空。
	Title string
	// Nodes 按原始顺序排列的子消息。
	Nodes []ForwardNode
}

// Segment.Extra 中转发记录载荷的键（见 SegmentExtraKey 通用键说明）。
const (
	// SegmentExtraForwardNodes 直发合并转发消息的结构化记录（SegmentForward 段）。
	SegmentExtraForwardNodes = "forward_nodes"
	// SegmentExtraQuotedForward 被引用合并转发记录（reply 段，被引用消息
	// 本身是合并转发时，如 QQ 103 引用 102）。
	SegmentExtraQuotedForward = "quote_forward"
)

// ForwardRecordText 将合并转发记录渲染为可读文本（供 AI 提示词、消息记录
// 等文本型下游消费）。媒体段渲染为占位标记（[图片]/[语音]/[视频]/[文件]），
// 不产出 URL——转发内媒体直链通常为时效性链接，不应进入持久文本。
//
// 内置防膨胀上限（面向提示词场景的保守预算）：
//   - 每条子消息文本截断至 forwardNodeTextMaxRunes rune；
//   - 每层最多渲染 forwardMaxNodesPerLevel 条，超出部分以省略行标注；
//   - 嵌套层级最多 forwardMaxDepth 层，更深的关联内容以占位行标注。
//
// 空记录返回 ""。
func ForwardRecordText(rec *ForwardRecord) string {
	if rec == nil || len(rec.Nodes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【合并转发聊天记录】")
	if rec.Title != "" {
		b.WriteString(rec.Title)
	}
	fmt.Fprintf(&b, "（共 %d 条消息）\n", len(rec.Nodes))
	writeForwardNodes(&b, rec.Nodes, 1)
	return strings.TrimRight(b.String(), "\n")
}

// 渲染预算上限（见 ForwardRecordText）。
const (
	forwardMaxDepth         = 3
	forwardMaxNodesPerLevel = 50
	forwardNodeTextMaxRunes = 200
)

// writeForwardNodes 以 depth 层缩进递归渲染节点列表。
func writeForwardNodes(b *strings.Builder, nodes []ForwardNode, depth int) {
	indent := strings.Repeat("  ", depth-1)
	shown := nodes
	truncated := false
	if len(nodes) > forwardMaxNodesPerLevel {
		shown = nodes[:forwardMaxNodesPerLevel]
		truncated = true
	}
	for i, n := range shown {
		fmt.Fprintf(b, "%s%d. %s\n", indent, i+1, forwardNodeSummary(n))
		switch {
		case len(n.Related) > 0 && depth < forwardMaxDepth:
			writeForwardNodes(b, n.Related, depth+1)
		case len(n.Related) > 0:
			fmt.Fprintf(b, "%s  …（嵌套层级过深省略）\n", indent)
		}
	}
	if truncated {
		fmt.Fprintf(b, "%s…（其余 %d 条省略）\n", indent, len(nodes)-len(shown))
	}
}

// forwardNodeSummary 生成单条子消息的单行摘要：正文 + 媒体占位 + 类型标记。
func forwardNodeSummary(n ForwardNode) string {
	var parts []string
	images := 0
	for _, s := range n.Segments {
		switch s.Type {
		case SegmentText:
			if t := strings.TrimSpace(s.Text); t != "" {
				parts = append(parts, truncateForwardRunes(t, forwardNodeTextMaxRunes))
			}
		case SegmentImage:
			images++
		case SegmentAudio:
			parts = append(parts, "[语音]")
		case SegmentVideo:
			parts = append(parts, "[视频]")
		case SegmentFile:
			parts = append(parts, "[文件]")
		}
	}
	switch {
	case images == 1:
		parts = append(parts, "[图片]")
	case images > 1:
		parts = append(parts, fmt.Sprintf("[图片x%d]", images))
	}
	switch n.Kind {
	case ForwardKindQuote:
		parts = append(parts, "[引用消息]")
	case ForwardKindRecord:
		parts = append(parts, "[嵌套聊天记录]")
	}
	if len(parts) == 0 {
		if n.Sender.DisplayName != "" {
			return n.Sender.DisplayName + ": （无文本内容）"
		}
		return ""
	}
	summary := strings.Join(parts, " ")
	if n.Sender.DisplayName != "" {
		return n.Sender.DisplayName + ": " + summary
	}
	return summary
}

// truncateForwardRunes 按 rune 截断文本，超长补省略号。
func truncateForwardRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
