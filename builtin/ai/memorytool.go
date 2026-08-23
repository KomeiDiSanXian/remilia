// Package ai memorytool.go — 长期记忆工具（memory_add / memory_query / memory_forget）。
//
// 让 AI 在对话中主动读写长期记忆（默认用户级作用域，群聊可指定群级）：
//   - memory_add：写入一条事实（与自动抽取共用去重合并与上限淘汰）
//   - memory_query：按关键词/语义检索记忆并返回 Top-N
//   - memory_forget：精确删除一条事实（"忘掉这件事"）
//
// 依赖 memory_enabled 开启（memoryStore 非 nil），未开启时不注册这些工具。
package ai

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	memoryAddToolName    = "memory_add"
	memoryQueryToolName  = "memory_query"
	memoryForgetToolName = "memory_forget"

	// memoryToolMaxLimit memory_query 单次返回上限。
	memoryToolMaxLimit = 20
)

// buildMemoryTools 构建长期记忆工具集（general 类别，恒被选中）。
func (p *Plugin) buildMemoryTools() []Tool {
	return []Tool{
		{
			Name:        memoryAddToolName,
			Categories:  []string{CategoryGeneral},
			Description: "把一条关于用户或本群的事实写入长期记忆，供未来对话参考。适合用户说\"记住：…\"或你确认了重要偏好的场景；与自动记忆抽取共用同一存储",
			Parameters: ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"text":  {Type: "string", Description: "要记住的事实内容（简洁、确定的陈述句，如\"用户喜欢喝冰美式\"）"},
					"scope": {Type: "string", Description: "记忆作用域：user=仅对当前用户可见（默认）；group=本群公共记忆（仅群聊可用）", Enum: []string{"user", "group"}},
				},
				Required: []string{"text"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolSourceFromContext(ctx)
				if !ok || src.p.memory == nil {
					return "", errors.New("长期记忆功能未启用（memory_enabled=false）")
				}
				text, _ := args["text"].(string)
				text = strings.TrimSpace(text)
				if text == "" {
					return "", errors.New("记忆内容不能为空")
				}
				scope, err := memoryToolScope(src, args["scope"])
				if err != nil {
					return "", err
				}
				src.p.memory.Add(scope, text)
				count := len(src.p.memory.Facts(scope))
				label := "用户记忆"
				if scope == groupScope(src.chatID) {
					label = "本群记忆"
				}
				return fmt.Sprintf("已记住：%s（%s现有 %d 条）", text, label, count), nil
			},
		},
		{
			Name:        memoryQueryToolName,
			Categories:  []string{CategoryGeneral},
			Description: "检索长期记忆中与查询相关的事实（关键词+语义），返回 Top-N。适合用户问\"我上次说过…\"\"你还记得…\"或需要个性化回答时先查记忆",
			Parameters: ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"query": {Type: "string", Description: "查询内容，如\"咖啡偏好\"\"生日\"\"宠物\"，或用完整问题"},
					"limit": {Type: "integer", Description: "最多返回条数（默认 5，最大 20）"},
				},
				Required: []string{"query"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolSourceFromContext(ctx)
				if !ok || src.p.memory == nil {
					return "", errors.New("长期记忆功能未启用（memory_enabled=false）")
				}
				query, _ := args["query"].(string)
				query = strings.TrimSpace(query)
				if query == "" {
					return "", errors.New("查询内容不能为空")
				}
				limit := memoryToolLimit(args["limit"])
				var out []MemoryFact
				seen := make(map[string]bool)
				labels := make(map[string]string) // text → 来源标签
				if f := src.p.memory.Retrieve(ctx, userScope(src.userID), query, limit); len(f) > 0 {
					for _, x := range f {
						if !seen[x.Text] {
							seen[x.Text] = true
							out = append(out, x)
							labels[x.Text] = "我"
						}
					}
				}
				if src.isGroup {
					rem := limit
					if len(out) > limit {
						rem = 0
					} else {
						rem = limit - len(out)
					}
					if rem > 0 {
						if f := src.p.memory.Retrieve(ctx, groupScope(src.chatID), query, rem); len(f) > 0 {
							for _, x := range f {
								if !seen[x.Text] {
									seen[x.Text] = true
									out = append(out, x)
									labels[x.Text] = "本群"
								}
							}
						}
					}
				}
				if len(out) == 0 {
					return "记忆中没有找到相关内容", nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "找到 %d 条相关记忆：\n", len(out))
				for i, f := range out {
					scopeLabel := labels[f.Text]
					if scopeLabel == "" {
						scopeLabel = "我"
					}
					fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, scopeLabel, f.Text)
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        memoryForgetToolName,
			Categories:  []string{CategoryGeneral},
			Description: "删除一条长期记忆事实（精确文本匹配，用 memory_query 确认要删除的原文）。适合用户要求\"忘掉这件事\"",
			Parameters: ToolParamSchema{
				Type: "object",
				Properties: map[string]ToolParamSchema{
					"text":  {Type: "string", Description: "要删除的事实原文（与记忆中的文本一致，可用 memory_query 查到）"},
					"scope": {Type: "string", Description: "记忆作用域：user=用户记忆（默认）；group=本群记忆", Enum: []string{"user", "group"}},
				},
				Required: []string{"text"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolSourceFromContext(ctx)
				if !ok || src.p.memory == nil {
					return "", errors.New("长期记忆功能未启用（memory_enabled=false）")
				}
				text, _ := args["text"].(string)
				text = strings.TrimSpace(text)
				if text == "" {
					return "", errors.New("请指定要删除的记忆内容")
				}
				scope, err := memoryToolScope(src, args["scope"])
				if err != nil {
					return "", err
				}
				if src.p.memory.Remove(scope, text) {
					return "已删除该记忆", nil
				}
				return "未找到完全匹配的记忆（可用 memory_query 查询原文后重试）", nil
			},
		},
	}
}

// memoryToolScope 解析记忆工具的作用域参数。
func memoryToolScope(src toolSource, raw any) (string, error) {
	scope := "user"
	if s, ok := raw.(string); ok && s != "" {
		scope = strings.ToLower(strings.TrimSpace(s))
	}
	switch scope {
	case "user":
		return userScope(src.userID), nil
	case "group":
		if !src.isGroup {
			return "", errors.New("group 作用域仅群聊可用，当前会话不是群聊")
		}
		return groupScope(src.chatID), nil
	default:
		return "", fmt.Errorf("未知作用域 %q（仅支持 user/group）", scope)
	}
}

// memoryToolLimit 解析 memory_query 的 limit 参数（默认 5，上限 memoryToolMaxLimit）。
func memoryToolLimit(raw any) int {
	n := 5
	switch v := raw.(type) {
	case float64:
		n = int(v)
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			n = parsed
		}
	}
	if n <= 0 {
		n = 5
	}
	if n > memoryToolMaxLimit {
		n = memoryToolMaxLimit
	}
	return n
}
