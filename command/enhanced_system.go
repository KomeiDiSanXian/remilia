package command

import (
	"fmt"
	"strconv"
	"strings"
)

// Definition 表示命令定义。
//
// 它支持命令树结构、参数验证、子命令等高级功能。
type Definition struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string

	Arguments []*Argument
	Flags     []*Flag

	SubCommands []*Definition

	Validator func(*Parsed) error
	Handler   Handler
}

// Handler 是 command 包内使用的最小处理器签名。
//
// 根包 `remilia` 可根据需要将其适配为自身的 Handler 类型。
//
// 注意：这里刻意不引用根包以避免循环依赖。
type Handler func(ctx any)

// Argument 表示命令的位置参数定义。
//
// 解析时会根据 Type 做类型转换；当 Required 为 true 且未提供时会返回错误；
// 当未提供且 Required 为 false 时会使用 Default。
type Argument struct {
	Name        string
	Description string
	Type        ArgType
	Required    bool
	Default     any
	Validator   func(string) error
}

// Flag 表示命令的标志（可选项）定义。
//
// Name 对应长标志（例如 --name），ShortName 对应短标志（例如 -n）。
// 解析时会根据 Type 做类型转换，并在需要时执行 Validator。
type Flag struct {
	Name        string
	ShortName   string
	Description string
	Type        ArgType
	Required    bool
	Default     any
	Validator   func(string) error
}

// ArgType 表示参数或标志的类型。
//
// 该类型用于指导解析阶段如何将字符串转换为具体类型。
type ArgType int

const (
	// ArgTypeString 表示字符串类型。
	ArgTypeString ArgType = iota
	// ArgTypeInt 表示整型。
	ArgTypeInt
	// ArgTypeBool 表示布尔类型。
	ArgTypeBool
	// ArgTypeFloat 表示浮点数类型（float64）。
	ArgTypeFloat
	// ArgTypeStringSlice 表示字符串切片类型。
	//
	// 对于位置参数：仅允许作为最后一个参数，且会将剩余所有 token 作为 slice。
	// 对于 flag：会对其 rawValue 做 strings.Fields 分割。
	ArgTypeStringSlice
)

// Parsed 表示输入命令解析后的结构化结果。
//
// Arguments / Flags 存储解析并完成类型转换后的值；rawArgs 保留原始解析结果供内部使用。
type Parsed struct {
	Raw         string
	CommandPath []string
	Definition  *Definition
	Arguments   map[string]any
	Flags       map[string]any

	rawArgs *Args
}

// Parser 表示命令解析器。
//
// 它维护已注册的根命令列表，并根据 prefix（例如“/”）进行命令匹配。
type Parser struct {
	rootCommands []*Definition
	prefix       string
}

// NewParser 创建一个新的命令解析器。
//
// prefix 用于指定命令前缀（例如“/”）；解析时会自动剥离该前缀用于匹配命令名。
func NewParser(prefix string) *Parser {
	return &Parser{rootCommands: make([]*Definition, 0), prefix: prefix}
}

// Register 注册一个根命令定义到解析器中。
func (p *Parser) Register(cmd *Definition) {
	p.rootCommands = append(p.rootCommands, cmd)
}

// Parse 将输入字符串解析为 Parsed。
//
// 会先进行词法拆分（ParseCommandLine），再匹配命令/子命令，最后解析参数与标志并执行定义中的 Validator。
func (p *Parser) Parse(input string) (*Parsed, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("empty input")
	}

	rawArgs, err := ParseCommandLine(input)
	if err != nil {
		return nil, err
	}

	cmdPath, def, remainingTokens := p.matchCommand(rawArgs)
	if def == nil {
		return nil, fmt.Errorf("unknown command: %s", rawArgs.Command)
	}

	return parseRest(input, rawArgs, cmdPath, def, remainingTokens)
}

// ParseFromDefinition 从给定的 rootDef 命令定义树解析输入字符串。
//
// 与 Parser.Parse 不同：该函数不依赖 Parser.Register 的根命令列表，而是直接以 rootDef 为根进行子命令匹配；
// 同时会校验输入命令是否与 prefix+rootDef.Name（或其别名）一致。
func ParseFromDefinition(input string, rootDef *Definition, prefix string) (*Parsed, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("empty input")
	}
	if rootDef == nil {
		return nil, fmt.Errorf("root command definition is nil")
	}

	rawArgs, err := ParseCommandLine(input)
	if err != nil {
		return nil, err
	}

	expectedCmd := prefix + rootDef.Name
	if rawArgs.Command != expectedCmd && !contains(rootDef.Aliases, strings.TrimPrefix(rawArgs.Command, prefix)) {
		return nil, fmt.Errorf("command mismatch: expected %s, got %s", expectedCmd, rawArgs.Command)
	}

	cmdPath, def, remainingTokens := matchSubCommands(rawArgs, rootDef)
	fullPath := append([]string{rootDef.Name}, cmdPath...)
	return parseRest(input, rawArgs, fullPath, def, remainingTokens)
}

func parseRest(input string, rawArgs *Args, cmdPath []string, def *Definition, remainingTokens []string) (*Parsed, error) {
	parsed := &Parsed{
		Raw:         input,
		CommandPath: cmdPath,
		Definition:  def,
		Arguments:   make(map[string]any),
		Flags:       make(map[string]any),
		rawArgs:     rawArgs,
	}

	if err := parseArguments(parsed, remainingTokens, def); err != nil {
		return nil, err
	}
	if err := parseFlags(parsed, rawArgs, def); err != nil {
		return nil, err
	}
	if def.Validator != nil {
		if err := def.Validator(parsed); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}
	return parsed, nil
}

func matchSubCommands(args *Args, rootDef *Definition) ([]string, *Definition, []string) {
	var path []string
	currentDef := rootDef
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

func (p *Parser) matchCommand(args *Args) ([]string, *Definition, []string) {
	cmdName := strings.TrimPrefix(args.Command, p.prefix)

	var currentDef *Definition
	for _, cmd := range p.rootCommands {
		if cmd.Name == cmdName || contains(cmd.Aliases, cmdName) {
			currentDef = cmd
			break
		}
	}
	if currentDef == nil {
		return nil, nil, nil
	}

	subPath, finalDef, remaining := matchSubCommands(args, currentDef)
	path := append([]string{currentDef.Name}, subPath...)
	return path, finalDef, remaining
}

func parseArguments(parsed *Parsed, tokens []string, def *Definition) error {
	for i, argDef := range def.Arguments {
		if argDef.Type == ArgTypeStringSlice {
			if i != len(def.Arguments)-1 {
				return fmt.Errorf("argument %s: stringSlice must be the last positional argument", argDef.Name)
			}
			vals := make([]string, 0)
			if i < len(tokens) {
				vals = append(vals, tokens[i:]...)
			}
			if argDef.Required && len(vals) == 0 {
				return fmt.Errorf("required argument %s is missing", argDef.Name)
			}
			parsed.Arguments[argDef.Name] = vals
			return nil
		}

		var value any
		var err error

		if i < len(tokens) {
			value, err = parseValue(tokens[i], argDef.Type)
			if err != nil {
				return fmt.Errorf("argument %s: %w", argDef.Name, err)
			}
			if argDef.Validator != nil {
				if err := argDef.Validator(tokens[i]); err != nil {
					return fmt.Errorf("argument %s validation failed: %w", argDef.Name, err)
				}
			}
		} else {
			if argDef.Required {
				return fmt.Errorf("required argument %s is missing", argDef.Name)
			}
			value = argDef.Default
		}
		parsed.Arguments[argDef.Name] = value
	}
	return nil
}

func parseFlags(parsed *Parsed, rawArgs *Args, def *Definition) error {
	for _, flagDef := range def.Flags {
		if flagDef.Type == ArgTypeStringSlice {
			var rawValue string
			var found bool
			if rawValue, found = rawArgs.Flags[flagDef.Name]; !found && flagDef.ShortName != "" {
				rawValue, found = rawArgs.Flags[flagDef.ShortName]
			}
			if found {
				vals := strings.Fields(rawValue)
				if flagDef.Required && len(vals) == 0 {
					return fmt.Errorf("required flag --%s is missing", flagDef.Name)
				}
				parsed.Flags[flagDef.Name] = vals
			} else {
				if flagDef.Required {
					return fmt.Errorf("required flag --%s is missing", flagDef.Name)
				}
				parsed.Flags[flagDef.Name] = flagDef.Default
			}
			continue
		}

		var rawValue string
		var found bool
		if rawValue, found = rawArgs.Flags[flagDef.Name]; !found && flagDef.ShortName != "" {
			rawValue, found = rawArgs.Flags[flagDef.ShortName]
		}

		if found {
			value, err := parseValue(rawValue, flagDef.Type)
			if err != nil {
				return fmt.Errorf("flag --%s: %w", flagDef.Name, err)
			}
			if flagDef.Validator != nil {
				if err := flagDef.Validator(rawValue); err != nil {
					return fmt.Errorf("flag --%s validation failed: %w", flagDef.Name, err)
				}
			}
			parsed.Flags[flagDef.Name] = value
		} else {
			if flagDef.Required {
				return fmt.Errorf("required flag --%s is missing", flagDef.Name)
			}
			parsed.Flags[flagDef.Name] = flagDef.Default
		}
	}
	return nil
}

func parseValue(s string, t ArgType) (any, error) {
	switch t {
	case ArgTypeString:
		return s, nil
	case ArgTypeInt:
		return strconv.Atoi(s)
	case ArgTypeBool:
		switch s {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		default:
			return false, fmt.Errorf("invalid boolean value: %s", s)
		}
	case ArgTypeFloat:
		return strconv.ParseFloat(s, 64)
	case ArgTypeStringSlice:
		return strings.Fields(s), nil
	default:
		return s, nil
	}
}

// GetString 按名称获取参数或标志值，并以 string 形式返回。
//
// 优先从 Arguments 中查找，其次从 Flags 中查找；若不存在或为 nil 则返回空字符串。
func (p *Parsed) GetString(name string) string {
	if val, ok := p.Arguments[name]; ok && val != nil {
		return val.(string)
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		return val.(string)
	}
	return ""
}

// GetInt 按名称获取参数或标志值，并以 int 形式返回。
//
// 优先从 Arguments 中查找，其次从 Flags 中查找；若不存在或为 nil 则返回 0。
func (p *Parsed) GetInt(name string) int {
	if val, ok := p.Arguments[name]; ok && val != nil {
		return val.(int)
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		return val.(int)
	}
	return 0
}

// GetBool 按名称获取参数或标志值，并以 bool 形式返回。
//
// 优先从 Arguments 中查找，其次从 Flags 中查找；若不存在或为 nil 则返回 false。
func (p *Parsed) GetBool(name string) bool {
	if val, ok := p.Arguments[name]; ok && val != nil {
		return val.(bool)
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		return val.(bool)
	}
	return false
}

// GetFloat 按名称获取参数或标志值，并以 float64 形式返回。
//
// 优先从 Arguments 中查找，其次从 Flags 中查找；若不存在或为 nil 则返回 0。
func (p *Parsed) GetFloat(name string) float64 {
	if val, ok := p.Arguments[name]; ok && val != nil {
		return val.(float64)
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		return val.(float64)
	}
	return 0
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
