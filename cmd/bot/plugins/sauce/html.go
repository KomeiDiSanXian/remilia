package sauce

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ── HTML 工具函数 ─────────────────────────────────────────────────────

// visitNodes 深度遍历 HTML 节点树，对每个节点调用 fn。
func visitNodes(n *html.Node, fn func(*html.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		visitNodes(c, fn)
	}
}

// findFirst 在节点子树内查找第一个指定类型的后代节点。
func findFirst(n *html.Node, atomID atom.Atom) *html.Node {
	if n == nil {
		return nil
	}
	if n.DataAtom == atomID {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirst(c, atomID); found != nil {
			return found
		}
	}
	return nil
}

// getAttr 获取节点的指定属性值。
func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// hasClass 检查节点是否包含指定的 class。
func hasClass(n *html.Node, class string) bool {
	c := getAttr(n, "class")
	return strings.Contains(c, class)
}

// getTextContent 获取节点下的纯文本内容。
func getTextContent(n *html.Node) string {
	var b strings.Builder
	visitNodes(n, func(cn *html.Node) {
		if cn.Type == html.TextNode {
			b.WriteString(strings.TrimSpace(cn.Data))
		}
	})
	return strings.TrimSpace(b.String())
}
