// memorytool.go — 长期记忆的写入与删除动作（memory_add / memory_forget）。
//
// 让 AI 在对话中主动读写长期记忆（默认用户级作用域，群聊可指定群级）：
//   - memory_add：写入一条事实（与自动抽取共用去重合并与上限淘汰）
//   - memory_forget：精确删除一条事实（"忘掉这件事"）
//
// 检索不是动作：它由上下文管线按本轮用户消息自动完成并注入，因此模型不再拥有
// 独立的查询动作。
//
// 作用域以"种类 + 标识"表达（user:<id> / group:<id> 的键格式由装配侧决定），
// 依赖装配侧注入的 MemoryAccess；未注入（memory_enabled 未开启）时动作返回"未启用"。
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

const (
	// MemoryAddToolName 写入长期记忆的动作名。
	MemoryAddToolName = "memory_add"
	// MemoryForgetToolName 删除长期记忆的动作名。
	MemoryForgetToolName = "memory_forget"
)

// 记忆作用域种类（与动作参数 scope 的取值一致）。
const (
	memoryScopeUser  = "user"
	memoryScopeGroup = "group"
)

// BuildMemoryTools 构建长期记忆动作集（general 类别，恒被选中）。
func BuildMemoryTools() []toolkit.Tool {
	return []toolkit.Tool{
		{
			Name:        MemoryAddToolName,
			Categories:  []string{toolkit.CategoryGeneral},
			Description: "把一条关于用户或本群的事实写入长期记忆，供未来对话参考。适合用户说\"记住：…\"或你确认了重要偏好的场景；与自动记忆抽取共用同一存储",
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"text":  {Type: "string", Description: "要记住的事实内容（简洁、确定的陈述句，如\"用户喜欢喝冰美式\"）"},
					"scope": {Type: "string", Description: "记忆作用域：user=仅对当前用户可见（默认）；group=本群公共记忆（仅群聊可用）", Enum: []string{memoryScopeUser, memoryScopeGroup}},
				},
				Required: []string{"text"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolkit.ToolSourceFromContext(ctx)
				caps, capsOK := capabilitiesFromContext(ctx)
				if !ok || !capsOK || caps.Memory == nil {
					return "", errors.New("长期记忆功能未启用（memory_enabled=false）")
				}
				text, _ := args["text"].(string)
				text = strings.TrimSpace(text)
				if text == "" {
					return "", errors.New("记忆内容不能为空")
				}
				kind, target, err := memoryToolScope(src, args["scope"])
				if err != nil {
					return "", err
				}
				caps.Memory.Add(kind, target, text)
				count := caps.Memory.Count(kind, target)
				label := "用户记忆"
				if kind == memoryScopeGroup {
					label = "本群记忆"
				}
				return fmt.Sprintf("已记住：%s（%s现有 %d 条）", text, label, count), nil
			},
		},
		{
			Name:        MemoryForgetToolName,
			Categories:  []string{toolkit.CategoryGeneral},
			Description: "删除一条长期记忆事实（精确文本匹配，原文取自注入的长期记忆条目）。适合用户要求\"忘掉这件事\"",
			Parameters: protocol.ToolParamSchema{
				Type: "object",
				Properties: map[string]protocol.ToolParamSchema{
					"text":  {Type: "string", Description: "要删除的事实原文（与注入的长期记忆条目文本完全一致；不确定时可请用户用 /ai memory 查看）"},
					"scope": {Type: "string", Description: "记忆作用域：user=用户记忆（默认）；group=本群记忆", Enum: []string{memoryScopeUser, memoryScopeGroup}},
				},
				Required: []string{"text"},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				src, ok := toolkit.ToolSourceFromContext(ctx)
				caps, capsOK := capabilitiesFromContext(ctx)
				if !ok || !capsOK || caps.Memory == nil {
					return "", errors.New("长期记忆功能未启用（memory_enabled=false）")
				}
				text, _ := args["text"].(string)
				text = strings.TrimSpace(text)
				if text == "" {
					return "", errors.New("请指定要删除的记忆内容")
				}
				kind, target, err := memoryToolScope(src, args["scope"])
				if err != nil {
					return "", err
				}
				if caps.Memory.Remove(kind, target, text) {
					return "已删除该记忆", nil
				}
				return "未找到完全匹配的记忆（原文需与注入的长期记忆条目一致；可请用户用 /ai memory 查看）", nil
			},
		},
	}
}

// memoryToolScope 解析记忆动作的作用域参数，返回作用域种类与其标识。
func memoryToolScope(src toolkit.ToolSource, raw any) (kind string, target string, err error) {
	scope := memoryScopeUser
	if s, ok := raw.(string); ok && s != "" {
		scope = strings.ToLower(strings.TrimSpace(s))
	}
	switch scope {
	case memoryScopeUser:
		return memoryScopeUser, src.UserID, nil
	case memoryScopeGroup:
		if !src.IsGroup {
			return "", "", errors.New("group 作用域仅群聊可用，当前会话不是群聊")
		}
		return memoryScopeGroup, src.ChatID, nil
	default:
		return "", "", fmt.Errorf("未知作用域 %q（仅支持 user/group）", scope)
	}
}
