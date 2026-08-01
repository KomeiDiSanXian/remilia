// Package pic format.go — 作品信息与错误信息的格式化输出。
package pic

import (
	"fmt"
	"strings"
)

// formatPostMD 将单个作品格式化为 Markdown 信息块。
func formatPostMD(p picPost, num int) string {
	var b strings.Builder

	// 标题行：编号 + 站点名 + 评分
	fmt.Fprintf(&b, "**%d.** [%s] 评分: `%d`", num, p.SiteName, p.Score)
	if p.Author != "" {
		fmt.Fprintf(&b, " | 画师: **%s**", p.Author)
	}
	b.WriteString("\n")

	// 标签（限长展示）
	if len(p.Tags) > 0 {
		tags := p.Tags
		if len(tags) > 12 {
			tags = tags[:12]
		}
		b.WriteString("`" + strings.Join(tags, "` `") + "`\n")
	}

	// 来源链接
	source := p.Source
	if source == "" {
		source = "无来源"
	} else if !strings.HasPrefix(source, "http") {
		source = "https://" + strings.TrimPrefix(source, "//")
	}
	fmt.Fprintf(&b, "[来源](%s)  |  原始图: %s\n", source, p.FileURL)

	return strings.TrimRight(b.String(), "\n")
}

// formatResultsMD 将多个作品拼接为汇总 Markdown。
func formatResultsMD(siteName string, posts []picPost) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🖼 随机图片 *%d* 张", len(posts))
	if siteName != "" {
		fmt.Fprintf(&b, "（来自 %s）", siteName)
	}
	b.WriteString("\n\n")
	for i, p := range posts {
		b.WriteString(formatPostMD(p, i+1))
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatPostText 将单个作品格式化为纯文本信息块（无 Markdown 能力时降级）。
func formatPostText(p picPost, num int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d. [%s] 评分 %d", num, p.SiteName, p.Score)
	if p.Author != "" {
		fmt.Fprintf(&b, " | 画师: %s", p.Author)
	}
	b.WriteString("\n")
	if len(p.Tags) > 0 {
		tags := p.Tags
		if len(tags) > 12 {
			tags = tags[:12]
		}
		b.WriteString("标签: " + strings.Join(tags, ", ") + "\n")
	}
	source := p.Source
	if source == "" {
		source = "无来源"
	} else if !strings.HasPrefix(source, "http") {
		source = "https://" + strings.TrimPrefix(source, "//")
	}
	b.WriteString("来源: " + source)
	b.WriteString("\n原图: " + p.FileURL)
	return strings.TrimRight(b.String(), "\n")
}

// formatResultsText 将多个作品拼接为汇总纯文本。
func formatResultsText(siteName string, posts []picPost) string {
	var b strings.Builder
	fmt.Fprintf(&b, "随机图片 %d 张", len(posts))
	if siteName != "" {
		fmt.Fprintf(&b, "（来自 %s）", siteName)
	}
	b.WriteString("\n\n")
	for i, p := range posts {
		b.WriteString(formatPostText(p, i+1))
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatToolResult 将作品格式化为 AI 工具返回文本（纯文本 + URL 列表）。
func formatToolResult(siteName string, posts []picPost) string {
	var b strings.Builder
	fmt.Fprintf(&b, "找到 %d 张随机图片（来自 %s）：\n", len(posts), siteName)
	for i, p := range posts {
		fmt.Fprintf(&b, "%d. %s\n", i+1, p.FileURL)
		if p.Author != "" {
			fmt.Fprintf(&b, "   画师: %s\n", p.Author)
		}
		if p.Source != "" {
			fmt.Fprintf(&b, "   来源: %s\n", p.Source)
		}
		if len(p.Tags) > 0 {
			tags := p.Tags
			if len(tags) > 12 {
				tags = tags[:12]
			}
			fmt.Fprintf(&b, "   标签: %s\n", strings.Join(tags, ", "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
