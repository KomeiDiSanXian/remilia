package wasm

// ── ULEB128 编解码 ──────────────────────────────────────────────────────────

// encodeULEB128 将 uint32 编码为无符号 LEB128 字节序列。
func encodeULEB128(v uint32) []byte {
	var b [5]byte
	i := 0
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		b[i] = c
		i++
		if v == 0 {
			break
		}
	}
	out := make([]byte, i)
	copy(out, b[:i])
	return out
}

// decodeULEB128 从字节序列解码一个无符号 LEB128 值，返回值和消耗的字节数。
func decodeULEB128(data []byte) (uint32, int) {
	var v uint32
	for i := range data {
		c := data[i]
		v |= uint32(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, 0
}

// ── TLV (Type-Length-Value) 编解码 ────────────────────────────────────────────
//
// 用于 WASM ABI 中宿主与模块之间的数据传输，替换 JSON 序列化。
// 格式：重复的 [key_len:ULEB128][key:bytes][val_len:ULEB128][val:bytes]
//
// 键名约定（单字节 ASCII 以减小体积）：
//   c — content (消息内容)
//   s — sender_id
//   p — platform
//   t — chat_type (group/private)
//   i — chat_id
//   e — event_id
//   r — reply (插件回复)
//   k — storage key
//   v — storage value / 通用值
//   E — error

// TLVBuilder 构建 TLV 字节序列。
type TLVBuilder struct {
	buf []byte
}

func NewTLVBuilder() *TLVBuilder {
	return &TLVBuilder{buf: make([]byte, 0, 256)}
}

func (b *TLVBuilder) Write(key string, val []byte) *TLVBuilder {
	b.buf = append(b.buf, encodeULEB128(uint32(len(key)))...)
	b.buf = append(b.buf, key...)
	b.buf = append(b.buf, encodeULEB128(uint32(len(val)))...)
	b.buf = append(b.buf, val...)
	return b
}

func (b *TLVBuilder) WriteString(key, val string) *TLVBuilder {
	return b.Write(key, []byte(val))
}

func (b *TLVBuilder) Bytes() []byte { return b.buf }

// TLVReader 从 TLV 字节序列中读取键值对。
type TLVReader struct {
	buf []byte
	pos int //nolint:unused
}

func NewTLVReader(data []byte) *TLVReader {
	return &TLVReader{buf: data}
}

// Read 读取指定键的值。返回 nil 表示键不存在。
func (r *TLVReader) Read(key string) []byte {
	pos := 0
	for pos < len(r.buf) {
		kLen, n := decodeULEB128(r.buf[pos:])
		if n == 0 {
			break
		}
		pos += n
		if pos+int(kLen) > len(r.buf) {
			break
		}
		k := string(r.buf[pos : pos+int(kLen)])
		pos += int(kLen)

		vLen, n := decodeULEB128(r.buf[pos:])
		if n == 0 {
			break
		}
		pos += n
		if pos+int(vLen) > len(r.buf) {
			break
		}
		v := r.buf[pos : pos+int(vLen)]
		pos += int(vLen)

		if k == key {
			return v
		}
	}
	return nil
}

func (r *TLVReader) ReadString(key string) string {
	v := r.Read(key)
	if v == nil {
		return ""
	}
	return string(v)
}
