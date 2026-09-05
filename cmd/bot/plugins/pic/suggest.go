// Package pic suggest.go — 无结果时的相似标签推荐。
//
// 探测端点：Moebooru（konachan.net / yande.re）的 /tag.json?name=<前缀>*
// （支持 * 通配前缀搜索，按站内热度排序）。Gelbooru 系的标签补全接口
// 已废弃（safebooru 返回空 body、gelbooru.com 返回 "Deprecated" 文本，
// 2026-09 实测），故仅 Moebooru 可作探测源。
//
// 流程：/pic 无结果 → 逐标签探测相似标签 → 回复"你是不是想找"建议；
// 探测失败或无建议时降级为通用拼写提示，绝不阻塞回复。
package pic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/zhtext"
)

// ── 常量 ─────────────────────────────────────────────────────────────────

const (
	// suggestProbeLimit 单次前缀探测返回的候选标签数（按站内热度排序）。
	suggestProbeLimit = 15
	// suggestMax 每个标签最多推荐的数量。
	suggestMax = 3
	// suggestTimeout 相似标签探测总超时（超时降级为通用提示，不阻塞回复）。
	suggestTimeout = 10 * time.Second
	// suggestMaxTags 最多逐个探测的用户标签数（避免多标签时请求数爆炸）。
	suggestMaxTags = 3
	// suggestPrefixRunes 截断前缀探测的长度（覆盖"中间拼错"场景，
	// 如 touhuo → tou* → touhou）。
	suggestPrefixRunes = 3
)

// ── 数据模型 ─────────────────────────────────────────────────────────────

// tagSuggestion 相似标签推荐条目。
type tagSuggestion struct {
	Name  string
	Count int // 站内帖子数（热度，仅用于排序展示）
}

// moebooruTagRow Moebooru /tag.json 单行。
type moebooruTagRow struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Type  int    `json:"type"`
}

// ── 站点支持判定 ──────────────────────────────────────────────────────────

// supportsSuggest 报告站点是否支持标签推荐探测。
// Gelbooru 系的 page=autocomplete 已废弃，仅 Moebooru 协议可用。
func (s site) supportsSuggest() bool {
	return s.Protocol == protocolMoebooru
}

// suggestProbes 返回相似标签探测站点：用户指定的站点优先（若支持），
// 之后附加其余支持探测的内置站点。推荐的是标签名而非内容，
// 与 rating 分级无关；无支持站点时返回 nil。
func suggestProbes(specified string) []site {
	var probes []site
	if s, ok := findSite(specified); ok && s.supportsSuggest() {
		probes = append(probes, s)
	}
	for _, s := range builtinSites {
		if !s.supportsSuggest() {
			continue
		}
		if !slices.ContainsFunc(probes, func(p site) bool { return p.Name == s.Name }) {
			probes = append(probes, s)
		}
	}
	return probes
}

// ── 探测实现 ─────────────────────────────────────────────────────────────

// suggestTag 为单个用户标签做相似标签探测。
//
// 返回：
//   - ok：是否至少一个探测站点请求成功（全失败时结果不可信）
//   - known：探测站点是否收录该标签（前缀结果中含完整同名标签）
//   - suggs：按相似度排序的推荐列表（不含标签本身）
//
// 逐站尝试：结果已含该标签（收录即存在）则提前结束；否则合并下一个
// 站点的结果再判定——不同 Moebooru 站标签收录有差异
// （各站收录策略不同，2026-09 实测）。
func (c *booruClient) suggestTag(ctx context.Context, probes []site, term string) (ok, known bool, suggs []tagSuggestion) {
	pool := make(map[string]int, suggestProbeLimit*2)
	probed := false
	for _, s := range probes {
		rows, err := c.fetchTagSuggestions(ctx, s, term)
		if err != nil {
			continue // 单站失败不致命，换下一站
		}
		probed = true
		for _, row := range rows {
			if _, ok := pool[row.Name]; !ok {
				pool[row.Name] = row.Count
			}
		}
		if _, ok := pool[term]; ok {
			break
		}
	}
	if !probed {
		return false, false, nil
	}
	_, known = pool[term]
	return true, known, rankSuggestions(term, pool)
}

// fetchTagSuggestions 请求站点的标签候选：完整前缀（term*）与截断前缀
// （前 3 字符*）两路探测，合并去重后返回。
func (c *booruClient) fetchTagSuggestions(ctx context.Context, s site, term string) ([]tagSuggestion, error) {
	merged := make(map[string]int, suggestProbeLimit*2)
	for _, prefix := range suggestPrefixes(term) {
		rows, err := c.fetchMoebooruTags(ctx, s, prefix)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if _, ok := merged[row.Name]; !ok {
				merged[row.Name] = row.Count
			}
		}
	}
	out := make([]tagSuggestion, 0, len(merged))
	for name, count := range merged {
		out = append(out, tagSuggestion{Name: name, Count: count})
	}
	return out, nil
}

// suggestPrefixes 生成前缀探测序列：完整标签 + 截断前缀（≤suggestPrefixRunes
// 字符的短标签只有完整前缀一路）。
func suggestPrefixes(term string) []string {
	prefixes := []string{term}
	if r := []rune(term); len(r) > suggestPrefixRunes {
		if p := string(r[:suggestPrefixRunes]); p != term {
			prefixes = append(prefixes, p)
		}
	}
	return prefixes
}

// fetchMoebooruTags 请求 /tag.json?name=<prefix>*&order=count&limit=N
// （name 支持 * 通配，结果按站内热度降序）。
func (c *booruClient) fetchMoebooruTags(ctx context.Context, s site, prefix string) ([]moebooruTagRow, error) {
	endpoint := fmt.Sprintf("https://%s/tag.json?limit=%d&order=count&name=%s",
		s.Domain, suggestProbeLimit, url.QueryEscape(prefix+"*")) + cacheBust()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", picUserAgent)

	body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	// 无匹配时部分站点返回空 body
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}

	var rows []moebooruTagRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return rows, nil
}

// ── 排序与文案 ───────────────────────────────────────────────────────────

// rankSuggestions 从候选池挑选 top-N 推荐：编辑距离升序 → 热度降序 →
// 字典序（稳定性）。排除标签本身与距离过远（> maxDist）的噪音候选。
func rankSuggestions(term string, pool map[string]int) []tagSuggestion {
	termRunes := len([]rune(term))
	// 距离上限：≤2 或标签长度一半（短标签容忍 2，长标签按比例放宽），
	// 过远的候选只是"同前缀的另一回事"，推荐出去会误导。
	maxDist := termRunes / 2
	maxDist = max(maxDist, 2)

	type scored struct {
		name  string
		dist  int
		count int
	}
	cands := make([]scored, 0, len(pool))
	for name, count := range pool {
		if name == term {
			continue
		}
		dist := zhtext.Rank(term, name)
		if dist < 0 || dist > maxDist {
			continue
		}
		cands = append(cands, scored{name: name, dist: dist, count: count})
	}
	slices.SortStableFunc(cands, func(a, b scored) int {
		if a.dist != b.dist {
			return a.dist - b.dist
		}
		if a.count != b.count {
			return b.count - a.count
		}
		return strings.Compare(a.name, b.name)
	})

	out := make([]tagSuggestion, 0, suggestMax)
	for _, c := range cands {
		if len(out) == suggestMax {
			break
		}
		out = append(out, tagSuggestion{Name: c.name, Count: c.count})
	}
	return out
}

// suggestibleTag 报告标签是否适合探测（小写化后跳过 meta 标签与取反标签）。
// booru 标签规范为小写，探测前统一转小写提升命中率。
func suggestibleTag(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" || strings.ContainsAny(tag, ":*-?") {
		return ""
	}
	return tag
}

// noMatchMessage 为"无匹配结果"构造引导文案：逐标签探测相似标签，
// 给出"你是不是想找"建议；探测不可用或无建议时返回通用拼写提示。
func (p *Plugin) noMatchMessage(parent context.Context, tags []string, probes []site) string {
	base := "没有找到匹配的图片"
	if len(tags) == 0 || len(probes) == 0 {
		return base + "，换个标签试试？（booru 标签为英文小写+下划线格式，如 touhou）"
	}

	ctx, cancel := context.WithTimeout(parent, suggestTimeout)
	defer cancel()

	var (
		parts     []string
		probed    int
		probeFail int
		topHint   string
	)
	for _, tag := range tags {
		if probed >= suggestMaxTags {
			break
		}
		term := suggestibleTag(tag)
		if term == "" {
			continue
		}
		ok, known, suggs := p.client.suggestTag(ctx, probes, term)
		probed++
		if !ok {
			probeFail++
			continue
		}
		if known {
			continue
		}
		if len(suggs) == 0 {
			parts = append(parts, fmt.Sprintf("标签「%s」未收录，请检查拼写", term))
			continue
		}
		names := make([]string, 0, len(suggs))
		for _, sg := range suggs {
			names = append(names, sg.Name)
		}
		parts = append(parts, fmt.Sprintf("标签「%s」未收录，你是不是想找：%s", term, strings.Join(names, "、")))
		if topHint == "" {
			topHint = suggs[0].Name
		}
	}

	switch {
	case len(parts) == 0 && probeFail == probed && probed > 0:
		// 探测全部失败：结果不可信，不要误导用户说标签有错
		return base + "，换个标签试试？"
	case len(parts) == 0:
		// 探测到的标签都存在：可能是组合太冷门或分级过滤后无内容
		return base + "。标签拼写看起来没问题，可能是组合太冷门、" +
			"或该标签在当前内容分级（plugins.pic.rating）下无可用图片，试试减少标签？"
	}

	msg := base + "。" + strings.Join(parts, "；")
	// 单标签场景给出可直接复制的替换命令
	if topHint != "" && probed == 1 {
		msg += "（试试：/pic " + topHint + "）"
	}
	return msg
}
