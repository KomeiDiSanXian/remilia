# WASM 插件开发指南

> **最后更新**: 2026-05-20  
> **适用版本**: ABI v2

---

## 概述

Remilia 支持通过 **WebAssembly (WASM)** 运行第三方插件，实现语言无关的插件扩展。
WASM 插件在 **wazero** 沙箱中运行，与宿主进程内存隔离，通过明确定义的 ABI 进行通信。

### 架构示意

```
┌───────────────────────────────────────────────────┐
│                    Host (Go)                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │
│  │  Manager  │  │  Bridge  │  │ HostFuncRegistry │  │
│  │ (生命周期) │  │ (事件桥接) │  │ (宿主函数)       │  │
│  └─────┬────┘  └────┬─────┘  └────────┬─────────┘  │
│        │            │                  │            │
│  ┌─────▼────────────▼──────────────────▼──────────┐ │
│  │                Runtime (wazero)                 │ │
│  │   ┌─────────────────────────────────────────┐   │ │
│  │   │           WASM Sandbox                   │   │ │
│  │   │  ┌──────────────┐  ┌──────────────────┐  │   │ │
│  │   │  │ plugin_init   │  │ plugin_handle    │  │   │ │
│  │   │  │ plugin_abi_ver│  │ malloc (可选)    │  │   │ │
│  │   │  └──────────────┘  └──────────────────┘  │   │ │
│  │   └─────────────────────────────────────────┘   │ │
│  └─────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────┘
```

---

## ABI 合约

### WASM 模块必须导出

| 导出函数 | 签名 | 说明 |
|----------|------|------|
| `plugin_init` | `() → i32` | 初始化。返回 `0` 表示成功，非零表示错误 |
| `plugin_handle` | `(ptr: i32, len: i32) → (ptr: i32, len: i32)` | 处理事件。优先多值返回，回退 `i64` 编码。返回 `(0,0)` 表示无回复 |

### WASM 模块可选导出

| 导出函数 | 签名 | 说明 |
|----------|------|------|
| `malloc` | `(size: i32) → i32` | 宿主分配事件写入内存。未导出时使用固定通信区 |
| `plugin_abi_version` | `() → i32` | 声明插件兼容的 ABI 版本，当前为 `2` |
| `plugin_metadata` | `() → (ptr: i32, len: i32)` | TLV 编码的插件元数据 |

### 宿主模块导入

所有宿主函数通过模块 `remilia_host` 导入：

| 导入函数 | 签名 | 说明 |
|----------|------|------|
| `log` | `(ptr: i32, len: i32) → i64` | 日志输出 |
| `get_config` | `(ptr: i32, len: i32) → i64` | 读取插件配置（TLV 参数 `k`=key，TLV 返回值 `v`=value） |
| `storage_get` | `(ptr: i32, len: i32) → i64` | 读取持久化存储（TLV 参数 `k`=key，TLV 返回值 `v`=value） |
| `storage_set` | `(ptr: i32, len: i32) → i64` | 写入持久化存储（TLV 参数 `k`=key, `v`=value） |
| `__host_abi_version` | `(ptr: i32, len: i32) → i64` | 查询宿主 ABI 版本 |
| `__host_functions` | `(ptr: i32, len: i32) → i64` | 查询可用宿主函数列表（TLV 多键 `f`=function_name） |

### i64 返回值编码

当 WASM 运行时不支持多值返回时，使用 i64 编码：
```
i64 = (len << 32) | ptr
低32位 = ptr，高32位 = len
```

---

## TLV 序列化格式

所有宿主与插件之间的数据交换使用 **TLV**（Type-Length-Value）格式：

```
[ key_len:ULEB128 ][ key:bytes ][ val_len:ULEB128 ][ val:bytes ]
[ key_len:ULEB128 ][ key:bytes ][ val_len:ULEB128 ][ val:bytes ]
...
```

### 键名约定

| 键 | 用途 | 出现位置 |
|----|------|----------|
| `c` | content（消息内容） | 事件 → 插件 |
| `s` | sender_id | 事件 → 插件 |
| `p` | platform | 事件 → 插件 |
| `i` | chat_id | 事件 → 插件 |
| `t` | chat_type (`group`/`private`) | 事件 → 插件 |
| `e` | event_id | 事件 → 插件 |
| `r` | reply（回复文本） | 插件 → 宿主 |
| `k` | key（存储/配置键名） | 宿主函数参数 |
| `v` | value（存储/配置值） | 宿主函数参数/返回值 |
| `E` | error | 插件 → 宿主 |

---

## 开发 WASM 插件（TinyGo）

### 环境要求

- [TinyGo](https://tinygo.org/) 0.31+（推荐 0.34）
- Go 1.23.x（TinyGo 0.34 兼容版本）
- [binaryen](https://github.com/WebAssembly/binaryen)（`wasm-opt` 工具）

### 最小示例

```go
// main.go
package main

import "unsafe"

// 响应缓冲区（静态分配，避免 GC 回收）
var respBuf [4096]byte

// ── 宿主函数导入 ──────────────────────────────────────────────────────────

//go:wasmimport remilia_host log
func hostLog(ptr uint32, len uint32) uint64

// ── ABI 导出 ──────────────────────────────────────────────────────────────

//go:export plugin_abi_version
func abiVersion() int32 { return 2 }

//go:export plugin_init
func pluginInit() int32 { return 0 }

//go:export plugin_handle
func pluginHandle(ptr uint32, length uint32) uint64 {
    if length == 0 {
        return 0
    }

    // 读取 TLV 事件
    input := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
    content := tlvRead(input, "c")

    // 构建 TLV 响应
    reply := "收到: " + content
    respBuf[0] = 1          // key_len
    respBuf[1] = 'r'        // key: "r"
    respBuf[2] = byte(len(reply))
    copy(respBuf[3:], reply)
    respLen := uint32(3 + len(reply))

    respPtr := uint32(uintptr(unsafe.Pointer(&respBuf[0])))
    return uint64(respLen)<<32 | uint64(respPtr)
}

// ── TLV 读取 ─────────────────────────────────────────────────────────────

func tlvRead(data []byte, key string) string {
    i := 0
    for i < len(data) {
        kLen, n := decodeULEB128(data[i:])
        if n == 0 { break }
        i += n
        if i+int(kLen) > len(data) { break }
        k := string(data[i : i+int(kLen)])
        i += int(kLen)

        vLen, n := decodeULEB128(data[i:])
        if n == 0 { break }
        i += n
        if i+int(vLen) > len(data) { break }
        v := string(data[i : i+int(vLen)])
        i += int(vLen)

        if k == key { return v }
    }
    return ""
}

func decodeULEB128(data []byte) (uint32, int) {
    var v uint32
    for i := 0; i < len(data); i++ {
        c := data[i]
        v |= uint32(c&0x7f) << (7 * i)
        if c&0x80 == 0 { return v, i + 1 }
    }
    return 0, 0
}

func main() {}
```

### 构建

```bash
# 编译为 WASM
tinygo build -o plugin.wasm -target=wasi .

# 查看导出的函数
wasm-opt --print plugin.wasm | grep "export"
```

### 加载到 Remilia

```go
package main

import (
    "context"
    "os"

    "github.com/KomeiDiSanXian/remilia/plugin/wasm"
)

func main() {
    // 创建管理器
    mgr := wasm.NewManager(eng, nil) // eng = *engine.Engine

    // 注册 WASM 插件
    desc := &wasm.Descriptor{
        Name:    "myplugin",
        Version: "1.0.0",
        Path:    "./plugin.wasm",
        Commands: []wasm.CommandDef{
            {Command: "/hello"},
        },
        // 可选：自定义资源限制
        ResourceLimit: &wasm.ResourceLimit{
            MaxCallPerSec: 500,
            CallTimeout:   15 * time.Second,
        },
    }

    inst, err := mgr.Register(context.Background(), desc)
    if err != nil {
        panic(err)
    }

    // 查看运行时信息
    println(inst.Module.CallCount()) // 调用次数
    println(inst.Module.Uptime())    // 运行时间
    println(inst.Module.ABICompatible()) // ABI 兼容性
}
```

### 使用 `go:embed` 内嵌 WASM

```go
package main

import (
    _ "embed"
    "github.com/KomeiDiSanXian/remilia/plugin/wasm"
)

//go:embed plugin.wasm
var pluginWasm []byte

func loadWasmPlugin(mgr *wasm.Manager) {
    desc := &wasm.Descriptor{
        Name: "myplugin",
        Commands: []wasm.CommandDef{
            {Command: "/hello"},
        },
    }
    mgr.Instantiate(context.Background(), desc, pluginWasm)
}
```

---

## 开发 WASM 插件（其他语言）

### Rust

```rust
// Cargo.toml: [lib] crate-type = ["cdylib"]
// 构建: cargo build --target wasm32-wasi --release

extern "C" {
    fn log(ptr: u32, len: u32) -> u64;
}

#[no_mangle]
pub extern "C" fn plugin_abi_version() -> i32 { 2 }

#[no_mangle]
pub extern "C" fn plugin_init() -> i32 { 0 }

#[no_mangle]
pub extern "C" fn plugin_handle(ptr: u32, len: u32) -> u64 {
    // TLV 读取 + 处理
    0
}
```

### C

```c
__attribute__((import_module("remilia_host"), import_name("log")))
uint64_t log(uint32_t ptr, uint32_t len);

__attribute__((export_name("plugin_init")))
int32_t plugin_init(void) { return 0; }

__attribute__((export_name("plugin_handle")))
uint64_t plugin_handle(uint32_t ptr, uint32_t len) {
    // TLV 读取 + 处理
    return 0;
}
```

---

## 安全模型

| 安全机制 | 说明 | 默认值 |
|----------|------|--------|
| **内存隔离** | wazero 沙箱隔离，插件无法访问宿主内存 | — |
| **宿主函数白名单** | 插件只能调用 `remilia_host` 模块中注册的函数 | — |
| **WASI 隔离** | 文件系统、网络等需显式配置，默认不可用 | — |
| **调用限流** | 令牌桶限流，防止 DoS | 1000 次/秒 |
| **执行超时** | `plugin_handle` 超时自动终止 | 30 秒 |
| **初始化超时** | `plugin_init` / `_start` 超时自动终止 | 10 秒 |
| **响应大小上限** | 单次回复最大字节数 | 1 MB |
| **WASM 文件大小上限** | 拒绝过大的 `.wasm` 文件 | 50 MB |
| **导入数量上限** | 防止恶意模块声明大量导入 | 100 |

通过 `Descriptor.ResourceLimit` 可逐字段覆盖：

```go
desc := &wasm.Descriptor{
    Name: "myplugin",
    ResourceLimit: &wasm.ResourceLimit{
        MaxCallPerSec:    100,     // 降低限流
        CallTimeout:      5 * time.Second,
        ResponseSizeMax:  64 * 1024, // 64KB
        ImportsMax:       20,
    },
}
```

---

## 完整示例

查看 [examples/showcase](../../examples/showcase/) 获取完整的 WASM 插件演示：

- **TinyGo 插件源码**: `examples/showcase/wasm/main.go`
- **宿主集成代码**: `examples/showcase/wasm_demo.go`
- **支持的命令**: `/wasmhello` `/wasmping` `/wasmcount` `/wasmecho` `/wasmstore` `/wasmhost` `/wasm`

---

## 常见问题

### Q: 为什么不用 Go 标准库编译 WASM？

Go 的 `GOOS=wasip1 GOARCH=wasm` 编译的模块在 `_start` 后会调用 `proc_exit(0)` 关闭模块，
无法保持活跃用于多次调用。因此 WASM 插件必须使用 **TinyGo** 编译。

### Q: 为什么用 TLV 不用 JSON？

TLV 无 GC 分配、无需标准库、极简实现（约 20 行代码/语言），
插件二进制大小从 ~920KB 降到 ~172KB。

### Q: 插件可以访问网络吗？

默认不可以。WASI 的网络功能默认不开放。需要走宿主提供的 `http_request` 等宿主函数。

### Q: 如何调试 WASM 插件？

通过导入的 `log` 宿主函数输出日志，日志格式为 `[wasm/level] message`。
