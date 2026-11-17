// envfacts.go — 环境事实的持久化载荷（provider 中立）。
//
// [Message] 是会话持久化的载体（session 把 []Message 序列化进库），因此某个
// 角色 content 的内部格式也属于持久化 schema，定义在这里而不是某个适配器里。
//
// 为什么要 JSON 而不是直接存某家的线格式：
//
//	历史一旦落库就长期存在，而端点是可以换的（DeepSeek ↔ Anthropic ↔ 其它
//	兼容网关）。DeepSeek 的 latest_reminder 要求单行 CSV、Anthropic 的中途
//	system 要求可读指令、兜底路径要求一段散文——同一份事实要能换三种表达。
//	存 CSV 就只能投递给 DeepSeek，存散文就只能兜底。
//
// 载荷只包含**回放安全**的框架事实：日期、星期、时区、平台、语言、地点。
// 用户昵称/ID 之类逐轮变化的信息不属于这里——它们本来就在 user 消息里。

package protocol

import (
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
)

// EnvFacts 可随历史回放的环境事实。
//
// 字段刻意都用字符串：这是持久化 schema，改动类型会让旧记录解不出来；而
// 这些值的唯一消费方式是"渲染成文本"，字符串就是它们的自然形态。
type EnvFacts struct {
	// Date 日期（YYYY-MM-DD）。
	Date string `json:"date"`
	// Weekday 星期，用 Language 对应的语言书写（如 "星期二"）。
	Weekday string `json:"weekday,omitempty"`
	// TimeZone 时区名（IANA，如 "Asia/Shanghai"）。
	TimeZone string `json:"timezone,omitempty"`
	// Offset UTC 偏移（如 "+08:00"）。
	Offset string `json:"offset,omitempty"`
	// Location 所在地（可为空——平台通常拿不到，不做推断）。
	Location string `json:"location,omitempty"`
	// Platform 平台标识（如 "qq"）。
	Platform string `json:"platform,omitempty"`
	// Language 用户语言的自称（如 "中文"）。
	Language string `json:"language,omitempty"`
}

// IsZero 判断载荷是否为空（空载荷不产生消息）。
func (f EnvFacts) IsZero() bool { return f == EnvFacts{} }

// MarshalEnvFacts 把事实序列化为消息 content。
//
// 走 jsonx（仓库唯一的 JSON 入口）：键顺序由结构体决定，同一份事实在任何
// 时候都得到同一串字节——这直接决定前缀缓存能否命中。
func MarshalEnvFacts(f EnvFacts) string {
	b, err := jsonx.Marshal(f)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseEnvFacts 解析消息 content。
//
// 返回 ok=false 表示这条消息的 content 不是本格式（例如外部工具写入的裸文本）。
// 调用方应当把它当作普通文本处理，而不是报错——历史里可能有任意内容。
func ParseEnvFacts(content string) (EnvFacts, bool) {
	content = strings.TrimSpace(content)
	if content == "" || content[0] != '{' {
		return EnvFacts{}, false
	}
	var f EnvFacts
	if err := jsonx.Unmarshal([]byte(content), &f); err != nil {
		return EnvFacts{}, false
	}
	if f.Date == "" {
		return EnvFacts{}, false
	}
	return f, true
}

// cleanField 把字段里的分隔符替换掉。
//
// latest_reminder 用逗号分隔 5 个字段，字段内出现逗号会被端点判为格式错误
// （400），因此投递前必须净化。
func cleanField(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), ",", " ")
}

// ReminderLine 渲染成 DeepSeek latest_reminder 的单行格式：
//
//	<date>,<weekday>,<location>,<platform>,<language>
//
// 字段数必须恒为 5（location 可以为空，但不能缺列）。
func (f EnvFacts) ReminderLine() string {
	return strings.Join([]string{
		cleanField(f.Date),
		cleanField(f.Weekday),
		cleanField(f.Location),
		cleanField(f.Platform),
		cleanField(f.Language),
	}, ",")
}

// Instruction 渲染成给 OpenAI developer / Anthropic 中途 system 用的可读指令。
//
// 这两个角色本身就是"应用级指令"，散文式表达比 CSV 更合适（模型对
// "当前日期：…" 的理解比对 5 列裸字段更稳）。字段顺序固定，保证逐轮字节一致。
func (f EnvFacts) Instruction() string {
	var b strings.Builder
	b.WriteString("<environment>\n")
	if f.Date != "" || f.Weekday != "" {
		b.WriteString("当前日期: ")
		b.WriteString(f.Date)
		if f.Weekday != "" {
			b.WriteString(" (")
			b.WriteString(f.Weekday)
			if f.TimeZone != "" {
				b.WriteString(", ")
				b.WriteString(f.TimeZone)
				if f.Offset != "" {
					b.WriteString(" ")
					b.WriteString(f.Offset)
				}
			}
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	if f.Location != "" {
		b.WriteString("所在地: " + f.Location + "\n")
	}
	if f.Platform != "" {
		b.WriteString("平台: " + f.Platform + "\n")
	}
	if f.Language != "" {
		b.WriteString("用户语言: " + f.Language + "\n")
	}
	b.WriteString("</environment>")
	return b.String()
}

// Prose 渲染成兜底路径用的散文（前置进 user 消息的形态）。
//
// 与 [Instruction] 的区别：兜底内容会混在用户发言里，因此不加 <environment>
// 标签（那会让模型以为存在结构化边界），直接给几行事实更自然。
func (f EnvFacts) Prose() string {
	ins := f.Instruction()
	ins = strings.TrimPrefix(ins, "<environment>\n")
	ins = strings.TrimSuffix(ins, "</environment>")
	return strings.TrimRight(ins, "\n")
}
