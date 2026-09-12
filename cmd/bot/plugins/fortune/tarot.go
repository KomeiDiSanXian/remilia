package fortune

import (
	"fmt"
	"strings"
)

// tarotUsage 是 /tarot 的用法提示。
const tarotUsage = "用法: /tarot [张数]，张数只能填 1 或 3（只写 /tarot 即抽 1 张）"

// tarotPosition 牌阵中一个位置的含义。
type tarotPosition struct {
	Name  string // 位置名，如「过去」
	Focus string // 该位置关注的内容
}

// tarotSpreads 按张数定义牌阵位置。
//
//	1 张：整体运势
//	3 张：过去・现在・未来
var tarotSpreads = map[int][]tarotPosition{
	1: {
		{Name: "当前", Focus: "此刻的整体处境与行动建议"},
	},
	3: {
		{Name: "过去", Focus: "促成现状的起因"},
		{Name: "现在", Focus: "当前面对的处境"},
		{Name: "未来", Focus: "顺着眼下走势的走向"},
	},
}

// tarotPositionsFor 返回指定张数对应的牌阵位置；未定义的张数返回 nil。
func tarotPositionsFor(count int) []tarotPosition {
	return tarotSpreads[count]
}

// positionName 返回第 i 张牌的位置名。
// 单张牌阵没有位置概念，未定义的张数同样返回空串。
func positionName(positions []tarotPosition, i int) string {
	if len(positions) <= 1 || i < 0 || i >= len(positions) {
		return ""
	}
	return positions[i].Name
}

// tarotCircled 牌阵内的序号，最多支持到 10 张。
var tarotCircled = []string{"①", "②", "③", "④", "⑤", "⑥", "⑦", "⑧", "⑨", "⑩"}

// tarotIndex 返回第 i 张牌的序号，超出范围时退回普通数字。
func tarotIndex(i int) string {
	if i >= 0 && i < len(tarotCircled) {
		return tarotCircled[i]
	}
	return fmt.Sprintf("(%d)", i+1)
}

// tarotTitle 返回结果标题。单张牌只写牌名，多张牌列出牌阵位置。
func tarotTitle(readings []TarotReading) string {
	if len(readings) == 1 {
		return fmt.Sprintf("🃏 塔罗牌 · %s · %s", readings[0].Card.NameCN, readings[0].Orientation())
	}
	names := make([]string, 0, len(readings))
	for _, r := range tarotPositionsFor(len(readings)) {
		names = append(names, r.Name)
	}
	if len(names) == 0 {
		return "🃏 塔罗牌阵"
	}
	return "🃏 塔罗牌阵 · " + strings.Join(names, " · ")
}

// 解签末尾的固定说明，与御神签保持一致的结构。
const (
	// tarotSource 牌面出处说明。
	tarotSource = "🃏 牌面为公有领域韦特塔罗（Rider-Waite-Smith，1909 年首版）。"

	// tarotReverseHint 逆位提示，仅牌阵中出现逆位时附加。
	tarotReverseHint = "🔄 逆位并非单纯的不吉，通常表示这股力量受阻、内转或尚未成熟。"

	// tarotDisclaimer 免责声明。
	tarotDisclaimer = "🎲 仅供娱乐，请勿据此做重要决定。"
)

// tarotFooter 返回塔罗结果末尾的固定说明：牌面出处、逆位提示（仅出现逆位时）
// 与免责声明。
func tarotFooter(readings []TarotReading) string {
	lines := []string{tarotSource}
	for _, r := range readings {
		if r.IsReverse {
			lines = append(lines, tarotReverseHint)
			break
		}
	}
	return strings.Join(append(lines, tarotDisclaimer), "\n")
}

// formatTarotMD 把塔罗结果格式化为 Markdown 说明块，与牌阵图片同条发送。
func formatTarotMD(readings []TarotReading) string {
	positions := tarotPositionsFor(len(readings))
	single := len(readings) == 1

	var b strings.Builder
	b.WriteString("**" + tarotTitle(readings) + "**\n")

	for i, r := range readings {
		b.WriteString("\n")
		if !single {
			fmt.Fprintf(&b, "**%s %s · %s（%s）**\n",
				tarotIndex(i), positionName(positions, i), r.Card.NameCN, r.Orientation())
			if i < len(positions) {
				// 引用行后必须留空行：CommonMark 的惰性续行会把紧跟的
				// 「牌意」「解读」两行并进引用块，整段被当成引文渲染。
				fmt.Fprintf(&b, "> %s\n\n", positions[i].Focus)
			}
		}
		fmt.Fprintf(&b, "**牌意**：%s\n", r.Meaning())
		fmt.Fprintf(&b, "**解读**：%s\n", r.Reading())
	}

	b.WriteString("\n---\n")
	b.WriteString(tarotFooter(readings))

	return strings.TrimRight(b.String(), "\n")
}

// formatTarotText 将塔罗结果格式化为纯文本，用于不支持 Markdown 的平台，
// 以及图片渲染失败时的兜底输出。
func formatTarotText(readings []TarotReading) string {
	positions := tarotPositionsFor(len(readings))
	single := len(readings) == 1

	var b strings.Builder
	b.WriteString(tarotTitle(readings) + "\n")

	for i, r := range readings {
		b.WriteString("\n")
		if !single {
			pos := positionName(positions, i)
			focus := ""
			if i < len(positions) {
				focus = positions[i].Focus
			}
			if focus != "" {
				fmt.Fprintf(&b, "%s %s · %s（%s）：%s\n",
					tarotIndex(i), pos, r.Card.NameCN, r.Orientation(), focus)
			} else {
				fmt.Fprintf(&b, "%s %s（%s）\n", tarotIndex(i), r.Card.NameCN, r.Orientation())
			}
		}
		fmt.Fprintf(&b, "牌意：%s\n", r.Meaning())
		fmt.Fprintf(&b, "解读：%s\n", r.Reading())
	}

	b.WriteString("\n")
	b.WriteString(tarotFooter(readings))

	return strings.TrimRight(b.String(), "\n")
}
