package wasm_test

// ── WASM 二进制模块生成器 ────────────────────────────────────────────────────
//
// 生成最小有效的 WASM 模块用于测试，避免对外部编译工具的依赖。
// 生成的模块符合 plugin/wasm ABI 约定：
//   - 导出 plugin_init, plugin_handle, malloc
//   - 导入 remilia_host.log

// newTestWasmModule 生成最小 WASM 模块二进制。
//
// plugin_init()    → 返回 0（成功）
// plugin_handle(ptr, len) → 调用 remilia_host.log 记录事件，返回 0（无回复）
// malloc(size)     → 固定返回 1024（简单 bump 偏移）
func newTestWasmModule() []byte {
	w := &wasmWriter{}
	w.magic()

	// ── Type section (id=1) ──
	// 3 个函数类型:
	//   [0] () → i32       (plugin_init)
	//   [1] (i32,i32)→i64  (log 导入 + plugin_handle)
	//   [2] (i32)→i32      (malloc)
	wt := &wasmWriter{}
	wt.u32(3)
	// type[0]: () → i32
	wt.byte(0x60, 0x00, 0x01, 0x7f)
	// type[1]: (i32,i32) → i64
	wt.byte(0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e)
	// type[2]: (i32) → i32
	wt.byte(0x60, 0x01, 0x7f, 0x01, 0x7f)
	w.section(1, wt.buf)

	// ── Import section (id=2) ──
	// 导入 remilia_host.log 使用 type[1] 签名
	wi := &wasmWriter{}
	wi.u32(1)
	wi.string("remilia_host")
	wi.string("log")
	wi.byte(0x00) // import kind: func
	wi.u32(1)     // type index 1
	w.section(2, wi.buf)

	// ── Function section (id=3) ──
	// 3 个函数体分别对应 type[0],type[1],type[2]
	// 注意：导入的 log 是 func index 0，本地函数从 index 1 开始
	wf := &wasmWriter{}
	wf.u32(3) // 3 个函数
	wf.u32(0) // func[0]: type 0 (plugin_init → 导出 index 1)
	wf.u32(1) // func[1]: type 1 (plugin_handle → 导出 index 2)
	wf.u32(2) // func[2]: type 2 (malloc → 导出 index 3)
	w.section(3, wf.buf)

	// ── Memory section (id=5) ──
	// 1 个线性内存，最小 1 页 (64KB)
	wm := &wasmWriter{}
	wm.u32(1)
	wm.byte(0x00) // flags: no max
	wm.u32(1)     // min: 1 page
	w.section(5, wm.buf)

	// ── Export section (id=7) ──
	// 导出 plugin_init(index 1), plugin_handle(index 2), malloc(index 3)
	we := &wasmWriter{}
	we.u32(3)
	we.exportEntry("plugin_init", 1)
	we.exportEntry("plugin_handle", 2)
	we.exportEntry("malloc", 3)
	w.section(7, we.buf)

	// ── Code section (id=10) ──
	wc := &wasmWriter{}
	wc.u32(3) // 3 个函数体

	// body[0]: plugin_init → i32.const 0; end
	// locals: 0; body_size = 1 + 3 = 4
	wc.u32(4)           // func body size
	wc.u32(0)           // 0 locals groups
	wc.byte(0x41, 0x00) // i32.const 0
	wc.byte(0x0b)       // end

	// body[1]: plugin_handle → local.get 0; local.get 1; call 0(log); drop; i64.const 0; end
	// locals: 0; body_size = 1 + 10 = 11
	wc.u32(11)          // func body size
	wc.u32(0)           // 0 locals groups
	wc.byte(0x20, 0x00) // local.get 0
	wc.byte(0x20, 0x01) // local.get 1
	wc.byte(0x10, 0x00) // call 0 (log)
	wc.byte(0x1a)       // drop
	wc.byte(0x42, 0x00) // i64.const 0
	wc.byte(0x0b)       // end

	// body[2]: malloc → i32.const 1024; end
	// locals: 0; body_size = 1 + 4 = 5
	wc.u32(5)     // func body size
	wc.u32(0)     // 0 locals groups
	wc.byte(0x41) // i32.const
	wc.u32(1024)  // 1024
	wc.byte(0x0b) // end

	w.section(10, wc.buf)

	return w.buf
}

// ── WASM 二进制写入器 ────────────────────────────────────────────────────────

type wasmWriter struct {
	buf []byte
}

func (w *wasmWriter) byte(b ...byte) {
	w.buf = append(w.buf, b...)
}

// u32 写入一个无符号 LEB128 编码的 uint32。
func (w *wasmWriter) u32(v uint32) {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		w.buf = append(w.buf, c)
		if v == 0 {
			break
		}
	}
}

func (w *wasmWriter) string(s string) {
	w.u32(uint32(len(s)))
	w.buf = append(w.buf, []byte(s)...)
}

func (w *wasmWriter) magic() {
	w.buf = append(w.buf,
		0x00, 0x61, 0x73, 0x6d, // \0asm
		0x01, 0x00, 0x00, 0x00, // version 1
	)
}

func (w *wasmWriter) section(id byte, content []byte) {
	w.byte(id)
	w.u32(uint32(len(content)))
	w.buf = append(w.buf, content...)
}

func (w *wasmWriter) exportEntry(name string, funcIdx uint32) {
	w.string(name)
	w.byte(0x00) // export kind: func
	w.u32(funcIdx)
}
