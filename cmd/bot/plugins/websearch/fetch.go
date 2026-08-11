package websearch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// ssrfPolicy 目标主机 SSRF 准入策略（默认仅公网）。
// 测试可覆盖以访问本地 TLS 服务器（httptest.NewTLSServer 绑定回环地址）。
var ssrfPolicy = func(host string) bool { return isPublicHost(host) }

// fetchPage 抓取网页并提取纯文本（标题 + 正文段落）。
//
// 安全约束：
//   - 仅允许 https；禁止内网/保留地址（SSRF 防护，含重定向逐跳校验）
//   - 响应大小上限 maxBytes（超限截断）
//   - 提取的正文按 maxRunes 截断，防止巨型页面撑爆上下文
func fetchPage(ctx context.Context, client *http.Client, rawURL string, maxBytes int64, maxRunes int) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return "", fmt.Errorf("仅支持 https 链接")
	}
	if u.User != nil {
		return "", fmt.Errorf("链接不允许携带用户信息")
	}
	if !ssrfPolicy(u.Hostname()) {
		return "", fmt.Errorf("目标地址不是公网地址（SSRF 防护）")
	}

	// 逐跳校验重定向目标（防重定向到内网）。
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("重定向次数过多")
		}
		if !ssrfPolicy(req.URL.Hostname()) {
			return fmt.Errorf("重定向到非公网地址被阻止（SSRF 防护）")
		}
		return nil
	}

	req, err := newRequest(ctx, u.String())
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("页面返回状态 %d", resp.StatusCode)
	}

	limit := maxBytes
	if limit <= 0 {
		limit = 2 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		data = data[:limit]
	}

	text := extractPageText(data)
	runes := []rune(text)
	if maxRunes > 0 && len(runes) > maxRunes {
		text = string(runes[:maxRunes]) + "\n…(内容过长已截断)"
	}
	return text, nil
}

// isPublicHost 检查主机名解析结果是否全部为公网 IP（SSRF 防护）。
func isPublicHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return isPublicIP(ip)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return false
		}
	}
	return true
}

// isPublicIP 判断 IP 是否为公网地址。
func isPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}

// extractPageText 将 HTML 提取为可读纯文本：
// 标题行 + 各文本块（跳过 script/style/nav/footer 等非正文节点，折叠空白）。
func extractPageText(data []byte) string {
	doc, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		// 解析失败时退化为去除标签的近似文本。
		return collapseSpaces(strings.TrimSpace(html.UnescapeString(stripTags(string(data)))))
	}

	var title string
	var blocks []string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			case "script", "style", "noscript", "svg", "nav", "footer", "header", "aside", "form":
				return
			case "title":
				if t := n.FirstChild; t != nil {
					title = strings.TrimSpace(t.Data)
				}
			}
		}
		if n.Type == html.TextNode {
			if s := collapseSpaces(n.Data); s != "" {
				blocks = append(blocks, s)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	// 段落级去重 + 合并（连续短行拼接为段落）。
	var parts []string
	if title != "" {
		parts = append(parts, "标题："+title)
	}
	text := joinTextBlocks(blocks)
	if text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

// joinTextBlocks 将文本块合并为段落（块尾无句读且较短时与下一块拼接，
// 避免 HTML 标签断行把一句话切成碎片）。
func joinTextBlocks(blocks []string) string {
	var paragraphs []string
	var cur strings.Builder
	for _, b := range blocks {
		if cur.Len() > 0 {
			prev := cur.String()
			if len(prev) > 0 && (strings.HasSuffix(prev, "。") || strings.HasSuffix(prev, "！") ||
				strings.HasSuffix(prev, "？") || strings.HasSuffix(prev, ".") || strings.HasSuffix(prev, "!") ||
				strings.HasSuffix(prev, "?") || len(prev) > 120) {
				paragraphs = append(paragraphs, strings.TrimSpace(cur.String()))
				cur.Reset()
			} else {
				cur.WriteString(" ")
			}
		}
		cur.WriteString(b)
	}
	if cur.Len() > 0 {
		paragraphs = append(paragraphs, strings.TrimSpace(cur.String()))
	}
	return strings.Join(paragraphs, "\n")
}

// collapseSpaces 折叠连续空白为单个空格并去除首尾。
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// stripTags 极简去标签（HTML 解析失败的兜底）。
func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}
