// discovery.go — 命令到动作的包装，以及"这条命令能否暴露给 AI"的判定。
//
// 这里只做纯计算：判定与构造都不读写插件状态，因此发现流程（何时扫描、扫描
// 结果落到哪里）留给调用方，本包不持有注册表。

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// Options 一次自动发现的输入。
type Options struct {
	// Allowlist 显式允许的命令工具名。非空时只发现名单内的命令
	// （用户显式意图优先于其它排除规则）。
	Allowlist []string
	// TriggerCmd 触发命令前缀（如 "/ai"）；对应命令会被排除，避免 AI 自我递归调用。
	TriggerCmd string
	// ExcludePlugins 已提供结构化动作（ToolProvider / SkillProvider）的插件名，
	// 它们的命令默认不再自动发现；命中 Allowlist 时例外。
	ExcludePlugins map[string]struct{}
}

// Sink 发现结果的落点（由调用方的目录与注册表实现）。
//
// 拆成 Has / Add 两步而不是让本包返回列表：跳过"已被显式注册占用的名字"
// 依赖逐步写入后的注册表状态，返回列表会改变同名命令的取舍。
type Sink interface {
	// Has 报告该动作名是否已存在。
	Has(name string) bool
	// Add 登记一个自动发现的命令动作，并记下它对应的命令模式。
	Add(tool toolkit.Tool, commandPattern string)
}

// Discover 扫描已注册的命令，把**无权限**的那些包装为动作交给 sink。
//
// 安全设计：仅自动发现不需要任何权限的命令。需要权限的命令不会被 AI 自动
// 发现，防止通过 AI 绕过权限检查；这类命令应由插件在自己的 Setup 中显式
// 注册工具并自行校验身份。
func Discover(reader engine.Reader, opts Options, sink Sink) {
	if reader == nil {
		return
	}

	hasAllowlist := len(opts.Allowlist) > 0
	allowSet := make(map[string]struct{}, len(opts.Allowlist))
	for _, name := range opts.Allowlist {
		allowSet[name] = struct{}{}
	}

	triggerName := ""
	if opts.TriggerCmd != "" {
		triggerName = strings.TrimLeft(opts.TriggerCmd, "/!$#")
	}

	for _, cmd := range reader.GetAllCommands() {
		if cmd.Definition != nil && cmd.Definition.Hidden {
			continue
		}
		if !IsCommandSafeForAI(cmd) {
			continue
		}
		// 已提供显式 AI 工具的插件，其命令默认不再自动发现（allowlist 例外）。
		if cmd.Plugin != "" {
			if _, excluded := opts.ExcludePlugins[cmd.Plugin]; excluded {
				if !hasAllowlist {
					continue
				}
				name := strings.TrimLeft(cmd.Command, "/!$#")
				name = strings.ReplaceAll(name, " ", "_")
				if _, allowed := allowSet[name]; !allowed {
					continue
				}
			}
		}
		name := strings.TrimLeft(cmd.Command, "/!$#")
		name = strings.ReplaceAll(name, " ", "_")
		if triggerName != "" && name == triggerName {
			continue
		}
		if hasAllowlist {
			if _, ok := allowSet[name]; !ok {
				continue
			}
		}
		// 已被显式注册（RegisterToolProvider / RegisterSkill）占用的名称
		// 不再自动发现为命令工具，避免覆盖或污染命令映射。
		if sink.Has(name) {
			continue
		}
		tool := ToolFromCommand(cmd)
		if tool != nil {
			sink.Add(*tool, cmd.Command)
		}
	}
}

// IsCommandSafeForAI 判断命令是否能安全地暴露给 AI。
//
// 安全条件（全部满足）：
//  1. 命令无权限要求（Permissions 为空）
//  2. 命令定义中也无权限要求（Definition.Permissions 为空）
//  3. 不是 AI 自身命令
//  4. 命令名称能构成合法的工具名（仅含 a-zA-Z0-9_-）
//
// 不满足任一条件 → AI 不可调用该命令。
func IsCommandSafeForAI(cmd engine.CommandInfo) bool {
	name := strings.TrimLeft(cmd.Command, "/!$#")
	name = strings.ReplaceAll(name, " ", "_")
	if name == "" || name == "ai" {
		return false
	}
	if !toolkit.ValidToolName(name) {
		return false
	}
	if len(cmd.Permissions) > 0 {
		return false
	}
	if cmd.Definition != nil && len(cmd.Definition.Permissions) > 0 {
		return false
	}
	return true
}

// DeriveToolCategory 从命令信息中推断动作分类。
// 优先使用命令信息中的 Category，其次使用插件名，最后使用命令定义中的
// Category，兜底为通用类别。
func DeriveToolCategory(cmd engine.CommandInfo) string {
	if cmd.Category != "" {
		return cmd.Category
	}
	if cmd.Plugin != "" && cmd.Plugin != "global" {
		return cmd.Plugin
	}
	if cmd.Definition != nil && cmd.Definition.Category != "" {
		return cmd.Definition.Category
	}
	return toolkit.CategoryGeneral
}

// ToolFromCommand 将命令信息转换为 LLM 工具。
//
// 调用方应确保已通过 IsCommandSafeForAI 前置检查。
func ToolFromCommand(cmd engine.CommandInfo) *toolkit.Tool {
	name := strings.TrimLeft(cmd.Command, "/!$#")
	if name == "" {
		return nil
	}
	name = strings.ReplaceAll(name, " ", "_")
	name = toolkit.SanitizeToolName(name)

	desc := cmd.Description
	if desc == "" {
		desc = fmt.Sprintf("执行命令 %s", cmd.Command)
	}

	category := DeriveToolCategory(cmd)
	return &toolkit.Tool{
		Name:        name,
		Categories:  []string{category},
		Description: desc,
		Parameters: protocol.ToolParamSchema{
			Type: "object",
			Properties: map[string]protocol.ToolParamSchema{
				"arguments": {
					Type:        "string",
					Description: "传递给命令的原始参数；无参数命令可省略",
				},
			},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("[命令 %s 已触发]", cmd.Command), nil
		},
	}
}
