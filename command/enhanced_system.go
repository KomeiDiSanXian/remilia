package command

import (
	"fmt"
	"strconv"
	"strings"
)

// CommandDefinition 命令定义
//
// 支持命令树结构、参数验证、子命令等高级功能。
type CommandDefinition struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string

	Arguments []*Argument
	Flags     []*Flag

	SubCommands []*CommandDefinition

	Validator func(*ParsedCommand) error
	Handler   Handler
}

// Handler is a minimal handler signature used by command package.
// Root package `remilia` can adapt this to its own Handler as needed.
//
// NOTE: We intentionally avoid importing root package here to prevent circular deps.
type Handler func(ctx any)

type Argument struct {
	Name        string
	Description string
	Type        ArgType
	Required    bool
	Default     interface{}
	Validator   func(string) error
}

type Flag struct {
	Name        string
	ShortName   string
	Description string
	Type        ArgType
	Required    bool
	Default     interface{}
	Validator   func(string) error
}

type ArgType int

const (
	ArgTypeString ArgType = iota
	ArgTypeInt
	ArgTypeBool
	ArgTypeFloat
	ArgTypeStringSlice
)

// ParsedCommand 解析后的命令
type ParsedCommand struct {
	Raw         string
	CommandPath []string
	Definition  *CommandDefinition
	Arguments   map[string]interface{}
	Flags       map[string]interface{}

	rawArgs *CommandArgs
}

type CommandParser struct {
	rootCommands []*CommandDefinition
	prefix       string
}

func NewCommandParser(prefix string) *CommandParser {
	return &CommandParser{rootCommands: make([]*CommandDefinition, 0), prefix: prefix}
}

func (p *CommandParser) Register(cmd *CommandDefinition) {
	p.rootCommands = append(p.rootCommands, cmd)
}

func (p *CommandParser) Parse(input string) (*ParsedCommand, error) {
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

func ParseFromDefinition(input string, rootDef *CommandDefinition, prefix string) (*ParsedCommand, error) {
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

func parseRest(input string, rawArgs *CommandArgs, cmdPath []string, def *CommandDefinition, remainingTokens []string) (*ParsedCommand, error) {
	parsed := &ParsedCommand{
		Raw:         input,
		CommandPath: cmdPath,
		Definition:  def,
		Arguments:   make(map[string]interface{}),
		Flags:       make(map[string]interface{}),
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

func matchSubCommands(args *CommandArgs, rootDef *CommandDefinition) ([]string, *CommandDefinition, []string) {
	path := []string{}
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

func (p *CommandParser) matchCommand(args *CommandArgs) ([]string, *CommandDefinition, []string) {
	cmdName := strings.TrimPrefix(args.Command, p.prefix)

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

	subPath, finalDef, remaining := matchSubCommands(args, currentDef)
	path := append([]string{currentDef.Name}, subPath...)
	return path, finalDef, remaining
}

func parseArguments(parsed *ParsedCommand, tokens []string, def *CommandDefinition) error {
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

		var value interface{}
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

func parseFlags(parsed *ParsedCommand, rawArgs *CommandArgs, def *CommandDefinition) error {
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

func parseValue(s string, t ArgType) (interface{}, error) {
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

func (p *ParsedCommand) GetString(name string) string {
	if val, ok := p.Arguments[name]; ok && val != nil {
		return val.(string)
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		return val.(string)
	}
	return ""
}

func (p *ParsedCommand) GetInt(name string) int {
	if val, ok := p.Arguments[name]; ok && val != nil {
		return val.(int)
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		return val.(int)
	}
	return 0
}

func (p *ParsedCommand) GetBool(name string) bool {
	if val, ok := p.Arguments[name]; ok && val != nil {
		return val.(bool)
	}
	if val, ok := p.Flags[name]; ok && val != nil {
		return val.(bool)
	}
	return false
}

func (p *ParsedCommand) GetFloat(name string) float64 {
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
