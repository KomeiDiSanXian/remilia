package sauce

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// ── 结果合并 / 去重 / 排序 ─────────────────────────────────────────────

// mergeResults 合并多引擎结果：
//   - 按规范化外链（或标题+时间轴）去重，统计多引擎命中次数
//   - 相同结果取相似度更高者，并标记命中的全部引擎
//   - 按相似度降序排列
//   - threshold > 0 时，仅当存在不低于阈值的匹配才过滤（否则全部保留，便于裁切图展示）
func mergeResults(all []SearchResult, threshold float64) []SearchResult {
	byKey := map[string]*SearchResult{}
	var order []string

	for i := range all {
		r := all[i]
		key := resultDedupKey(r)

		if existing, ok := byKey[key]; ok {
			existing.Hits++
			if parseSimilarity(r.Similarity) > parseSimilarity(existing.Similarity) {
				existing.Similarity = r.Similarity
			}
			if existing.Title == "" {
				existing.Title = r.Title
			}
			if !strings.Contains(existing.Source, r.Source) {
				existing.Source += "+" + r.Source
			}
			continue
		}

		if r.Hits == 0 {
			r.Hits = 1
		}
		byKey[key] = &r
		order = append(order, key)
	}

	merged := make([]SearchResult, 0, len(order))
	for _, k := range order {
		merged = append(merged, *byKey[k])
	}

	sortResults(merged)

	if threshold > 0 {
		var high []SearchResult
		for _, r := range merged {
			if parseSimilarity(r.Similarity) >= threshold {
				high = append(high, r)
			}
		}
		if len(high) > 0 {
			return high
		}
	}

	return merged
}

// pickResults 截取前 maxResults 条结果。
func pickResults(results []SearchResult, maxResults int) []SearchResult {
	if maxResults > 0 && len(results) > maxResults {
		return results[:maxResults]
	}
	return results
}

// resultDedupKey 生成用于跨引擎去重的键。
func resultDedupKey(r SearchResult) string {
	if len(r.ExtURLs) > 0 {
		return "url:" + normalizeResultURL(r.ExtURLs[0])
	}
	if r.Source == "TraceMoe" {
		return "tracemoe:" + r.Title + "|" + r.Episode + "|" + r.Timestamp
	}
	return "other:" + r.Source + "|" + r.Title + "|" + r.Thumbnail
}

// sortResults 按相似度降序排列。
func sortResults(results []SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		si, sj := parseSimilarity(results[i].Similarity), parseSimilarity(results[j].Similarity)
		if si == sj {
			return results[i].Hits > results[j].Hits
		}
		return si > sj
	})
}

// ── URL / 文本工具函数 ────────────────────────────────────────────────

// normalizeResultURL 规范化 URL 用于去重键（去协议、www、查询参数、尾斜杠）。
func normalizeResultURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = normalizeResultURLRaw(u)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "www.")
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimSuffix(u, "/")
	return strings.ToLower(u)
}

// normalizeResultURLRaw 将协议相对 URL（//...）补全为绝对 https URL。
func normalizeResultURLRaw(raw string) string {
	u := strings.TrimSpace(raw)
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}

// hostOf 提取 URL 的域名。
func hostOf(raw string) string {
	u := normalizeResultURLRaw(raw)
	p, err := url.Parse(u)
	if err != nil {
		return strings.TrimSpace(raw)
	}
	return p.Host
}

// formatSimilarity 将相似度浮点数格式化为两位小数字符串（如 97.00）。
func formatSimilarity(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// firstLine 返回文本的首行（去掉多余换行）。
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// escapeURLParam URL 查询参数转义。
func escapeURLParam(s string) string {
	return url.QueryEscape(s)
}

// limitResults 将结果截断到 maxResults 条（maxResults <= 0 表示不限制）。
func limitResults(results []SearchResult, maxResults int) []SearchResult {
	if maxResults > 0 && len(results) > maxResults {
		return results[:maxResults]
	}
	return results
}
