package jsonx

import (
	"bytes"
	"io"
	"testing"
	"time"

	"encoding/json/jsontext"
)

func TestMarshalDeterministic(t *testing.T) {
	v := map[string]int{"b": 2, "a": 1, "c": 3}
	for range 10 {
		got, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if want := `{"a":1,"b":2,"c":3}`; string(got) != want {
			t.Fatalf("Marshal = %s, want %s", got, want)
		}
	}
}

func TestMarshalIndent(t *testing.T) {
	got, err := MarshalIndent(map[string]any{"a": []int{1, 2}}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	want := "{\n  \"a\": [\n    1,\n    2\n  ]\n}"
	if string(got) != want {
		t.Fatalf("MarshalIndent = %q, want %q", got, want)
	}
}

func TestDurationRoundTrip(t *testing.T) {
	type carrier struct {
		D time.Duration            `json:"d"`
		M map[string]time.Duration `json:"m"`
	}
	in := carrier{D: 1500 * time.Millisecond, M: map[string]time.Duration{"a": time.Second}}

	data, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// 与 encoding/json（v1）的纳秒数字线格式一致
	if want := `{"d":1500000000,"m":{"a":1000000000}}`; string(data) != want {
		t.Fatalf("Marshal = %s, want %s", data, want)
	}

	var out carrier
	if err := Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.D != in.D || out.M["a"] != in.M["a"] {
		t.Fatalf("round trip = %+v, want %+v", out, in)
	}

	// 人工编辑过的状态文件可能写成 "1s"
	out = carrier{}
	if err := Unmarshal([]byte(`{"d":"2s"}`), &out); err != nil {
		t.Fatalf("Unmarshal string duration: %v", err)
	}
	if out.D != 2*time.Second {
		t.Fatalf("string duration = %v, want 2s", out.D)
	}

	// null 不清零（与 v1 一致）：v2 默认会把 null 解成零值
	preset := carrier{D: 5 * time.Second}
	if err := Unmarshal([]byte(`{"d":null}`), &preset); err != nil {
		t.Fatalf("Unmarshal null duration: %v", err)
	}
	if preset.D != 5*time.Second {
		t.Fatalf("null duration = %v, want 5s", preset.D)
	}
}

func TestOmitZeroOmitsScalarZeroValues(t *testing.T) {
	type payload struct {
		N int    `json:"n,omitzero"`
		B bool   `json:"b,omitzero"`
		S string `json:"s,omitzero"`
	}
	got, err := Marshal(payload{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := `{}`; string(got) != want {
		t.Fatalf("Marshal = %s, want %s", got, want)
	}
}

func TestOmitEmptyKeepsScalarZeroValues(t *testing.T) {
	// v2 的 omitempty 只看编码结果是否为空 JSON：0 与 false 不再被省略。
	// 这是本次迁移把 bool/数值字段改成 omitzero 的原因，测试用来锁住预期。
	type payload struct {
		N int    `json:"n,omitempty"`
		B bool   `json:"b,omitempty"`
		S string `json:"s,omitempty"`
	}
	got, err := Marshal(payload{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := `{"n":0,"b":false}`; string(got) != want {
		t.Fatalf("Marshal = %s, want %s", got, want)
	}
}

func TestCaseInsensitiveNames(t *testing.T) {
	type payload struct {
		FooBar string `json:"foo_bar"`
	}
	var out payload
	if err := Unmarshal([]byte(`{"FooBar":"x"}`), &out, CaseInsensitiveNames()); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.FooBar != "x" {
		t.Fatalf("case-insensitive match failed: %+v", out)
	}
}

func TestDecoderStream(t *testing.T) {
	dec := NewDecoder(bytes.NewBufferString(`{"a":1} {"b":2}`))
	var first, second map[string]int
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("Decode first: %v", err)
	}
	if err := dec.Decode(&second); err != nil {
		t.Fatalf("Decode second: %v", err)
	}
	if first["a"] != 1 || second["b"] != 2 {
		t.Fatalf("decoded = %v %v", first, second)
	}
	if err := dec.Decode(&second); err != io.EOF {
		t.Fatalf("Decode after EOF = %v, want io.EOF", err)
	}
}

func TestEncoderAppendsNewline(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if want := "{\"a\":1}\n"; buf.String() != want {
		t.Fatalf("Encode = %q, want %q", buf.String(), want)
	}
}

func TestUnmarshalReadRejectsTrailing(t *testing.T) {
	var v map[string]int
	err := UnmarshalRead(bytes.NewBufferString(`{"a":1} {"b":2}`), &v)
	if err == nil {
		t.Fatalf("UnmarshalRead accepted trailing content: %v", v)
	}
	if err := UnmarshalRead(bytes.NewBufferString(`{"a":1}`), &v); err != nil {
		t.Fatalf("UnmarshalRead single value: %v", err)
	}
}

func TestValid(t *testing.T) {
	cases := map[string]bool{
		`{"a":1}`:       true,
		`{"a":1,"a":2}`: false, // 重复键在严格语义下非法
		`{"a":1} {}`:    false,
		``:              false,
	}
	for input, want := range cases {
		if got := Valid([]byte(input)); got != want {
			t.Errorf("Valid(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestNumber(t *testing.T) {
	var n Number
	if err := Unmarshal([]byte(`1234567890123456789`), &n); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if n.String() != "1234567890123456789" {
		t.Fatalf("Number = %q", n.String())
	}
	if _, err := n.Int64(); err != nil {
		t.Fatalf("Int64: %v", err)
	}
	if err := Unmarshal([]byte(`"abc"`), &n); err == nil {
		t.Fatal("Unmarshal accepted non-number")
	}

	data, err := Marshal(n)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(data) != "1234567890123456789" {
		t.Fatalf("Marshal = %s", data)
	}
}

func TestStrictSemantics(t *testing.T) {
	// 大小写敏感：v1 的宽松匹配在 v2 默认关闭
	var s struct {
		FooBar string `json:"foo_bar"`
	}
	if err := Unmarshal([]byte(`{"FooBar":"x"}`), &s); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if s.FooBar != "" {
		t.Fatalf("case-insensitive match in strict mode: %+v", s)
	}

	// 重复键报错
	var m map[string]int
	if err := Unmarshal([]byte(`{"a":1,"a":2}`), &m); err == nil {
		t.Fatal("duplicate member accepted")
	}

	// 非法 UTF-8 报错
	if err := Unmarshal([]byte("{\"a\":\"\xff\"}"), &m); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}

func TestNilCollections(t *testing.T) {
	type payload struct {
		S []int          `json:"s"`
		M map[string]int `json:"m"`
	}
	got, err := Marshal(payload{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// v2 语义：nil 切片/映射编码为空容器，不再输出 null
	if want := `{"s":[],"m":{}}`; string(got) != want {
		t.Fatalf("Marshal = %s, want %s", got, want)
	}
}

func TestRawMessageAlias(t *testing.T) {
	var v struct {
		X RawMessage `json:"x"`
	}
	if err := Unmarshal([]byte(`{"x":{"a":1}}`), &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// RawMessage 是 jsontext.Value 的别名，可直接交给 jsontext API
	if !jsontext.Value(v.X).IsValid() {
		t.Fatalf("RawMessage is not valid JSON: %s", v.X)
	}
}
