package command

import (
	"fmt"
	"slices"
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

	// ===== Help 生成相关字段 =====
	Category    string   // 命令分类（如 "管理"、"实用工具"）
	Examples    []string // 使用示例
	Permissions []string // 所需权限
	Hidden      bool     // 是否在帮助中隐藏
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
// 流程：
//  1. 词法拆分（tokenize）
//  2. 匹配根命令名称
//  3. 通过流式分层解析（parseHierarchical）将 flag 归属到其所在层级
//  4. 对叶子层级执行参数绑定与 Validator
//
// flag 作用域规则（与 Cobra / GNU getopt 一致）：
//   - flag 出现在子命令 token 之前 → 属于父命令
//   - flag 出现在子命令 token 之后 → 属于子命令
//   - "--" 终止 flag 解析，其后所有 token 均为位置参数
func (p *Parser) Parse(input string) (*Parsed, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("empty input")
	}

	rawArgs, err := ParseCommandLine(input)
	if err != nil {
		return nil, err
	}

	// Locate the root command definition.
	cmdName := strings.TrimPrefix(rawArgs.Command, p.prefix)
	var rootDef *Definition
	for _, cmd := range p.rootCommands {
		if cmd.Name == cmdName || contains(cmd.Aliases, cmdName) {
			rootDef = cmd
			break
		}
	}
	if rootDef == nil {
		return nil, fmt.Errorf("unknown command: %s", rawArgs.Command)
	}

	// Re-tokenize to obtain the raw token stream for hierarchical parsing.
	// (ParseCommandLine already called tokenize internally, but does not expose
	// the token slice — this second call is intentional and cheap.)
	tokens, err := tokenize(input)
	if err != nil {
		return nil, err
	}
	levels := parseHierarchical(tokens[1:], rootDef)
	return buildParsedFromLevels(input, rawArgs, levels)
}

// ParseFromDefinition 从给定的 rootDef 命令定义树解析输入字符串。
//
// 与 Parser.Parse 不同：该函数不依赖 Parser.Register 的根命令列表，而是直接以
// rootDef 为根进行子命令匹配；同时会校验输入命令是否与 prefix+rootDef.Name
// （或其别名）一致。
//
// flag 作用域规则与 Parser.Parse 完全相同（流式分层解析）。
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

	tokens, err := tokenize(input)
	if err != nil {
		return nil, err
	}
	levels := parseHierarchical(tokens[1:], rootDef)
	return buildParsedFromLevels(input, rawArgs, levels)
}

// cmdLevel holds the parsed data for one level of a command hierarchy.
type cmdLevel struct {
	def   *Definition
	flags map[string]string // raw string flags scoped to this level
	args  []string          // positional args for this level
}

// parseHierarchical processes the token slice left-to-right, assigning each flag
// and positional argument to the command level where it appears.
//
// Design follows the Cobra / GNU getopt convention:
//
//   - A flag token (--key, --key=val, -k, -k val) belongs to the current level.
//   - Bool flags never consume the following token as a value (Cobra behaviour).
//     Use --flag=false to explicitly negate; bare --flag always means true.
//   - A non-flag token is first tested as a subcommand name at the current level;
//     on match the parser descends into the child level for all subsequent tokens.
//   - If no subcommand matches, the token becomes a positional argument.
//   - The POSIX "--" sentinel ends flag parsing; all following tokens are positional
//     at the current level.
//
// This ensures that flags typed before a subcommand belong to the parent, while
// flags typed after the subcommand belong to the child — no global reordering.
func parseHierarchical(tokens []string, rootDef *Definition) []cmdLevel {
	levels := []cmdLevel{{
		def:   rootDef,
		flags: make(map[string]string),
		args:  make([]string, 0),
	}}

	i := 0
	for i < len(tokens) {
		token := tokens[i]
		current := &levels[len(levels)-1]

		// POSIX "--": stop flag parsing, treat all remaining tokens as positional.
		if token == "--" {
			i++
			for i < len(tokens) {
				current.args = append(current.args, tokens[i])
				i++
			}
			break
		}

		// Long flag: --key  or  --key=value
		if after, ok := strings.CutPrefix(token, "--"); ok && after != "" {
			if eqIdx := strings.Index(after, "="); eqIdx > 0 {
				// --key=value — always explicit, no ambiguity
				current.flags[after[:eqIdx]] = after[eqIdx+1:]
				i++
				continue
			}
			// Bool flags only consume the next token when it is an unambiguous
			// boolean literal (true/false/yes/no/on/off/1/0).  Any other token —
			// including subcommand names — is left in the stream.
			if isBoolFlag(current.def, after) {
				if i+1 < len(tokens) && isBoolLiteral(tokens[i+1]) {
					current.flags[after] = tokens[i+1]
					i += 2
				} else {
					current.flags[after] = "true"
					i++
				}
				continue
			}
			// String/int/… flag: consume next token as value if it does not look
			// like a flag itself (same heuristic as ParseCommandLine).
			if i+1 < len(tokens) && !hierarchyLooksLikeFlag(tokens[i+1]) {
				current.flags[after] = tokens[i+1]
				i += 2
			} else {
				current.flags[after] = "true"
				i++
			}
			continue
		}

		// Short flag: -k  or  -k value  (single letter only, not a number)
		if len(token) == 2 && token[0] == '-' && isAlphaRune(rune(token[1])) {
			key := string(token[1])
			if isBoolFlagShort(current.def, key) {
				if i+1 < len(tokens) && isBoolLiteral(tokens[i+1]) {
					current.flags[key] = tokens[i+1]
					i += 2
				} else {
					current.flags[key] = "true"
					i++
				}
				continue
			}
			if i+1 < len(tokens) && !hierarchyLooksLikeFlag(tokens[i+1]) {
				current.flags[key] = tokens[i+1]
				i += 2
			} else {
				current.flags[key] = "true"
				i++
			}
			continue
		}

		// Non-flag token: attempt subcommand match at the current level first.
		if len(current.def.SubCommands) > 0 {
			if sub := findSubCmd(current.def.SubCommands, token); sub != nil {
				levels = append(levels, cmdLevel{
					def:   sub,
					flags: make(map[string]string),
					args:  make([]string, 0),
				})
				i++
				continue
			}
		}

		// Falls through to positional argument at the current level.
		current.args = append(current.args, token)
		i++
	}

	return levels
}

// buildParsedFromLevels constructs a Parsed from the hierarchical level chain.
// Only the leaf level's flags and positional arguments are bound — each level
// owns its own scope, matching Cobra / Click semantics.
func buildParsedFromLevels(input string, rawArgs *Args, levels []cmdLevel) (*Parsed, error) {
	leaf := levels[len(levels)-1]

	path := make([]string, len(levels))
	for i, l := range levels {
		path[i] = l.def.Name
	}

	parsed := &Parsed{
		Raw:         input,
		CommandPath: path,
		Definition:  leaf.def,
		Arguments:   make(map[string]any),
		Flags:       make(map[string]any),
		rawArgs:     rawArgs,
	}

	if err := parseArguments(parsed, leaf.args, leaf.def); err != nil {
		return nil, err
	}
	if err := parseFlagsFromMap(parsed, leaf.flags, leaf.def); err != nil {
		return nil, err
	}
	if leaf.def.Validator != nil {
		if err := leaf.def.Validator(parsed); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}
	return parsed, nil
}

// hierarchyLooksLikeFlag reports whether s is a flag token (starts with "-" and
// the second character is a letter or "-"). Negative numbers such as -1, -3.14
// are not considered flags.
func hierarchyLooksLikeFlag(s string) bool {
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	return isAlphaRune(rune(s[1])) || s[1] == '-'
}

// isAlphaRune reports whether r is an ASCII letter.
func isAlphaRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isBoolFlag reports whether the long flag name is declared as ArgTypeBool in def.
func isBoolFlag(def *Definition, name string) bool {
	for _, f := range def.Flags {
		if f.Name == name && f.Type == ArgTypeBool {
			return true
		}
	}
	return false
}

// isBoolFlagShort reports whether the short flag name is declared as ArgTypeBool in def.
func isBoolFlagShort(def *Definition, shortName string) bool {
	for _, f := range def.Flags {
		if f.ShortName == shortName && f.Type == ArgTypeBool {
			return true
		}
	}
	return false
}

// isBoolLiteral reports whether s is an unambiguous boolean literal.
// These are the only values a bool flag will consume from the token stream;
// any other adjacent token (e.g. a subcommand name) is left in place.
func isBoolLiteral(s string) bool {
	switch s {
	case "true", "false", "1", "0", "yes", "no", "on", "off":
		return true
	}
	return false
}

// findSubCmd returns the first Definition in cmds whose Name or Aliases matches name.
func findSubCmd(cmds []*Definition, name string) *Definition {
	for _, cmd := range cmds {
		if cmd.Name == name || contains(cmd.Aliases, name) {
			return cmd
		}
	}
	return nil
}

// parseFlagsFromMap resolves and type-converts the raw string flags collected
// for a single command level into parsed.Flags, validating required flags.
func parseFlagsFromMap(parsed *Parsed, rawFlags map[string]string, def *Definition) error {
	for _, flagDef := range def.Flags {
		var rawValue string
		var found bool

		if rawValue, found = rawFlags[flagDef.Name]; !found && flagDef.ShortName != "" {
			rawValue, found = rawFlags[flagDef.ShortName]
		}

		if flagDef.Type == ArgTypeStringSlice {
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
		if s, ok := val.(string); ok {
			return s
		}
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// GetInt 按名称获取参数或标志值，并以 int 形式返回。
//
// 优先从 Arguments 中查找，其次从 Flags 中查找；若不存在或为 nil 则返回 0。
func (p *Parsed) GetInt(name string) int {
	if val, ok := p.Arguments[name]; ok && val != nil {
		if i, ok := val.(int); ok {
			return i
		}
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		if i, ok := val.(int); ok {
			return i
		}
	}
	return 0
}

// GetBool 按名称获取参数或标志值，并以 bool 形式返回。
//
// 优先从 Arguments 中查找，其次从 Flags 中查找；若不存在或为 nil 则返回 false。
func (p *Parsed) GetBool(name string) bool {
	if val, ok := p.Arguments[name]; ok && val != nil {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

// GetFloat 按名称获取参数或标志值，并以 float64 形式返回。
//
// 优先从 Arguments 中查找，其次从 Flags 中查找；若不存在或为 nil 则返回 0。
func (p *Parsed) GetFloat(name string) float64 {
	if val, ok := p.Arguments[name]; ok && val != nil {
		if f, ok := val.(float64); ok {
			return f
		}
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		if f, ok := val.(float64); ok {
			return f
		}
	}
	return 0
}

func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}
