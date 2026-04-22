// Package expr 提供安全的数学表达式求值工具。
//
// 使用递归下降解析器实现，支持 + - * / () 运算符和一元负号。
// 不依赖任何外部包，不使用 eval/reflect/unsafe，适合在生产环境安全使用。
//
// # 基本用法
//
//	val, err := expr.Eval("(1+2)*3")
//	// val = 9.0, err = nil
//
// # 带数字收集（用于 24 点游戏等验证场景）
//
//	val, nums, err := expr.EvalWithNums("(5+3)*2*1")
//	// val = 16.0, nums = [5, 3, 2, 1], err = nil
//
// # 支持的语法
//
//   - 整数数字字面量（如 1, 13, 100）
//   - 二元运算符：+ - * /
//   - 一元负号：-（如 -(1+2)）
//   - 括号分组：()
//   - 空白字符（空格、制表符）忽略
//
// # 已知限制
//
//   - 仅支持整数字面量（不支持小数点）
//   - 所有中间计算使用 float64（浮点精度）
//   - 除以零时返回 ErrDivisionByZero
//
// # 框架问题记录
//
// 本包解决框架问题 #22：
// "缺少内置的数学表达式安全求值工具"
// zerobot-remilia 中 math24 和 calc 插件通过此包实现表达式求值，
// 无需引入第三方依赖（如 github.com/expr-lang/expr）。
package expr

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

// ErrDivisionByZero 除以零错误
var ErrDivisionByZero = errors.New("expr: division by zero")

// ErrInvalidExpr 表达式语法错误
var ErrInvalidExpr = errors.New("expr: invalid expression")

// Eval 求值数学表达式，返回 float64 结果。
//
// 表达式只能包含整数字面量、+ - * / ()、空白字符；
// 遇到非法字符或语法错误时返回 ErrInvalidExpr 包装的错误。
//
// 示例：
//
//	val, _ := expr.Eval("(3+5)*2")  // 16.0
//	val, _ := expr.Eval("10/3")     // 3.3333...
//	_, err := expr.Eval("1/0")      // ErrDivisionByZero
func Eval(expression string) (float64, error) {
	val, _, err := EvalWithNums(expression)
	return val, err
}

// EvalWithNums 求值并收集表达式中出现的所有整数字面量（按出现顺序）。
//
// 用于 24 点等游戏场景：调用方可以验证玩家使用的数字与给定牌面完全匹配。
//
// 示例：
//
//	val, nums, _ := expr.EvalWithNums("(5+7)*2")
//	// val = 24.0, nums = [5, 7, 2]
func EvalWithNums(expression string) (float64, []int, error) {
	if len(expression) > 256 {
		return 0, nil, fmt.Errorf("%w: expression too long (max 256 chars)", ErrInvalidExpr)
	}

	tokens, err := tokenize(expression)
	if err != nil {
		return 0, nil, err
	}

	p := &parser{tokens: tokens}
	val, err := p.parseExpr()
	if err != nil {
		return 0, nil, err
	}

	if p.peek().kind != tokEOF {
		return 0, nil, fmt.Errorf("%w: unexpected token after expression", ErrInvalidExpr)
	}

	return val, p.nums, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Tokenizer
// ────────────────────────────────────────────────────────────────────────────

type tokenKind int

const (
	tokNum    tokenKind = iota
	tokPlus             // +
	tokMinus            // -
	tokMul              // *
	tokDiv              // /
	tokLParen           // (
	tokRParen           // )
	tokEOF
)

type token struct {
	kind tokenKind
	val  int // only valid for tokNum
}

func tokenize(expr string) ([]token, error) {
	var tokens []token
	i := 0
	for i < len(expr) {
		ch := expr[i]

		// Fast path for pure ASCII characters
		if ch < 0x80 {
			switch {
			case ch == ' ' || ch == '\t':
				i++
			case ch >= '0' && ch <= '9':
				j := i
				for j < len(expr) && expr[j] >= '0' && expr[j] <= '9' {
					j++
				}
				n, _ := strconv.Atoi(expr[i:j])
				tokens = append(tokens, token{tokNum, n})
				i = j
			case ch == '+':
				tokens = append(tokens, token{tokPlus, 0})
				i++
			case ch == '-':
				tokens = append(tokens, token{tokMinus, 0})
				i++
			case ch == '*':
				tokens = append(tokens, token{tokMul, 0})
				i++
			case ch == '/':
				tokens = append(tokens, token{tokDiv, 0})
				i++
			case ch == '(':
				tokens = append(tokens, token{tokLParen, 0})
				i++
			case ch == ')':
				tokens = append(tokens, token{tokRParen, 0})
				i++
			default:
				return nil, fmt.Errorf("%w: illegal character '%c' (U+%04X)", ErrInvalidExpr, rune(ch), rune(ch))
			}
			continue
		}

		// Multi-byte UTF-8: decode rune and map to operator
		r, size := decodeRuneAt(expr, i)
		switch r {
		case '×': // U+00D7 MULTIPLICATION SIGN
			tokens = append(tokens, token{tokMul, 0})
		case '÷': // U+00F7 DIVISION SIGN
			tokens = append(tokens, token{tokDiv, 0})
		case '（': // U+FF08 FULLWIDTH LEFT PARENTHESIS
			tokens = append(tokens, token{tokLParen, 0})
		case '）': // U+FF09 FULLWIDTH RIGHT PARENTHESIS
			tokens = append(tokens, token{tokRParen, 0})
		default:
			return nil, fmt.Errorf("%w: illegal character '%c' (U+%04X)", ErrInvalidExpr, r, r)
		}
		i += size
	}
	tokens = append(tokens, token{tokEOF, 0})
	return tokens, nil
}

// decodeRuneAt decodes a UTF-8 rune at position i in s.
func decodeRuneAt(s string, i int) (rune, int) {
	// Simple UTF-8 decode: first byte determines length
	b := s[i]
	if b < 0x80 {
		return rune(b), 1
	}
	if b < 0xC0 {
		return rune(b), 1 // invalid, treat as 1 byte
	}
	// Multi-byte: decode properly
	n := 0
	if b < 0xE0 {
		n = 2
	} else if b < 0xF0 {
		n = 3
	} else {
		n = 4
	}
	if i+n > len(s) {
		return rune(b), 1
	}
	var r rune
	switch n {
	case 2:
		r = rune(b&0x1F)<<6 | rune(s[i+1]&0x3F)
	case 3:
		r = rune(b&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
	case 4:
		r = rune(b&0x07)<<18 | rune(s[i+1]&0x3F)<<12 | rune(s[i+2]&0x3F)<<6 | rune(s[i+3]&0x3F)
	}
	return r, n
}

// ────────────────────────────────────────────────────────────────────────────
// Recursive Descent Parser
// ────────────────────────────────────────────────────────────────────────────

type parser struct {
	tokens []token
	pos    int
	nums   []int // integer literals encountered during parsing
}

func (p *parser) peek() token {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return token{tokEOF, 0}
}

func (p *parser) next() token {
	t := p.peek()
	p.pos++
	return t
}

// parseExpr → parseTerm ( ('+' | '-') parseTerm )*
func (p *parser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		tok := p.peek()
		if tok.kind != tokPlus && tok.kind != tokMinus {
			break
		}
		p.next()
		right, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if tok.kind == tokPlus {
			left += right
		} else {
			left -= right
		}
	}
	return left, nil
}

// parseTerm → parseFactor ( ('*' | '/') parseFactor )*
func (p *parser) parseTerm() (float64, error) {
	left, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		tok := p.peek()
		if tok.kind != tokMul && tok.kind != tokDiv {
			break
		}
		p.next()
		right, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		if tok.kind == tokMul {
			left *= right
		} else {
			if math.Abs(right) < 1e-12 {
				return 0, ErrDivisionByZero
			}
			left /= right
		}
	}
	return left, nil
}

// parseFactor → NUMBER | '(' parseExpr ')' | '-' parseFactor
func (p *parser) parseFactor() (float64, error) {
	tok := p.peek()

	switch tok.kind {
	case tokNum:
		p.next()
		p.nums = append(p.nums, tok.val)
		return float64(tok.val), nil

	case tokLParen:
		p.next()
		val, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.peek().kind != tokRParen {
			return 0, fmt.Errorf("%w: missing closing ')'", ErrInvalidExpr)
		}
		p.next()
		return val, nil

	case tokMinus:
		p.next()
		val, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		return -val, nil

	case tokPlus:
		// Unary +
		p.next()
		return p.parseFactor()

	default:
		return 0, fmt.Errorf("%w: unexpected token at position %d", ErrInvalidExpr, p.pos)
	}
}
