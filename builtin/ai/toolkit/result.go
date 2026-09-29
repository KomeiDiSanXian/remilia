package toolkit

import (
	"strings"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// result.go — 工具的富结果模型。
//
// 既有工具只返回一段文本（[Tool.Execute]）；但外部工具（如 MCP 的
// tools/call）可以返回文本、图片、音频、资源与结构化数据。若直接压成字符串
// 会丢信息，因此这里定义框架自己的、可扩展的结果容器，由来源适配器负责把
// 外部结果转换进来——外部协议的字段形状不进入本层。
//
// 回填给模型时统一经 [ToolResult.Flatten] 得到文本；片段中的媒体/资源信息
// 在未来的多模态回灌中可被消费。既有工具无需改动。

// ResultKind 富结果片段的类型。
type ResultKind string

const (
	// ResultText 文本片段。
	ResultText ResultKind = "text"
	// ResultImage 图片片段（Data 或 URI 二选一）。
	ResultImage ResultKind = "image"
	// ResultAudio 音频片段。
	ResultAudio ResultKind = "audio"
	// ResultResource 资源引用（URI + 可选 MIME）。
	ResultResource ResultKind = "resource"
	// ResultStructured 结构化数据（JSON 序列化后置于 Text）。
	ResultStructured ResultKind = "structured"
)

// ResultPart 工具结果中的一个片段。
type ResultPart struct {
	// Kind 片段类型。
	Kind ResultKind
	// Text 文本内容（ResultText 为原文，ResultStructured 为 JSON 文本）。
	Text string
	// Data 二进制内容（图片/音频；可为空）。
	Data []byte
	// MimeType 媒体类型（如图片 image/png）。
	MimeType string
	// URI 资源地址（ResultResource/ResultImage/ResultAudio 可用）。
	URI string
	// Name 片段名称（可选，用于展示）。
	Name string
}

// ToolResult 富工具结果：一组有序片段。
type ToolResult struct {
	// Parts 有序片段（文本/媒体/资源/结构化）。
	Parts []ResultPart
	// Attachments 媒体附件（可选）：工具的图片/音频等可直接走框架既有的附件
	// 通道发送。与 Parts 表达同一份媒体，供不同消费者使用；是否真正发送由
	// 装配侧决定，本层只承载。
	Attachments []platform.Attachment
}

// TextResult 构造只含文本的富结果。
func TextResult(text string) ToolResult {
	return ToolResult{Parts: []ResultPart{{Kind: ResultText, Text: text}}}
}

// Empty 报告结果是否不含任何有内容的片段。
func (r ToolResult) Empty() bool {
	for _, p := range r.Parts {
		if p.Text != "" || len(p.Data) > 0 || p.URI != "" {
			return false
		}
	}
	return true
}

// Flatten 把富结果渲染为回填给模型的文本：文本片段直出，其余片段降级为
// 可读的占位说明（媒体/资源信息不丢失语义，但不内联二进制）。
func (r ToolResult) Flatten() string {
	var b strings.Builder
	for _, p := range r.Parts {
		switch p.Kind {
		case ResultText, ResultStructured:
			if p.Text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(p.Text)
		case ResultImage, ResultAudio, ResultResource:
			if p.URI == "" && len(p.Data) == 0 && p.Name == "" && p.MimeType == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(describePart(p))
		default:
			if p.Text != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(p.Text)
			}
		}
	}
	return b.String()
}

// describePart 为媒体/资源片段生成降级文本。
func describePart(p ResultPart) string {
	label := string(p.Kind)
	switch p.Kind {
	case ResultImage:
		label = "图片"
	case ResultAudio:
		label = "音频"
	case ResultResource:
		label = "资源"
	}
	var b strings.Builder
	b.WriteString("[")
	b.WriteString(label)
	if p.Name != "" {
		b.WriteString(": ")
		b.WriteString(p.Name)
	}
	if p.MimeType != "" {
		b.WriteString(" (")
		b.WriteString(p.MimeType)
		b.WriteString(")")
	}
	if p.URI != "" {
		b.WriteString(" ")
		b.WriteString(p.URI)
	} else if len(p.Data) > 0 && p.Name == "" {
		b.WriteString(" 内联数据已省略")
	}
	b.WriteString("]")
	return b.String()
}
