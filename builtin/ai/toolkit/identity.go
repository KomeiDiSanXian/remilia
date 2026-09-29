package toolkit

import "strings"

// identity.go — 动作身份：把"工具叫什么"与"工具来自哪里"分开。
//
// 工具名（Name）是模型可见的函数名，受 API 字符与长度约束，且在单次请求里
// 必须全局唯一；但不同来源（内置、插件、命令、外部工具服务器）出现同名是
// 常态。身份用 {来源, 名称} 表达：名称仍作展示与协议字段，来源说明归属，
// 用于路由、去重、观测与冲突消解。
//
// 兼容：未声明来源的工具按内置（[SourceBuiltin]）处理，行为与改造前一致。

// ActionSource 标识动作/工具的来源命名空间。
type ActionSource string

const (
	// SourceBuiltin 内置动作（框架自带）。
	SourceBuiltin ActionSource = "builtin"
	// SourceCommand 由已注册命令自动发现包装的动作。
	SourceCommand ActionSource = "command"
	// SourceSkill 技能（Skill）动作。
	SourceSkill ActionSource = "skill"
	// SourceMCP 外部工具协议（MCP）来源前缀；具体服务器使用 "mcp:<server>"。
	SourceMCP ActionSource = "mcp"
)

// MCPSource 返回某个外部工具服务器对应的来源标识。
func MCPSource(server string) ActionSource {
	server = strings.TrimSpace(server)
	if server == "" {
		return SourceMCP
	}
	return ActionSource(string(SourceMCP) + ":" + server)
}

// ActionID 动作的稳定身份：来源 + 该来源内的名称。
type ActionID struct {
	Source ActionSource
	Name   string
}

// String 返回 "来源/名称" 形式（用于日志与去重键）。
func (id ActionID) String() string {
	if id.Source == "" {
		return string(SourceBuiltin) + "/" + id.Name
	}
	return string(id.Source) + "/" + id.Name
}

// ActionIDOf 派生工具的身份；未声明来源时视为内置。
func ActionIDOf(t Tool) ActionID {
	return ActionID{Source: t.EffectiveSource(), Name: t.Name}
}

// EffectiveSource 返回工具声明的来源，未声明时回退到内置。
func (t Tool) EffectiveSource() ActionSource {
	if t.Source == "" {
		return SourceBuiltin
	}
	return t.Source
}
