// skill.go — 技能注册的输入约束与默认参数补全。
//
// 与 discovery.go 一样是纯计算：不读写注册表，因此调用方（注册流程）与
// 校验逻辑可以分开演进。

package catalog

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// UserSkillNamePattern 用户技能名的合法字符约束（字母、数字、下划线、连字符）。
var UserSkillNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,62}$`)

// ApplyDefaultParamSchema 在技能未声明参数时补上默认的 {"query": string}。
func ApplyDefaultParamSchema(s *toolkit.Skill) {
	if len(s.Parameters.Properties) == 0 {
		s.Parameters = protocol.ToolParamSchema{
			Type: "object",
			Properties: map[string]protocol.ToolParamSchema{
				"query": {Type: "string", Description: "需要该技能处理的问题"},
			},
			Required: []string{"query"},
		}
	}
}

// SystemSkill 归一化系统级技能：未声明所有者时归入 OwnerSystem，
// 并在未声明参数时补上默认参数。
func SystemSkill(s toolkit.Skill) toolkit.Skill {
	if s.OwnerID == "" {
		s.OwnerID = toolkit.OwnerSystem
	}
	ApplyDefaultParamSchema(&s)
	return s
}

// UserSkill 归一化用户自定义技能：校验名称、补上用户前缀与所有者，
// 补默认参数，并把未显式启用的技能视为启用。
//
// 名称非法时返回错误；调用方负责后续的数量与 Prompt 长度上限检查。
func UserSkill(s toolkit.Skill, ownerID string) (toolkit.Skill, error) {
	name := strings.TrimPrefix(strings.TrimSpace(s.Name), toolkit.UserSkillPrefix)
	if !UserSkillNamePattern.MatchString(name) {
		return toolkit.Skill{}, fmt.Errorf("技能名称只能使用字母、数字、下划线或连字符，长度为 1–62")
	}
	s.OwnerID = ownerID
	s.Name = toolkit.UserSkillPrefix + name
	ApplyDefaultParamSchema(&s)
	if !s.Enabled {
		s.Enabled = true
	}
	return s, nil
}

// ToolFromSkill 把技能包装为一个可注册的占位动作。
//
// 动作的执行体由调用方注入（技能自身由技能注册表按所有者解析后执行），
// 这里只负责"技能以什么名字、什么描述、什么参数暴露给模型"。
func ToolFromSkill(s toolkit.Skill, run func(ctx context.Context, args map[string]any) (string, error)) toolkit.Tool {
	return toolkit.Tool{
		Name:        s.Name,
		Description: s.Description,
		Parameters:  s.Parameters,
		Execute:     run,
	}
}
