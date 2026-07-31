package sauce

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ── 输出格式化 ─────────────────────────────────────────────────────────

// 单条结果字段截断长度（rune 计数）
const (
	maxTitleRunes   = 120 // 标题（IQDB 的标签串可能非常长）
	maxAuthorRunes  = 40  // 作者名
	maxSourceRunes  = 24  // 来源站点名
	maxURLRunes     = 120 // 外链 URL
	maxEpisodeRunes = 16  // 话数/时间点
)

// maxTitleLen 标题最大长度（rune），0 表示不截断。
func (p *Plugin) maxTitleLen() int {
	if p.cfg == nil {
		return maxTitleRunes
	}
	return p.cfg.GetInt("max_title_len", maxTitleRunes)
}

// maxMessageLen 文本模式整条结果消息的最大长度（rune），0 表示不截断。
func (p *Plugin) maxMessageLen() int {
	if p.cfg == nil {
		return 2000
	}
	return p.cfg.GetInt("max_message_len", 2000)
}

// truncate 按 rune 截断文本，超出补省略号；limit <= 0 表示不截断。
func truncate(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	return platform.TruncateText(s, limit)
}

// formatResults 拼接整条结果消息（文本模式），并按 max_message_len 截断。
func (p *Plugin) formatResults(results []SearchResult, errReports []string, showErrors bool) string {
	var b strings.Builder
	b.WriteString("🔍 图片来源搜索\n━━━━━━━━━━━━━━\n\n")
	for i, r := range results {
		b.WriteString(p.formatOneResult(r, i+1))
		b.WriteString("\n\n")
	}
	if showErrors && len(errReports) > 0 {
		b.WriteString("（部分引擎异常）\n")
		b.WriteString(strings.Join(errReports, "\n"))
		b.WriteString("\n")
	}
	msg := strings.TrimRight(b.String(), "\n")
	return truncate(msg, p.maxMessageLen())
}

// formatOneResult 格式化单条搜索结果：相似度/标题、作者、话数时间点、
// 来源、命中引擎与外部链接，各字段按固定长度截断。
func (p *Plugin) formatOneResult(r SearchResult, num int) string {
	var b strings.Builder
	sim := r.Similarity
	title := r.Title
	if title == "" {
		title = "（无标题）"
	}
	title = truncate(title, p.maxTitleLen())
	if sim != "" {
		b.WriteString(fmt.Sprintf("%d. [%s%%] %s\n", num, sim, title))
	} else {
		b.WriteString(fmt.Sprintf("%d. %s\n", num, title))
	}

	if r.Author != "" {
		b.WriteString(fmt.Sprintf("   作者: %s\n", truncate(r.Author, maxAuthorRunes)))
	}

	// TraceMoe 额外展示话数与时间点
	if r.Episode != "" {
		b.WriteString(fmt.Sprintf("   话数: %s\n", truncate(r.Episode, maxEpisodeRunes)))
	}
	if r.Timestamp != "" {
		b.WriteString(fmt.Sprintf("   时间点: %s\n", truncate(r.Timestamp, maxEpisodeRunes)))
	}

	source := r.SourceName
	if source == "" {
		source = "未知来源"
	}
	b.WriteString(fmt.Sprintf("   来源: %s\n", truncate(source, maxSourceRunes)))

	if r.Hits > 1 {
		b.WriteString(fmt.Sprintf("   命中引擎: %s（%d 个）\n", truncate(r.Source, maxSourceRunes), r.Hits))
	}

	for _, u := range r.ExtURLs {
		b.WriteString(fmt.Sprintf("   %s\n", truncate(u, maxURLRunes)))
	}

	return strings.TrimRight(b.String(), "\n")
}
