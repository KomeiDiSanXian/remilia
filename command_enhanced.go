package remilia

import (
	"fmt"
	"strconv"
	"strings"
)

// CommandDefinition 命令定义
//
// 支持命令树结构、参数验证、子命令等高级功能。
//
// 示例：
//
//	cmd := &CommandDefinition{
//	    Name: "admin",
//	    Description: "管理员命令",
//	    SubCommands: []*CommandDefinition{
//	        {
//	            Name: "user",
//	            SubCommands: []*CommandDefinition{
//	                {Name: "list", Description: "列出用户"},
//	                {Name: "add", Description: "添加用户"},
//	            },
//	        },
//	    },
//	}
type CommandDefinition struct {
	// 基本信息
	Name        string   // 命令名称
	Aliases     []string // 别名
	Description string   // 描述
	Usage       string   // 使用方法（可选，用于覆盖自动生成）

	// 参数定义
	Arguments []*Argument // 位置参数
	Flags     []*Flag     // 命名参数

	// 子命令
	SubCommands []*CommandDefinition

	// 验证和处理
	Validator func(*ParsedCommand) error // 自定义验证函数
	Handler   Handler                    // 命令处理器
}

// Argument 位置参数定义
type Argument struct {
	Name        string      // 参数名称
	Description string      // 描述
	Type        ArgType     // 类型
	Required    bool        // 是否必需
	Default     interface{} // 默认值
	Validator   func(string) error
}

// Flag 命名参数定义
type Flag struct {
	Name        string      // 长名称（--name）
	ShortName   string      // 短名称（-n）
	Description string      // 描述
	Type        ArgType     // 类型
	Required    bool        // 是否必需
	Default     interface{} // 默认值
	Validator   func(string) error
}

// ArgType 参数类型
type ArgType int

const (
	ArgTypeString ArgType = iota
	ArgTypeInt
	ArgTypeBool
	ArgTypeFloat
	ArgTypeStringSlice // 多个值，如: --tags tag1 tag2 tag3
)

// ParsedCommand 解析后的命令
type ParsedCommand struct {
	// 原始数据
	Raw string

	// 命令路径（如 ["admin", "user", "list"]）
	CommandPath []string

	// 匹配的定义
	Definition *CommandDefinition

	// 解析的参数
	Arguments map[string]interface{} // 位置参数（按名称）
	Flags     map[string]interface{} // 命名参数（按名称）

	// 原始参数（用于获取未定义的参数）
	rawArgs *CommandArgs
}

// CommandParser 增强版命令解析器
type CommandParser struct {
	rootCommands []*CommandDefinition
	prefix       string // 命令前缀（如 "/"）
}

// NewCommandParser 创建命令解析器
func NewCommandParser(prefix string) *CommandParser {
	return &CommandParser{
		rootCommands: make([]*CommandDefinition, 0),
		prefix:       prefix,
	}
}

// Register 注册命令定义
func (p *CommandParser) Register(cmd *CommandDefinition) {
	p.rootCommands = append(p.rootCommands, cmd)
}

// Parse 解析命令
func (p *CommandParser) Parse(input string) (*ParsedCommand, error) {
	// 使用原有的基础解析器
	rawArgs, err := parseCommandRaw(input)
	if err != nil {
		return nil, err
	}

	// 查找匹配的命令定义
	cmdPath, def, remainingTokens := p.matchCommand(rawArgs)
	if def == nil {
		return nil, fmt.Errorf("unknown command: %s", rawArgs.Command)
	}

	parsed := &ParsedCommand{
		Raw:         input,
		CommandPath: cmdPath,
		Definition:  def,
		Arguments:   make(map[string]interface{}),
		Flags:       make(map[string]interface{}),
		rawArgs:     rawArgs,
	}

	// 解析位置参数
	if err := p.parseArguments(parsed, remainingTokens, def); err != nil {
		return nil, err
	}

	// 解析命名参数
	if err := p.parseFlags(parsed, rawArgs, def); err != nil {
		return nil, err
	}

	// 运行自定义验证
	if def.Validator != nil {
		if err := def.Validator(parsed); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	return parsed, nil
}

// matchCommand 匹配命令定义（支持子命令）
func (p *CommandParser) matchCommand(args *CommandArgs) ([]string, *CommandDefinition, []string) {
	// 移除命令前缀
	cmdName := strings.TrimPrefix(args.Command, p.prefix)
	path := []string{cmdName}

	// 在根命令中查找
	var currentDef *CommandDefinition
	for _, cmd := range p.rootCommands {
		if cmd.Name == cmdName || contains(cmd.Aliases, cmdName) {
			currentDef = cmd
			break
		}
	}

	if currentDef == nil {
		return nil, nil, nil
	}

	// 查找子命令
	remainingArgs := args.Positional
	argIndex := 0

	for argIndex < len(remainingArgs) && len(currentDef.SubCommands) > 0 {
		subCmdName := remainingArgs[argIndex]
		matched := false

		for _, subCmd := range currentDef.SubCommands {
			if subCmd.Name == subCmdName || contains(subCmd.Aliases, subCmdName) {
				path = append(path, subCmdName)
				currentDef = subCmd
				argIndex++
				matched = true
				break
			}
		}

		if !matched {
			break
		}
	}

	return path, currentDef, remainingArgs[argIndex:]
}

// parseArguments 解析位置参数
func (p *CommandParser) parseArguments(parsed *ParsedCommand, tokens []string, def *CommandDefinition) error {
	for i, argDef := range def.Arguments {
		var value interface{}
		var err error

		if i < len(tokens) {
			// 有值，解析
			value, err = parseValue(tokens[i], argDef.Type)
			if err != nil {
				return fmt.Errorf("argument %s: %w", argDef.Name, err)
			}

			// 自定义验证
			if argDef.Validator != nil {
				if err := argDef.Validator(tokens[i]); err != nil {
					return fmt.Errorf("argument %s validation failed: %w", argDef.Name, err)
				}
			}
		} else {
			// 没有值
			if argDef.Required {
				return fmt.Errorf("required argument %s is missing", argDef.Name)
			}
			value = argDef.Default
		}

		parsed.Arguments[argDef.Name] = value
	}

	return nil
}

// parseFlags 解析命名参数
func (p *CommandParser) parseFlags(parsed *ParsedCommand, rawArgs *CommandArgs, def *CommandDefinition) error {
	// 处理定义的 flags
	for _, flagDef := range def.Flags {
		var rawValue string
		var found bool

		// 查找 flag 值（支持长名称和短名称）
		if rawValue, found = rawArgs.Flags[flagDef.Name]; !found && flagDef.ShortName != "" {
			rawValue, found = rawArgs.Flags[flagDef.ShortName]
		}

		if found {
			// 解析值
			value, err := parseValue(rawValue, flagDef.Type)
			if err != nil {
				return fmt.Errorf("flag --%s: %w", flagDef.Name, err)
			}

			// 自定义验证
			if flagDef.Validator != nil {
				if err := flagDef.Validator(rawValue); err != nil {
					return fmt.Errorf("flag --%s validation failed: %w", flagDef.Name, err)
				}
			}

			parsed.Flags[flagDef.Name] = value
		} else {
			// 没有提供值
			if flagDef.Required {
				return fmt.Errorf("required flag --%s is missing", flagDef.Name)
			}
			parsed.Flags[flagDef.Name] = flagDef.Default
		}
	}

	return nil
}

// parseValue 解析值为指定类型
func parseValue(s string, t ArgType) (interface{}, error) {
	switch t {
	case ArgTypeString:
		return s, nil
	case ArgTypeInt:
		return strconv.Atoi(s)
	case ArgTypeBool:
		if s == "true" || s == "1" || s == "yes" || s == "on" {
			return true, nil
		}
		if s == "false" || s == "0" || s == "no" || s == "off" {
			return false, nil
		}
		return false, fmt.Errorf("invalid boolean value: %s", s)
	case ArgTypeFloat:
		return strconv.ParseFloat(s, 64)
	default:
		return s, nil
	}
}

// parseCommandRaw 基础命令解析（复用现有逻辑）
func parseCommandRaw(input string) (*CommandArgs, error) {
	args := &CommandArgs{
		Raw:        input,
		Flags:      make(map[string]string),
		Positional: make([]string, 0),
	}

	tokens, err := tokenize(input)
	if err != nil {
		return nil, fmt.Errorf("tokenize error: %w", err)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("no tokens found")
	}

	args.Command = tokens[0]
	tokens = tokens[1:]

	i := 0
	for i < len(tokens) {
		token := tokens[i]

		if strings.HasPrefix(token, "--") {
			key := strings.TrimPrefix(token, "--")
			if key == "" {
				return nil, fmt.Errorf("invalid flag: %s", token)
			}

			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				args.Flags[key] = tokens[i+1]
				i += 2
			} else {
				args.Flags[key] = "true"
				i++
			}
		} else if strings.HasPrefix(token, "-") && len(token) == 2 {
			key := strings.TrimPrefix(token, "-")
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				args.Flags[key] = tokens[i+1]
				i += 2
			} else {
				args.Flags[key] = "true"
				i++
			}
		} else {
			args.Positional = append(args.Positional, token)
			i++
		}
	}

	return args, nil
}

// GetString 获取字符串类型的参数
func (p *ParsedCommand) GetString(name string) string {
	if val, ok := p.Arguments[name]; ok && val != nil {
		return val.(string)
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		return val.(string)
	}
	return ""
}

// GetInt 获取整数类型的参数
func (p *ParsedCommand) GetInt(name string) int {
	if val, ok := p.Arguments[name]; ok && val != nil {
		return val.(int)
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		return val.(int)
	}
	return 0
}

// GetBool 获取布尔类型的参数
func (p *ParsedCommand) GetBool(name string) bool {
	if val, ok := p.Arguments[name]; ok && val != nil {
		return val.(bool)
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		return val.(bool)
	}
	return false
}

// GetFloat 获取浮点数类型的参数
func (p *ParsedCommand) GetFloat(name string) float64 {
	if val, ok := p.Arguments[name]; ok && val != nil {
		return val.(float64)
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		return val.(float64)
	}
	return 0
}

// GenerateHelp 生成帮助信息
func (p *CommandParser) GenerateHelp(cmdPath ...string) string {
	var sb strings.Builder

	if len(cmdPath) == 0 {
		// 生成所有根命令的帮助
		sb.WriteString("Available commands:\n\n")
		for _, cmd := range p.rootCommands {
			p.writeCommandHelp(&sb, cmd, 0)
		}
	} else {
		// 生成指定命令的帮助
		def := p.findCommand(cmdPath)
		if def == nil {
			return fmt.Sprintf("Command not found: %s", strings.Join(cmdPath, " "))
		}
		p.writeDetailedHelp(&sb, def, cmdPath)
	}

	return sb.String()
}

// writeCommandHelp 写入命令简要帮助
func (p *CommandParser) writeCommandHelp(sb *strings.Builder, cmd *CommandDefinition, indent int) {
	indentStr := strings.Repeat("  ", indent)
	sb.WriteString(fmt.Sprintf("%s%s%s", indentStr, p.prefix, cmd.Name))

	if len(cmd.Aliases) > 0 {
		sb.WriteString(fmt.Sprintf(" (aliases: %s)", strings.Join(cmd.Aliases, ", ")))
	}

	if cmd.Description != "" {
		sb.WriteString(fmt.Sprintf("\n%s  %s", indentStr, cmd.Description))
	}

	sb.WriteString("\n")

	// 递归写入子命令
	for _, subCmd := range cmd.SubCommands {
		p.writeCommandHelp(sb, subCmd, indent+1)
	}
}

// writeDetailedHelp 写入详细帮助
func (p *CommandParser) writeDetailedHelp(sb *strings.Builder, cmd *CommandDefinition, path []string) {
	sb.WriteString(fmt.Sprintf("Command: %s%s\n", p.prefix, strings.Join(path, " ")))

	if cmd.Description != "" {
		sb.WriteString(fmt.Sprintf("\n%s\n", cmd.Description))
	}

	// Usage
	sb.WriteString("\nUsage:\n")
	if cmd.Usage != "" {
		sb.WriteString(fmt.Sprintf("  %s\n", cmd.Usage))
	} else {
		usage := fmt.Sprintf("  %s%s", p.prefix, strings.Join(path, " "))
		for _, arg := range cmd.Arguments {
			if arg.Required {
				usage += fmt.Sprintf(" <%s>", arg.Name)
			} else {
				usage += fmt.Sprintf(" [%s]", arg.Name)
			}
		}
		if len(cmd.Flags) > 0 {
			usage += " [options]"
		}
		sb.WriteString(usage + "\n")
	}

	// Arguments
	if len(cmd.Arguments) > 0 {
		sb.WriteString("\nArguments:\n")
		for _, arg := range cmd.Arguments {
			req := ""
			if arg.Required {
				req = " (required)"
			}
			sb.WriteString(fmt.Sprintf("  %s%s - %s\n", arg.Name, req, arg.Description))
		}
	}

	// Flags
	if len(cmd.Flags) > 0 {
		sb.WriteString("\nOptions:\n")
		for _, flag := range cmd.Flags {
			flagStr := fmt.Sprintf("  --%s", flag.Name)
			if flag.ShortName != "" {
				flagStr += fmt.Sprintf(", -%s", flag.ShortName)
			}
			req := ""
			if flag.Required {
				req = " (required)"
			}
			sb.WriteString(fmt.Sprintf("%s%s - %s\n", flagStr, req, flag.Description))
		}
	}

	// Sub-commands
	if len(cmd.SubCommands) > 0 {
		sb.WriteString("\nSub-commands:\n")
		for _, subCmd := range cmd.SubCommands {
			sb.WriteString(fmt.Sprintf("  %s - %s\n", subCmd.Name, subCmd.Description))
		}
	}
}

// findCommand 查找命令定义
func (p *CommandParser) findCommand(path []string) *CommandDefinition {
	if len(path) == 0 {
		return nil
	}

	// 查找根命令
	var current *CommandDefinition
	for _, cmd := range p.rootCommands {
		if cmd.Name == path[0] || contains(cmd.Aliases, path[0]) {
			current = cmd
			break
		}
	}

	if current == nil {
		return nil
	}

	// 查找子命令
	for i := 1; i < len(path); i++ {
		found := false
		for _, subCmd := range current.SubCommands {
			if subCmd.Name == path[i] || contains(subCmd.Aliases, path[i]) {
				current = subCmd
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}

	return current
}

// contains 检查字符串切片是否包含指定值
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
