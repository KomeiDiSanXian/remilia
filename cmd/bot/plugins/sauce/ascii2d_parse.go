package sauce

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func parseASCII2DResults(body []byte) []SearchResult {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}

	var results []SearchResult
	visitNodes(doc, func(n *html.Node) {
		if n.DataAtom != atom.Div {
			return
		}
		class := getAttr(n, "class")
		if class != "row item-box" {
			return
		}
		if r := extractASCII2DItem(n); r != nil {
			results = append(results, *r)
		}
	})

	return results
}

// extractASCII2DItem 从单个 .row.item-box 节点中提取搜索结果。
func extractASCII2DItem(n *html.Node) *SearchResult {
	r := &SearchResult{Source: "ASCII2D"}

	// 提取缩略图（改版后使用 src 而非 data-src，两者兼容处理）
	visitNodes(n, func(cn *html.Node) {
		if cn.DataAtom == atom.Img && cn.Parent != nil && hasClass(cn.Parent, "image-box") {
			src := getAttr(cn, "data-src")
			if src == "" {
				src = getAttr(cn, "src")
			}
			if src != "" {
				r.Thumbnail = normalizeASCII2DURL(src)
			}
		}
	})

	// 提取详情
	visitNodes(n, func(cn *html.Node) {
		if cn.DataAtom != atom.Div {
			return
		}
		class := getAttr(cn, "class")
		if !strings.Contains(class, "detail-box") {
			return
		}
		extractDetailBox(cn, r)
	})

	if len(r.ExtURLs) == 0 {
		return nil
	}
	return r
}

// extractDetailBox 从 detail-box 节点中提取链接、标题、作者和来源信息。
func extractDetailBox(n *html.Node, r *SearchResult) {
	var hrefs []string
	var texts []string

	visitNodes(n, func(cn *html.Node) {
		switch cn.DataAtom {
		case atom.A:
			href := getAttr(cn, "href")
			if href == "" || strings.HasPrefix(href, "/") {
				return
			}
			hrefs = append(hrefs, normalizeASCII2DURL(href))
			texts = append(texts, getTextContent(cn))
		case atom.Small:
			if txt := strings.TrimSpace(getTextContent(cn)); txt != "" && r.SourceName == "" {
				r.SourceName = txt
			}
		}
	})

	if len(hrefs) == 0 {
		return
	}
	r.ExtURLs = hrefs

	if r.Title == "" && len(texts) > 0 && texts[0] != "" {
		r.Title = texts[0]
	}

	artwork := hrefs[0]
	switch {
	case strings.Contains(artwork, "twitter.com"):
		r.SourceName = "Twitter"
		parts := strings.Split(strings.TrimPrefix(artwork, "https://twitter.com/"), "/")
		if len(parts) > 0 && parts[0] != "" {
			r.Author = "@" + parts[0]
		}
	case strings.Contains(artwork, "pixiv.net"):
		r.SourceName = "Pixiv"
	case strings.Contains(artwork, "danbooru"):
		r.SourceName = "Danbooru"
	case strings.Contains(artwork, "yande.re"):
		r.SourceName = "Yande.re"
	case strings.Contains(artwork, "sankaku"):
		r.SourceName = "Sankaku"
	case strings.Contains(artwork, "gelbooru"):
		r.SourceName = "Gelbooru"
	}

	if r.Author == "" && len(texts) > 1 && texts[1] != "" {
		r.Author = texts[1]
	}
}

// mergeASCII2DModes 合并色合与特征检索结果（按首个外链去重）。
func mergeASCII2DModes(color, feature []SearchResult, maxResults int) []SearchResult {
	seen := map[string]bool{}
	var merged []SearchResult
	for _, list := range [][]SearchResult{color, feature} {
		for _, r := range list {
			key := ""
			if len(r.ExtURLs) > 0 {
				key = normalizeResultURL(r.ExtURLs[0])
			}
			if key == "" {
				key = r.Thumbnail
			}
			if key != "" {
				if seen[key] {
					continue
				}
				seen[key] = true
			}
			merged = append(merged, r)
		}
	}
	if maxResults > 0 && len(merged) > maxResults {
		return merged[:maxResults]
	}
	return merged
}
