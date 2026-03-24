package command

import (
	"fmt"
	"strconv"
	"strings"
)

// Args 命令参数结构
//
// 注意：这里是“基础命令行解析”结果，供增强解析器/业务方复用。
//
// 解析约定：
// - 第一个 token 为 Command
// - 其余 token：支持 positional 与 flags（--k v / -k v），无值 flag 视为 true
// - token 化支持引号与反斜杠转义
//
// 示例：
//
//	input:  "/weather Beijing --unit celsius --days 3"
//	output: Command="/weather", Positional=["Beijing"], Flags={unit:celsius, days:3}
//
// 备注：StringSlice 等更高级语义由增强命令系统处理。
type Args struct {
	Raw        string            // 原始命令字符串
	Command    string            // 命令名称（如 /weather）
	Positional []string          // 位置参数
	Flags      map[string]string // 命名参数（--key value）
	parsed     bool
}

// ParseCommandLine 解析原始命令行字符串
//
// 性能优化：
// - 预分配 map 和 slice 容量
// - 减少字符串操作和内存分配
func ParseCommandLine(input string) (*Args, error) {
	tokens, err := tokenize(input)
	if err != nil {
		return nil, fmt.Errorf("tokenize error: %w", err)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("no tokens found")
	}

	// 预分配容量：估计有少量 flags 和 positional 参数
	estimatedFlags := max(len(tokens)/4, 2)
	estimatedPositional := max(len(tokens)/2, 2)

	args := &Args{
		Raw:        input,
		Command:    tokens[0],
		Flags:      make(map[string]string, estimatedFlags),
		Positional: make([]string, 0, estimatedPositional),
	}

	tokens = tokens[1:]

	for i := 0; i < len(tokens); {
		token := tokens[i]

		// POSIX "--": stop flag parsing, all following tokens are positional.
		if token == "--" {
			i++
			for i < len(tokens) {
				args.Positional = append(args.Positional, tokens[i])
				i++
			}
			break
		}

		// 处理长标志（--key 或 --key=value）
		if after, ok := strings.CutPrefix(token, "--"); ok {
			key := after
			if key == "" {
				return nil, fmt.Errorf("invalid flag: %s", token)
			}

			// 检查 --key=value 格式
			if eqIdx := strings.Index(key, "="); eqIdx > 0 {
				args.Flags[key[:eqIdx]] = key[eqIdx+1:]
				i++
				continue
			}

			// 检查下一个 token 是否为值（而非另一个标志）
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "--") && !isShortFlag(tokens[i+1]) {
				// 接受下一个 token 作为值
				// 允许负数，如 --days -1
				args.Flags[key] = tokens[i+1]
				i += 2
			} else {
				args.Flags[key] = "true"
				i++
			}
			continue
		}

		// 处理短标志（-k 或 -k value）
		if strings.HasPrefix(token, "-") && len(token) == 2 {
			key := strings.TrimPrefix(token, "-")
			// 检查下一个 token 是否存在且不是另一个标志
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "--") && !isShortFlag(tokens[i+1]) {
				// 接受下一个 token 作为值
				args.Flags[key] = tokens[i+1]
				i += 2
			} else {
				args.Flags[key] = "true"
				i++
			}
			continue
		}

		args.Positional = append(args.Positional, token)
		i++
	}

	args.parsed = true
	return args, nil
}

// isShortFlag 检查 token 是否看起来像短标志（-x，x 为字母）
// 对于负数（如 -1、-42、-3.14）返回 false
func isShortFlag(token string) bool {
	if !strings.HasPrefix(token, "-") {
		return false
	}
	if len(token) != 2 {
		return false
	}
	// 检查 '-' 后的字符是否为字母（而非数字）
	char := token[1]
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

// tokenize 分词函数，支持引号和转义字符
// 示例: `hello "world test" foo` -> ["hello", "world test", "foo"]
//
// 性能优化：
// - 预分配 tokens slice 容量（减少扩容）
// - 预分配 strings.Builder 缓冲区（减少内存分配）
func tokenize(s string) ([]string, error) {
	// 预分配容量：估计平均每8个字符一个token
	estimatedTokens := max(len(s)/8+1, 4)
	tokens := make([]string, 0, estimatedTokens)

	var current strings.Builder
	current.Grow(32) // 预分配32字节缓冲区，适合大多数token

	inQuote := false
	quoteChar := rune(0)
	escaped := false

	for _, r := range s {
		if escaped {
			// 处理特殊转义字符
			switch r {
			case 'n':
				current.WriteRune('\n')
			case 't':
				current.WriteRune('\t')
			case 'r':
				current.WriteRune('\r')
			case '\\':
				current.WriteRune('\\')
			case '"':
				current.WriteRune('"')
			case '\'':
				current.WriteRune('\'')
			default:
				// 其他字符直接输出
				current.WriteRune(r)
			}
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
				// 每次 Reset 后重新 Grow，保持缓冲区大小
				current.Grow(32)
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
func (args *Args) Get(index int) string {
	if index < 0 || index >= len(args.Positional) {
		return ""
	}
	return args.Positional[index]
}

// GetFlag 获取命名参数
func (args *Args) GetFlag(key string) string { return args.Flags[key] }

// GetFlagOrDefault 获取命名参数，如果不存在返回默认值
func (args *Args) GetFlagOrDefault(key, defaultValue string) string {
	if val, ok := args.Flags[key]; ok {
		return val
	}
	return defaultValue
}

// HasFlag 检查是否存在某个命名参数
func (args *Args) HasFlag(key string) bool {
	_, ok := args.Flags[key]
	return ok
}

// GetInt 获取位置参数并转换为 int
func (args *Args) GetInt(index int) (int, error) {
	val := args.Get(index)
	if val == "" {
		return 0, fmt.Errorf("argument at index %d not found", index)
	}
	return strconv.Atoi(val)
}

// GetFlagInt 获取命名参数并转换为 int
func (args *Args) GetFlagInt(key string) (int, error) {
	val := args.GetFlag(key)
	if val == "" {
		return 0, fmt.Errorf("flag %s not found", key)
	}
	return strconv.Atoi(val)
}

// GetIntOrDefault 获取位置参数并转换为 int，失败返回默认值
func (args *Args) GetIntOrDefault(index, defaultValue int) int {
	val, err := args.GetInt(index)
	if err != nil {
		return defaultValue
	}
	return val
}

// GetFlagIntOrDefault 获取命名参数并转换为 int，失败返回默认值
func (args *Args) GetFlagIntOrDefault(key string, defaultValue int) int {
	val, err := args.GetFlagInt(key)
	if err != nil {
		return defaultValue
	}
	return val
}

// GetBool 获取位置参数并转换为 bool
func (args *Args) GetBool(index int) (bool, error) {
	val := args.Get(index)
	if val == "" {
		return false, fmt.Errorf("argument at index %d not found", index)
	}
	return strconv.ParseBool(val)
}

// GetFlagBool 获取命名参数并转换为 bool
func (args *Args) GetFlagBool(key string) bool {
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

// ParseInt 解析字符串为 int（全局辅助函数）
func ParseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

// Len 返回位置参数数量
func (args *Args) Len() int { return len(args.Positional) }

func (args *Args) String() string { return args.Raw }
