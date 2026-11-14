// Package jsonx 是仓库内 JSON 的唯一入口，封装 Go 1.27 的 encoding/json/v2。
//
// 为什么需要这一层：
//
//   - 语义口径集中在一处。v1（encoding/json）与 v2（encoding/json/v2）在
//     omitempty、大小写匹配、重复键、非法 UTF-8、nil 切片/映射、[N]byte、
//     time.Duration 等十余处行为不同，散落在调用点各自写 Options 迟早会漂移。
//   - 禁止 v1/v2 混用。同一次 Marshal 的产物若来自不同语义，出站字节、落盘
//     文件与外部 payload 的兼容性都会变得不可推理；仓库里不允许 import
//     encoding/json。少数需要流式 API 的位置（SSE、Encoder/Decoder）直接用
//     jsontext，但选项必须取自 [Options]，不要自行拼装。
//   - time.Duration 在 v2 中没有默认表示（v1 是纳秒数字），直接 Marshal 会
//     报 "no default representation"。本包注册了纳秒数字编解码器，保持既有
//     JSON 线格式不变。
//
// 语义基线：v2 默认值（严格模式），仅额外固定两项非兼容性选项：
//
//	Deterministic(true)  map 按键排序，保证同样输入产出同样字节
//	time.Duration        编解码为 JSON 数字（纳秒）
//
// 与 v1 的差异（迁移时需知）：
//
//   - omitempty 只省略编码结果为 null/""/{}/[] 的字段，bool 与数值的 0/false
//     不再被省略——这些字段应改用 omitzero。
//   - 结构体字段名大小写敏感；重复键、非法 UTF-8 与尾随内容直接报错。
//   - nil 切片/映射编码为 []/{}；[N]byte 编码为 Base64 字符串。
//   - 字符串不再转义 HTML 字符（<、>、&）。
//   - 解码到 any 时数字仍是 float64；需要保留数字原文请用 [Number] 或
//     [RawMessage]。
package jsonx

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"strconv"
	"time"
)

// options 是本包所有编解码函数共享的选项集。
//
// 调用方需要额外选项时以可变参数追加：后出现的选项覆盖此处的设置，
// 因此可以按单次调用临时放宽（例如 offline 工具读取历史文件时开启
// jsontext.AllowDuplicateNames(true)）。
var options = jsonv2.JoinOptions(
	jsonv2.Deterministic(true),
	jsonv2.WithMarshalers(jsonv2.JoinMarshalers(
		jsonv2.MarshalFunc(marshalDuration),
	)),
	jsonv2.WithUnmarshalers(jsonv2.JoinUnmarshalers(
		jsonv2.UnmarshalFunc(unmarshalDuration),
	)),
)

// Options 返回本包默认选项集的副本，供需要自行构造 Encoder/Decoder 的
// 少数场景使用（例如 SSE 流式编码）。调用方不应修改其行为，只能追加。
func Options() jsonv2.Options { return options }

// CaseInsensitiveNames 放宽结构体字段名匹配（v1 的历史行为）。
//
// 只在读取历史文件、兼容外部脏数据时使用；新代码保持默认的大小写敏感，
// 否则字段名写错也会静默解到零值。
func CaseInsensitiveNames() jsonv2.Options { return jsonv2.MatchCaseInsensitiveNames(true) }

// marshalDuration 把 time.Duration 编码为纳秒整数，与 v1 表示一致。
func marshalDuration(d time.Duration) ([]byte, error) {
	return strconv.AppendInt(nil, int64(d), 10), nil
}

// unmarshalDuration 从 JSON 数字解析 time.Duration（纳秒）。
// 同时接受字符串形式（"1s"），兼容人工编辑过的状态文件；
// null 按 v1 语义保留原值（解码成功但不清零）。
func unmarshalDuration(src []byte, d *time.Duration) error {
	if string(src) == "null" {
		return nil
	}
	if len(src) > 0 && src[0] == '"' {
		unquoted, err := jsontext.AppendUnquote(nil, src)
		if err != nil {
			return err
		}
		parsed, err := time.ParseDuration(string(unquoted))
		if err != nil {
			return err
		}
		*d = parsed
		return nil
	}
	n, err := strconv.ParseInt(string(src), 10, 64)
	if err != nil {
		return err
	}
	*d = time.Duration(n)
	return nil
}

// Marshal 把 v 编码为 JSON，不追加换行。
func Marshal(v any, opts ...jsonv2.Options) ([]byte, error) {
	return jsonv2.Marshal(v, append([]jsonv2.Options{options}, opts...)...)
}

// MarshalIndent 把 v 编码为带缩进的 JSON，不追加换行。
// prefix 加在每行前，indent 用于逐层缩进（与 encoding/json.MarshalIndent 一致）。
func MarshalIndent(v any, prefix, indent string, opts ...jsonv2.Options) ([]byte, error) {
	all := append([]jsonv2.Options{
		options,
		jsontext.WithIndentPrefix(prefix),
		jsontext.WithIndent(indent),
	}, opts...)
	return jsonv2.Marshal(v, all...)
}

// MarshalWrite 把 v 编码后写入 w，不追加换行。
func MarshalWrite(w io.Writer, v any, opts ...jsonv2.Options) error {
	return jsonv2.MarshalWrite(w, v, append([]jsonv2.Options{options}, opts...)...)
}

// Unmarshal 把 JSON 文本解码到 out（必须是非 nil 指针）。
func Unmarshal(data []byte, out any, opts ...jsonv2.Options) error {
	return jsonv2.Unmarshal(data, out, append([]jsonv2.Options{options}, opts...)...)
}

// UnmarshalRead 从 r 读取**单个完整** JSON 值并解码到 out，读到 EOF 不报错。
//
// 与 encoding/json.Decoder.Decode 的差别：尾部若还有非空白内容会报错。
// 需要逐个读取流式多值时请用 [Decoder]。
func UnmarshalRead(r io.Reader, out any, opts ...jsonv2.Options) error {
	return jsonv2.UnmarshalRead(r, out, append([]jsonv2.Options{options}, opts...)...)
}

// MarshalEncode 把 v 编码进已构造的 jsontext.Encoder（流式场景，例如 SSE）。
func MarshalEncode(enc *jsontext.Encoder, v any, opts ...jsonv2.Options) error {
	return jsonv2.MarshalEncode(enc, v, append([]jsonv2.Options{options}, opts...)...)
}

// UnmarshalDecode 从已构造的 jsontext.Decoder 解出下一个 JSON 值。
func UnmarshalDecode(dec *jsontext.Decoder, out any, opts ...jsonv2.Options) error {
	return jsonv2.UnmarshalDecode(dec, out, append([]jsonv2.Options{options}, opts...)...)
}

// Valid 报告 data 是否为单个合法 JSON 值。
func Valid(data []byte) bool {
	return jsontext.Value(data).IsValid()
}

// RawMessage 是未经解码的 JSON 片段，语义与 v1 的 json.RawMessage 一致。
// 类型别名指向 jsontext.Value，因此两者可以互相赋值。
type RawMessage = jsontext.Value

// Number 是 JSON 数字的文本形式，用于在不损失精度的情况下传递数字。
//
// 与 v1 的 json.Number 等价；v2 默认把数字解到 any 时得到 float64，
// 需要保留原文（尤其是超长整数 ID）时显式使用本类型。
type Number string

// String 返回数字原文。
func (n Number) String() string { return string(n) }

// Float64 把数字解析为 float64。
func (n Number) Float64() (float64, error) { return strconv.ParseFloat(string(n), 64) }

// Int64 把数字解析为 int64。
func (n Number) Int64() (int64, error) { return strconv.ParseInt(string(n), 10, 64) }

// MarshalJSON 输出数字原文；原文不是合法 JSON 数字时报错。
func (n Number) MarshalJSON() ([]byte, error) {
	if !jsontext.Value(n).IsValid() {
		return nil, errors.New("jsonx: invalid JSON number " + strconv.Quote(string(n)))
	}
	return []byte(n), nil
}

// UnmarshalJSON 保存数字原文；非数字输入报错。
func (n *Number) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || (data[0] != '-' && (data[0] < '0' || data[0] > '9')) {
		return errors.New("jsonx: cannot unmarshal non-number into Number")
	}
	*n = Number(data)
	return nil
}

// Decoder 逐个读取 JSON 值，语义与 encoding/json.Decoder 一致（尾部内容不影响），
// 适合处理同一 Reader 上的连续 JSON 值。
type Decoder struct {
	dec *jsontext.Decoder
}

// NewDecoder 返回从 r 读取的 Decoder。
func NewDecoder(r io.Reader, opts ...jsonv2.Options) *Decoder {
	// jsontext 对 *bytes.Buffer 有"边读边写"优化，先藏起来避免误用。
	if _, ok := r.(*bytes.Buffer); ok {
		r = struct{ io.Reader }{r}
	}
	return &Decoder{dec: jsontext.NewDecoder(r, append([]jsonv2.Options{options}, opts...)...)}
}

// Decode 读取下一个 JSON 值并解码到 v；没有更多值时返回 io.EOF。
func (d *Decoder) Decode(v any) error {
	return jsonv2.UnmarshalDecode(d.dec, v)
}

// Encoder 把值逐个写入 w，每次 Encode 追加换行（与 encoding/json.Encoder 一致）。
type Encoder struct {
	enc *jsontext.Encoder
}

// NewEncoder 返回写入 w 的 Encoder。
func NewEncoder(w io.Writer, opts ...jsonv2.Options) *Encoder {
	return &Encoder{enc: jsontext.NewEncoder(w, append([]jsonv2.Options{options}, opts...)...)}
}

// Encode 把 v 编码写入底层 Writer 并追加换行。
func (e *Encoder) Encode(v any) error {
	return jsonv2.MarshalEncode(e.enc, v)
}
