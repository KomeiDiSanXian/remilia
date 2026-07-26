package satori

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/KomeiDiSanXian/remilia/platform"
	nethtml "golang.org/x/net/html"
)

// selfClosingQuoteRe 匹配自闭合的 <quote .../> 元素（不匹配 <quote>...</quote>）。
var selfClosingQuoteRe = regexp.MustCompile(`(?i)<quote\b[^>]*/>`)

// ─────────────────────────────────────────────────────────────────────────────
// 编码：platform.OutboundMessage → Satori XML 消息字符串
// ─────────────────────────────────────────────────────────────────────────────

// EncodeOutboundMessage 将平台无关的 OutboundMessage 转换为
// Satori 消息内容的 XML 字符串。
//
// Satori 消息内容使用类 XHTML 语法，包含 <at>、<img>、<audio>、
// <video>、<file>、<a> 等标准元素。
//
// 参考：https://satori.chat/zh-CN/protocol/elements.html
func EncodeOutboundMessage(msg platform.OutboundMessage) string {
	var b strings.Builder

	// 引用（回复）元素 – 必须出现在最前面
	if msg.ReplyToID != "" {
		fmt.Fprintf(&b, `<quote id=%s/>`, escapeAttr(msg.ReplyToID))
	}

	// @提及 → <at id="..."/>
	for _, uid := range msg.Mentions {
		fmt.Fprintf(&b, `<at id=%s/>`, escapeAttr(uid))
	}

	// 文本内容（XML 转义）
	if msg.Text != "" {
		b.WriteString(escapeText(msg.Text))
	} else if msg.Markdown != "" {
		// Markdown 作为纯文本传递；Satori 没有原生的 markdown 元素
		b.WriteString(escapeText(msg.Markdown))
	}

	// 附件 → <img/> / <audio/> / <video/> / <file/>
	for _, att := range msg.Attachments {
		if att.URL == "" {
			// 仅含数据的附件无法内联到 Satori XML 中。
			// 必须先通过资源上传 API 上传，再替换为 URL。
			// 此处静默跳过；调用方应在调用前完成上传。
			continue
		}
		switch att.Kind {
		case platform.AttachmentKindImage:
			if att.Name != "" {
				fmt.Fprintf(&b, `<img src=%s title=%s/>`, escapeAttr(att.URL), escapeAttr(att.Name))
			} else {
				fmt.Fprintf(&b, `<img src=%s/>`, escapeAttr(att.URL))
			}
		case platform.AttachmentKindAudio:
			fmt.Fprintf(&b, `<audio src=%s/>`, escapeAttr(att.URL))
		case platform.AttachmentKindVideo:
			fmt.Fprintf(&b, `<video src=%s/>`, escapeAttr(att.URL))
		default: // AttachmentKindFile
			if att.Name != "" {
				fmt.Fprintf(&b, `<file src=%s title=%s/>`, escapeAttr(att.URL), escapeAttr(att.Name))
			} else {
				fmt.Fprintf(&b, `<file src=%s/>`, escapeAttr(att.URL))
			}
		}
	}

	// 按钮 → <button> 元素（Satori 实验性功能）
	for _, btn := range msg.Buttons {
		switch btn.Style {
		case platform.ButtonStyleLink:
			fmt.Fprintf(&b, `<button type="link" href=%s>%s</button>`, escapeAttr(btn.URL), escapeText(btn.Label))
		default:
			if btn.ID != "" {
				fmt.Fprintf(&b, `<button type="action" id=%s>%s</button>`, escapeAttr(btn.ID), escapeText(btn.Label))
			} else {
				fmt.Fprintf(&b, `<button type="action">%s</button>`, escapeText(btn.Label))
			}
		}
	}

	return b.String()
}

// escapeText 转义 Satori XML 消息内容中具有特殊含义的字符：<、>、& 和 "。
func escapeText(s string) string {
	return html.EscapeString(s)
}

// escapeAttr 将字符串转义为带双引号的 XML 属性值。
//
// 不能使用 %q：Go 的 %q 以反斜杠转义双引号，而 XML/HTML 中反斜杠并非转义字符，
// 属性值仍会被用户输入中的 " 提前闭合，从而可注入任意元素
// （如文件名 `a"><at type='all'/>` 会变成真实的 @全体成员元素）。
func escapeAttr(s string) string {
	return `"` + html.EscapeString(s) + `"`
}

// ─────────────────────────────────────────────────────────────────────────────
// 解码：Satori 消息内容字符串 → 纯文本 + 附件
// ─────────────────────────────────────────────────────────────────────────────

// ParseMessageContent 解析 Satori XML 消息内容字符串，返回：
//   - text：所有文本节点和内联元素的纯文本表示
//   - attachments：内容中找到的所有资源元素（img、audio、video、file）
//
// 参考：https://satori.chat/zh-CN/protocol/elements.html
func ParseMessageContent(content string) (text string, attachments []platform.InboundAttachment) {
	if content == "" {
		return "", nil
	}

	// 先移除自闭合的 <quote .../>：HTML 解析器不认识该元素，会把其后的兄弟节点
	// 解析为它的子节点，而 traverseNodes 对 quote 是整体跳过的，
	// 导致 "<quote id=\"1\"/>用户正文" 这种（多数 Satori SDK 的回复格式）
	// 整条正文与附件全部丢失。提取纯文本本就忽略引用内容，直接剥离最安全。
	content = selfClosingQuoteRe.ReplaceAllString(content, "")
	if content == "" {
		return "", nil
	}

	// 包裹在根元素中，使 HTML 解析器能正确处理片段。
	wrapped := "<div>" + content + "</div>"

	doc, err := nethtml.Parse(strings.NewReader(wrapped))
	if err != nil {
		// 解析失败时，降级为返回原始内容的纯文本
		return html.UnescapeString(content), nil
	}

	var textBuf strings.Builder
	traverseNodes(doc, &textBuf, &attachments)
	return strings.TrimSpace(textBuf.String()), attachments
}

// traverseNodes 递归遍历 HTML 节点，收集文本和附件。
func traverseNodes(n *nethtml.Node, text *strings.Builder, attachments *[]platform.InboundAttachment) {
	switch n.Type {
	case nethtml.TextNode:
		// n.Data 已由 nethtml.Parse 完成实体解码，此处不可再次 UnescapeString，
		// 否则用户原文中的 "&amp;lt;" 会被二次解码成 "<"，破坏消息内容。
		text.WriteString(n.Data)
		return
	case nethtml.ElementNode:
		tag := strings.ToLower(n.Data)
		attrs := nodeAttrs(n)

		switch tag {
		case "img":
			src := attrs["src"]
			title := attrs["title"]
			att := platform.InboundAttachment{
				URL:  src,
				Name: title,
			}
			if w := attrInt(attrs, "width"); w > 0 {
				att.Width = w
			}
			if h := attrInt(attrs, "height"); h > 0 {
				att.Height = h
			}
			att.MimeType = "image/*"
			*attachments = append(*attachments, att)
			// 在文本中插入占位标记，表示图片位置
			text.WriteString("[图片]")
			return

		case "audio":
			att := platform.InboundAttachment{
				URL:      attrs["src"],
				MimeType: "audio/*",
				Name:     attrs["title"],
			}
			// 扩展属性：时长与封面（实验性）
			if dur := attrFloat(attrs, "duration"); dur > 0 || attrs["poster"] != "" {
				att.Extra = &MediaExtra{
					Duration: dur,
					Poster:   attrs["poster"],
				}
			}
			*attachments = append(*attachments, att)
			text.WriteString("[语音]")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, text, attachments)
			}
			return

		case "video":
			att := platform.InboundAttachment{
				URL:      attrs["src"],
				MimeType: "video/*",
				Name:     attrs["title"],
			}
			if w := attrInt(attrs, "width"); w > 0 {
				att.Width = w
			}
			if h := attrInt(attrs, "height"); h > 0 {
				att.Height = h
			}
			// 扩展属性：时长与封面（实验性）
			if dur := attrFloat(attrs, "duration"); dur > 0 || attrs["poster"] != "" {
				att.Extra = &MediaExtra{
					Duration: dur,
					Poster:   attrs["poster"],
				}
			}
			*attachments = append(*attachments, att)
			text.WriteString("[视频]")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, text, attachments)
			}
			return

		case "link":
			// 外部资源链接：渲染为 "链接: url"，同时加入附件
			src := attrs["src"]
			if src == "" {
				src = attrs["url"]
			}
			if src != "" {
				text.WriteString("[链接: " + src + "]")
				*attachments = append(*attachments, platform.InboundAttachment{
					URL:  src,
					Name: attrs["title"],
				})
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, text, attachments)
			}
			return

		case "file":
			att := platform.InboundAttachment{
				URL:  attrs["src"],
				Name: attrs["title"],
			}
			// 扩展属性：缩略图（实验性）
			if attrs["poster"] != "" {
				att.Extra = &MediaExtra{
					Poster: attrs["poster"],
				}
			}
			*attachments = append(*attachments, att)
			text.WriteString("[文件]")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, text, attachments)
			}
			return

		case "at":
			// @提及 → 显示为 "@name" 或 "@id" 或 "@role:name" 或 "@全体成员"
			if name := attrs["name"]; name != "" {
				text.WriteString("@" + name)
			} else if id := attrs["id"]; id != "" {
				text.WriteString("@" + id)
			} else if role := attrs["role"]; role != "" {
				// 角色提及（实验性）
				text.WriteString("@role:" + role)
			} else if typ := attrs["type"]; typ == "all" {
				text.WriteString("@全体成员")
			} else if typ == "here" {
				text.WriteString("@在线成员")
			}
			// Go HTML 解析器会将紧跟在未知自闭合元素（如 <at/>）后面的兄弟节点
			// 解析为该元素的子节点，需继续遍历以避免内容丢失。
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, text, attachments)
			}
			return

		case "sharp":
			// #频道提及
			if name := attrs["name"]; name != "" {
				text.WriteString("#" + name)
			} else if id := attrs["id"]; id != "" {
				text.WriteString("#" + id)
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, text, attachments)
			}
			return

		case "emoji":
			if name := attrs["name"]; name != "" {
				text.WriteString(":" + name + ":")
			} else if id := attrs["id"]; id != "" {
				text.WriteString("[emoji:" + id + "]")
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, text, attachments)
			}
			return

		case "a":
			// 渲染为 "标签文本 (href)"
			href := attrs["href"]
			var labelBuf strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, &labelBuf, attachments)
			}
			label := strings.TrimSpace(labelBuf.String())
			if label != "" && href != "" {
				text.WriteString(label + " (" + href + ")")
			} else if href != "" {
				text.WriteString(href)
			} else {
				text.WriteString(label)
			}
			return

		case "br":
			text.WriteString("\n")
			return

		case "p":
			// 段落：前后各加换行
			text.WriteString("\n")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, text, attachments)
			}
			text.WriteString("\n")
			return

		case "b", "strong", "i", "em", "u", "ins", "s", "del", "spl", "sup", "sub", "code":
			// 装饰性元素：递归进入子节点
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, text, attachments)
			}
			return

		case "quote":
			// 提取纯文本时跳过引用内容
			return

		case "author":
			// <author> 元素用于表示消息的作者（在 <message> 内使用）。
			// 文本层面渲染为 "[来自: name]"（或 "[来自: id]"）。
			name := attrs["name"]
			if name == "" {
				name = attrs["id"]
			}
			if name != "" {
				text.WriteString("[来自: " + name + "] ")
			}
			// 继续递归处理子节点（通常 <author> 为自闭合元素，子节点为空）。
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, text, attachments)
			}
			return

		case "message":
			// <message> 元素表示一条独立消息或转发消息。
			// 递归解析子节点（可能含 <author>、文本、资源等）。
			id := attrs["id"]
			_, isForward := attrs["forward"] // boolean 属性：存在即为 true

			var innerBuf strings.Builder
			var innerAtts []platform.InboundAttachment
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, &innerBuf, &innerAtts)
			}
			inner := strings.TrimSpace(innerBuf.String())

			// 转发消息（含 id 或 forward 属性）存入 attachments，供高层消费
			if id != "" || isForward {
				fwd := &ForwardedMessage{
					ID:      id,
					Forward: true,
					Content: inner,
				}
				// 提取 <author> 信息（已被 traverseNodes 写入 innerBuf，
				// 此处直接从 inner 获取内容即可；精细解析可按需扩展）
				*attachments = append(*attachments, platform.InboundAttachment{
					Extra: fwd,
				})
				if inner != "" {
					text.WriteString("[转发: " + inner + "]")
				} else if id != "" {
					text.WriteString("[转发消息 ID:" + id + "]")
				} else {
					text.WriteString("[合并转发]")
				}
			} else {
				// 无 id/forward 的 <message> 仅用于分隔多条消息，渲染内容即可
				text.WriteString(inner)
				*attachments = append(*attachments, innerAtts...)
			}
			return

		case "button":
			// 渲染按钮标签文本
			var labelBuf strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				traverseNodes(c, &labelBuf, attachments)
			}
			label := strings.TrimSpace(labelBuf.String())
			if label != "" {
				text.WriteString("[" + label + "]")
			}
			return

		case "root", "html", "head", "body":
			// Go HTML 解析器添加的结构包装元素；穿透继续递归
		}
	}

	// 默认：递归处理子节点
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		traverseNodes(c, text, attachments)
	}
}

// nodeAttrs 返回 HTML 节点的属性映射表。
func nodeAttrs(n *nethtml.Node) map[string]string {
	m := make(map[string]string, len(n.Attr))
	for _, a := range n.Attr {
		m[a.Key] = a.Val
	}
	return m
}

// attrInt 解析整数类型的属性值，解析失败时返回 0。
func attrInt(attrs map[string]string, key string) int {
	v, ok := attrs[key]
	if !ok || v == "" {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(v, "%d", &n)
	return n
}

// attrFloat 解析浮点数类型的属性值，解析失败时返回 0。
func attrFloat(attrs map[string]string, key string) float64 {
	v, ok := attrs[key]
	if !ok || v == "" {
		return 0
	}
	var f float64
	_, _ = fmt.Sscanf(v, "%f", &f)
	return f
}
