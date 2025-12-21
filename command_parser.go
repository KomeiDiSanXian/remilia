package remilia

import (
	"fmt"
	"strconv"
	"strings"
)

// CommandArgs 命令参数结构
type CommandArgs struct {
	Raw        string            // 原始命令字符串
	Command    string            // 命令名称（如 /weather）
	Positional []string          // 位置参数
	Flags      map[string]string // 命名参数（--key value）
	parsed     bool              // 是否已解析
}

// ParseCommand 解析命令参数
// 支持格式：
//   - 位置参数: /weather Beijing
//   - 命名参数: /weather --city Beijing --unit celsius
//   - 混合参数: /weather Beijing --unit celsius
//
// 示例:
//
//	input: "/weather Beijing --unit celsius --days 3"
//	output: CommandArgs{
//	  Command: "/weather",
//	  Positional: ["Beijing"],
//	  Flags: {"unit": "celsius", "days": "3"},
//	}
func (ctx *Context) ParseCommand() (*CommandArgs, error) {
	content := ctx.GetMessageContent()
	if content == "" {
		return nil, fmt.Errorf("empty message content")
	}

	args := &CommandArgs{
		Raw:        content,
		Flags:      make(map[string]string),
		Positional: make([]string, 0),
	}

	// 分词（支持引号）
	tokens, err := tokenize(content)
	if err != nil {
		return nil, fmt.Errorf("tokenize error: %w", err)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("no tokens found")
	}

	// 第一个 token 是命令
	args.Command = tokens[0]
	tokens = tokens[1:]

	// 解析参数
	i := 0
	for i < len(tokens) {
		token := tokens[i]

		// 检查是否是命名参数
		if strings.HasPrefix(token, "--") {
			// 命名参数格式: --key value
			key := strings.TrimPrefix(token, "--")
			if key == "" {
				return nil, fmt.Errorf("invalid flag: %s", token)
			}

			// 检查是否有值
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "--") {
				args.Flags[key] = tokens[i+1]
				i += 2
			} else {
				// 无值的 flag，设为 "true"
				args.Flags[key] = "true"
				i++
			}
		} else if strings.HasPrefix(token, "-") && len(token) == 2 {
			// 短选项格式: -c value
			key := strings.TrimPrefix(token, "-")
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				args.Flags[key] = tokens[i+1]
				i += 2
			} else {
				args.Flags[key] = "true"
				i++
			}
		} else {
			// 位置参数
			args.Positional = append(args.Positional, token)
			i++
		}
	}

	args.parsed = true
	return args, nil
}

// tokenize 分词函数，支持引号和转义字符
// 示例: `hello "world test" foo` -> ["hello", "world test", "foo"]
func tokenize(s string) ([]string, error) {
	tokens := make([]string, 0)
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)
	escaped := false

	for _, r := range s {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' {
			escaped = true
			continue
		}

		switch r {
		case '"', '\'':
			if !inQuote {
				inQuote = true
				quoteChar = r
			} else if r == quoteChar {
				inQuote = false
				quoteChar = 0
			} else {
				current.WriteRune(r)
			}
		case ' ', '\t', '\n':
			if inQuote {
				current.WriteRune(r)
			} else if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if escaped {
		return nil, fmt.Errorf("unexpected end of string after escape character")
	}

	if inQuote {
		return nil, fmt.Errorf("unclosed quote: %c", quoteChar)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens, nil
}

// Get 获取位置参数（从 0 开始）
func (args *CommandArgs) Get(index int) string {
	if index < 0 || index >= len(args.Positional) {
		return ""
	}
	return args.Positional[index]
}

// GetFlag 获取命名参数
func (args *CommandArgs) GetFlag(key string) string {
	return args.Flags[key]
}

// GetFlagOrDefault 获取命名参数，如果不存在返回默认值
func (args *CommandArgs) GetFlagOrDefault(key, defaultValue string) string {
	if val, ok := args.Flags[key]; ok {
		return val
	}
	return defaultValue
}

// HasFlag 检查是否存在某个命名参数
func (args *CommandArgs) HasFlag(key string) bool {
	_, ok := args.Flags[key]
	return ok
}

// GetInt 获取位置参数并转换为 int
func (args *CommandArgs) GetInt(index int) (int, error) {
	val := args.Get(index)
	if val == "" {
		return 0, fmt.Errorf("argument at index %d not found", index)
	}
	return strconv.Atoi(val)
}

// GetFlagInt 获取命名参数并转换为 int
func (args *CommandArgs) GetFlagInt(key string) (int, error) {
	val := args.GetFlag(key)
	if val == "" {
		return 0, fmt.Errorf("flag %s not found", key)
	}
	return strconv.Atoi(val)
}

// GetIntOrDefault 获取位置参数并转换为 int，失败返回默认值
func (args *CommandArgs) GetIntOrDefault(index, defaultValue int) int {
	val, err := args.GetInt(index)
	if err != nil {
		return defaultValue
	}
	return val
}

// GetFlagIntOrDefault 获取命名参数并转换为 int，失败返回默认值
func (args *CommandArgs) GetFlagIntOrDefault(key string, defaultValue int) int {
	val, err := args.GetFlagInt(key)
	if err != nil {
		return defaultValue
	}
	return val
}

// GetBool 获取位置参数并转换为 bool
func (args *CommandArgs) GetBool(index int) (bool, error) {
	val := args.Get(index)
	if val == "" {
		return false, fmt.Errorf("argument at index %d not found", index)
	}
	return strconv.ParseBool(val)
}

// GetFlagBool 获取命名参数并转换为 bool
func (args *CommandArgs) GetFlagBool(key string) bool {
	val := args.GetFlag(key)
	if val == "" {
		return false
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return false
	}
	return b
}

// Len 返回位置参数的数量
func (args *CommandArgs) Len() int {
	return len(args.Positional)
}

// String 返回命令参数的字符串表示（用于调试）
func (args *CommandArgs) String() string {
	return fmt.Sprintf("CommandArgs{Command: %s, Positional: %v, Flags: %v}",
		args.Command, args.Positional, args.Flags)
}
