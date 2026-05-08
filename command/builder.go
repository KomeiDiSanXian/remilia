package command

// builder.go — Definition 的 Fluent Builder
//
// 用法：
//
//	def := NewDef("search").
//	    Description("搜索内容").
//	    Arg("keyword", "搜索关键词", true).
//	    Flag("page", "p", "页码", ArgTypeInt).
//	    Example("/search hello").
//	    Build()
//
//	ctx.OnCommandDef("", "/search", def)

// DefBuilder 是 [Definition] 的 Fluent Builder。
//
// 零值可用：直接 &DefBuilder{} 后链式调用，最后 .Build() 获取结果。
type DefBuilder struct {
	def *Definition
}

// NewDef 创建一个命令定义构建器。
//
// name 是命令名（不含触发前缀），如 "search"、"help"。
//
//	// 创建最小定义后直接 Build
//	NewDef("ping").Build()
//
//	// 完整链式构建
//	NewDef("search").Description("搜索").Arg("keyword", "关键词", true).Build()
func NewDef(name string) *DefBuilder {
	return &DefBuilder{def: &Definition{Name: name}}
}

// Description 设置命令描述。
func (b *DefBuilder) Description(d string) *DefBuilder {
	b.def.Description = d
	return b
}

// Usage 设置命令用法说明。
func (b *DefBuilder) Usage(u string) *DefBuilder {
	b.def.Usage = u
	return b
}

// Category 设置命令分类。
func (b *DefBuilder) Category(c string) *DefBuilder {
	b.def.Category = c
	return b
}

// Arg 添加一个字符串类型的位置参数。
//
// name 参数名，desc 描述，required 是否必需。
//
//	NewDef("ban").Arg("user", "要封禁的用户", true).Build()
func (b *DefBuilder) Arg(name, desc string, required bool) *DefBuilder {
	return b.ArgWithType(name, desc, required, ArgTypeString)
}

// ArgWithType 添加一个指定类型的位歨参数。
//
//	NewDef("add").
//	    ArgWithType("a", "第一个数", true, ArgTypeInt).
//	    ArgWithType("b", "第二个数", true, ArgTypeInt).
//	    Build()
func (b *DefBuilder) ArgWithType(name, desc string, required bool, argType ArgType) *DefBuilder {
	b.def.Arguments = append(b.def.Arguments, &Argument{
		Name: name, Description: desc, Required: required, Type: argType,
	})
	return b
}

// Flag 添加一个标志参数。
//
// name 长标志名（如 "page"），shortName 短标志名（如 "p"，可空），
// desc 描述，flagType 类型。
//
//	NewDef("search").
//	    Flag("page", "p", "页码", ArgTypeInt).
//	    Flag("verbose", "v", "详细输出", ArgTypeBool).
//	    Build()
func (b *DefBuilder) Flag(name, shortName, desc string, flagType ArgType) *DefBuilder {
	b.def.Flags = append(b.def.Flags, &Flag{
		Name: name, ShortName: shortName, Description: desc, Type: flagType,
	})
	return b
}

// Example 添加一个使用示例。
func (b *DefBuilder) Example(ex string) *DefBuilder {
	b.def.Examples = append(b.def.Examples, ex)
	return b
}

// Alias 添加命令别名。
//
//	NewDef("help").Alias("h", "?").Build()
func (b *DefBuilder) Alias(aliases ...string) *DefBuilder {
	b.def.Aliases = append(b.def.Aliases, aliases...)
	return b
}

// SubCommand 添加一个子命令。
//
//	NewDef("config").
//	    SubCommand(NewDef("get").Description("获取配置").Build()).
//	    SubCommand(NewDef("set").Description("设置配置").Arg("key", "键", true).Arg("value", "值", true).Build()).
//	    Build()
func (b *DefBuilder) SubCommand(sub *Definition) *DefBuilder {
	b.def.SubCommands = append(b.def.SubCommands, sub)
	return b
}

// Hidden 设置命令是否在帮助列表中隐藏。
func (b *DefBuilder) Hidden() *DefBuilder {
	b.def.Hidden = true
	return b
}

// Permission 添加所需权限。
func (b *DefBuilder) Permission(p ...string) *DefBuilder {
	b.def.Permissions = append(b.def.Permissions, p...)
	return b
}

// Handler 设置命令处理函数。
//
// 注意：command.Handler 签名为 func(any)，在运行时传入的是 *Context。
//
//	NewDef("ping").Handler(func(ctx any) {
//	    // 需要类型断言为 *corectx.Context
//	}).Build()
func (b *DefBuilder) Handler(h func(any)) *DefBuilder {
	b.def.Handler = h
	return b
}

// Build 返回构建完成的 [Definition]。
//
// 构建器在 Build 后可继续链式调用，不会影响已返回的 Definition。
func (b *DefBuilder) Build() *Definition {
	return b.def
}
