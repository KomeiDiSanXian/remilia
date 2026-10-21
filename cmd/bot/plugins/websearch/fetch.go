package websearch

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/outbound"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// fetchPage 抓取网页并提取可读文本。
//
// 安全约束：
//   - 仅允许 https；禁止内网/保留地址（SSRF 防护，含重定向逐跳校验）
//   - 准入与拨号校验由 [outbound.Client]（Untrusted 策略）完成：请求前
//     判定方案/地址，拨号时对解析结果二次校验并直连已校验 IP
//   - 响应大小上限 maxBytes（超限截断）
//   - 提取的文本按 maxRunes 截断，防止巨型页面撑爆上下文
func fetchPage(ctx context.Context, client *outbound.Client, rawURL string, maxBytes int64, maxRunes int) (string, error) {
	if err := client.Allow(rawURL); err != nil {
		return "", err
	}

	req, err := newRequest(ctx, rawURL)
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
	data, truncated, err := readBody(resp.Body, limit)
	if err != nil {
		return "", err
	}
	// 页面编码在响应头未声明时由 HTML meta 标签决定（charset.NewReader 读取
	// 前 1024 字节完成嗅探），GBK 等中文站点因此不会整页乱码。
	contentType := resp.Header.Get("Content-Type")
	reader, err := charset.NewReader(strings.NewReader(string(data)), contentType)
	if err != nil {
		return "", err
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	text := extractContent(contentType, decoded)
	runes := []rune(text)
	if maxRunes > 0 && len(runes) > maxRunes {
		text = string(runes[:maxRunes]) + "\n…(内容过长已截断)"
	}
	if truncated {
		text += "\n…(响应超过大小上限，仅保留前段)"
	}
	return text, nil
}

// readBody 读取响应体并强制大小上限；超限时截断并报告。
func readBody(body io.Reader, limit int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

// extractContent 按内容类型选择提取方式：
//   - HTML / XHTML（或类型未知，按 HTML 处理）：标题 + 正文文本块；
//   - 其他类型（JSON / 纯文本 / Markdown 等）：原文直接截断使用，
//     不按 HTML 解析，避免结构化数据被标签清洗成不可用文本。
func extractContent(contentType string, data []byte) string {
	mediaType := normalizeMediaType(contentType)
	if mediaType == "" || isHTMLMediaType(mediaType) {
		return extractPageText(data)
	}
	return strings.TrimSpace(string(data))
}

// isHTMLMediaType 报告已归一化的 MIME 类型是否按 HTML 处理。
func isHTMLMediaType(mediaType string) bool {
	switch mediaType {
	case "text/html", "application/xhtml+xml", "application/xml", "text/xml":
		return true
	}
	return strings.HasSuffix(mediaType, "+html") || strings.HasSuffix(mediaType, "+xml")
}

// normalizeMediaType 归一化 Content-Type 的 MIME 部分（小写、去参数与首尾空白）。
func normalizeMediaType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	}
	return strings.ToLower(mediaType)
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

// fetchTTL 同一 URL 的抓取缓存时长：同一任务内模型重复抓取同一链接时
// 直接复用结果，避免重复出网（结果本身已按 rune 截断，体积有界）。
const fetchTTL = 5 * time.Minute

// fetchCached 带 TTL 缓存的抓取：命中缓存直接返回上次结果。
func (p *Plugin) fetchCached(ctx context.Context, rawURL string) (string, error) {
	if p.cache != nil {
		if entry, ok := p.cache.Get(rawURL); ok && entry.text != "" {
			return entry.text, nil
		}
	}
	if err := p.acquire(ctx); err != nil {
		return "", err
	}
	defer p.release()

	text, err := fetchPage(ctx, p.client, rawURL, p.cfg.MaxFetchBytes, p.cfg.MaxFetchRunes)
	if err != nil {
		return "", err
	}
	if p.cache != nil && strings.TrimSpace(text) != "" {
		p.cache.Set(rawURL, fetchEntry{text: text}, fetchTTL)
	}
	return text, nil
}
