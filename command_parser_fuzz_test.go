package remilia

import (
	"testing"
	"unicode/utf8"
)

func FuzzTokenize(f *testing.F) {
	// 添加种子语料
	f.Add("hello world")
	f.Add(`hello "world"`)
	f.Add(`hello 'world'`)
	f.Add(`hello \"world\"`)
	f.Add(`hello \\ world`)
	f.Add(`"unclosed quote`)
	f.Add(`escaped \`)

	f.Fuzz(func(t *testing.T, input string) {
		// 确保输入是有效的 UTF-8，虽然 tokenize 应该能处理无效 UTF-8（range 循环会产生 utf8.RuneError）
		if !utf8.ValidString(input) {
			return
		}

		tokens, err := tokenize(input)

		// 检查基本的不变性
		if err == nil {
			// 如果没有错误，tokens 不应该为 nil（虽然可以是空切片）
			if tokens == nil {
				t.Errorf("expected non-nil tokens for input: %q", input)
			}
		} else {
			// 如果有错误，tokens 应该是 nil
			if tokens != nil {
				t.Errorf("expected nil tokens on error for input: %q", input)
			}
		}
	})
}
