package fortune

import (
	"fmt"
	"math/rand"
	"strings"
)

// Level 御神签吉凶等级。
//
// 浅草寺观音签（元三大師百籤）只有 7 档，签面自带说明列出的次序为
// 凶・吉・末吉・半吉・小吉・末小吉・大吉。本枚举按由吉到凶排列。
// 注意：真实签面没有「中吉」，也没有「大凶」。
type Level int

const (
	Daikichi    Level = iota // 大吉
	Sueshokichi              // 末小吉
	Shokichi                 // 小吉
	Hankichi                 // 半吉
	Suekichi                 // 末吉
	Kichi                    // 吉
	Kyo                      // 凶
)

// String 返回吉凶等级的中文名。
func (f Level) String() string {
	switch f {
	case Daikichi:
		return "大吉"
	case Sueshokichi:
		return "末小吉"
	case Shokichi:
		return "小吉"
	case Hankichi:
		return "半吉"
	case Suekichi:
		return "末吉"
	case Kichi:
		return "吉"
	case Kyo:
		return "凶"
	}
	return "?"
}

// OmikujiItem 签面上的单个运势分类项目，如「愿望：能实现」。
type OmikujiItem struct {
	Name  string // 中文项目名
	Value string // 中文内容
}

// OmikujiSlip 一番御神签的完整数据。
//
// 签文数据见 omikuji_zh.go：吉凶、漢詩与分类运势取自浅草寺观音签的
// 中文解签数据集，已与 assets/omikuji 的签纸扫描件交叉核对 —— 吉凶取值
// 全部落在真实签面的七档之内，抽查番号的吉凶与漢詩均与签纸一致。
type OmikujiSlip struct {
	Number int           // 签号 1-100
	Level  Level         // 吉凶
	Poem   [4]string     // 漢詩四句（简体）
	PoemZH string        // 漢詩中文译意
	Items  []OmikujiItem // 分类运势
}

// drawOmikuji 抽取签号：number 在 1-100 之间时原样返回，否则随机抽取一张。
func drawOmikuji(number int) int {
	if number >= 1 && number <= 100 {
		return number
	}
	return rand.Intn(100) + 1
}

// omikujiSlip 返回指定签号的签文数据，签号越界时返回 nil。
func omikujiSlip(number int) *OmikujiSlip {
	if number < 1 || number > 100 {
		return nil
	}
	return &omikujiSlips[number-1]
}

// 解签末尾的固定说明。番号已由标题行给出，这里只补充签的来历、
// 凶签在浅草寺的习俗，以及娱乐用途提醒，避免被当成实际指引。
const (
	// omikujiSource 出处说明。
	omikujiSource = "📜 出自浅草寺观音签（元三大師百籤）：一番至百番，" +
		"吉凶分大吉·吉·末吉·半吉·小吉·末小吉·凶七档。"

	// omikujiKyoHint 凶签专属提示，仅吉凶为凶时附加。
	omikujiKyoHint = "🙏 抽到凶签，浅草寺的习俗是将它系在寺内，以求结缘转运。"

	// omikujiDisclaimer 免责声明。
	omikujiDisclaimer = "🎲 仅供娱乐，请勿据此做重要决定。"
)

// omikujiFooter 返回解签末尾的固定说明：出处、凶签提示（仅凶签）与免责声明。
func omikujiFooter(level Level) string {
	lines := []string{omikujiSource}
	if level == Kyo {
		lines = append(lines, omikujiKyoHint)
	}
	lines = append(lines, omikujiDisclaimer)
	return strings.Join(lines, "\n")
}

// formatOmikujiMD 把签文格式化为 Markdown 说明块，与签纸图片同条发送。
func formatOmikujiMD(s *OmikujiSlip) string {
	var b strings.Builder

	fmt.Fprintf(&b, "**🏮 浅草寺御神签 · 第 %d 番 · %s**\n\n", s.Number, s.Level)
	b.WriteString("**汉诗**\n")
	fmt.Fprintf(&b, "%s　%s\n%s　%s\n\n", s.Poem[0], s.Poem[1], s.Poem[2], s.Poem[3])

	if s.PoemZH != "" {
		b.WriteString("**签意**\n")
		b.WriteString(s.PoemZH)
		b.WriteString("\n\n")
	}

	if len(s.Items) > 0 {
		b.WriteString("**各项运势**\n")
		b.WriteString(formatOmikujiItems(s.Items))
	}

	b.WriteString("\n---\n")
	b.WriteString(omikujiFooter(s.Level))

	return strings.TrimRight(b.String(), "\n")
}

// formatOmikujiItems 把分类运势渲染为 Markdown 表格，每行最多 4 列，
// 避免项目较多时在窄屏上被挤压。
func formatOmikujiItems(items []OmikujiItem) string {
	const perRow = 4

	var b strings.Builder
	for i := 0; i < len(items); i += perRow {
		chunk := items[i:min(i+perRow, len(items))]

		b.WriteString("|")
		for _, it := range chunk {
			fmt.Fprintf(&b, " %s |", mdCell(it.Name))
		}
		b.WriteString("\n|")
		for range chunk {
			b.WriteString(" :---: |")
		}
		b.WriteString("\n|")
		for _, it := range chunk {
			fmt.Fprintf(&b, " %s |", mdCell(it.Value))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// formatOmikujiText 把签文格式化为纯文本说明块，平台不支持 Markdown 时降级使用。
func formatOmikujiText(s *OmikujiSlip) string {
	var b strings.Builder

	fmt.Fprintf(&b, "🏮 浅草寺御神签 · 第 %d 番 · %s\n\n", s.Number, s.Level)
	b.WriteString("汉诗\n")
	fmt.Fprintf(&b, "%s　%s\n%s　%s\n\n", s.Poem[0], s.Poem[1], s.Poem[2], s.Poem[3])

	if s.PoemZH != "" {
		b.WriteString("签意\n")
		b.WriteString(s.PoemZH)
		b.WriteString("\n\n")
	}

	for _, it := range s.Items {
		fmt.Fprintf(&b, "%s：%s\n", it.Name, it.Value)
	}

	b.WriteString("\n")
	b.WriteString(omikujiFooter(s.Level))

	return strings.TrimRight(b.String(), "\n")
}

// mdCell 转义 Markdown 表格单元格里会破坏排版的字符。
func mdCell(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ").Replace(s)
}
