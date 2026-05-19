package main

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugin/wasm"
)

//go:embed demo.wasm
var demoWasm []byte

// wasmDemo 持有 WASM 演示相关状态。
type wasmDemo struct {
	manager *wasm.Manager
}

// setupWasmDemo 配置 WASM 插件演示，使用自定义宿主函数（storage、config 等）。
func setupWasmDemo(pm *plugin.Manager, eng *engine.Engine) *wasmDemo {
	demo := &wasmDemo{}

	// 创建带有增强宿主函数的注册表
	reg := wasm.NewHostFuncRegistry()
	wasm.RegisterDefaultHostFuncs(reg)

	// 内存存储
	store := &memoryStore{data: make(map[string]string)}

	// 增强 get_config — 返回宿主指纹等信息
	reg.Register("get_config", func(args []byte) ([]byte, error) {
		key := wasm.NewTLVReader(args).ReadString("k")
		var val string
		switch key {
		case "host_info":
			val = "Remilia Showcase / Go 1.26 / wazero"
		case "plugin_name":
			val = "showcase-wasm"
		default:
			val = "unknown_key"
		}
		return wasm.NewTLVBuilder().WriteString("v", val).Bytes(), nil
	})

	// storage_get — 从内存存储读取
	reg.Register("storage_get", func(args []byte) ([]byte, error) {
		key := wasm.NewTLVReader(args).ReadString("k")
		val, _ := store.Get(key)
		return wasm.NewTLVBuilder().WriteString("v", val).Bytes(), nil
	})

	// storage_set — 写入内存存储
	reg.Register("storage_set", func(args []byte) ([]byte, error) {
		key := wasm.NewTLVReader(args).ReadString("k")
		val := wasm.NewTLVReader(args).ReadString("v")
		store.Set(key, val)
		logger.Infof("[wasm/storage] set %q = %q", key, val)
		return nil, nil
	})

	mgr := wasm.NewManager(eng, reg)
	demo.manager = mgr

	desc := &wasm.Descriptor{
		Name:    "showcase-wasm",
		Version: "1.0.0",
		Commands: []wasm.CommandDef{
			{EventType: "", Command: "/wasmhello"},
			{EventType: "", Command: "/wasmping"},
			{EventType: "", Command: "/wasmcount"},
			{EventType: "", Command: "/wasmecho"},
			{EventType: "", Command: "/wasmstore"},
			{EventType: "", Command: "/wasmhost"},
		},
	}

	inst, err := mgr.Instantiate(context.Background(), desc, demoWasm)
	if err != nil {
		logger.Warnf("[wasm] demo setup failed: %v", err)
		return demo
	}
	logger.Infof("[wasm] demo plugin loaded: %s (calls=%d)", inst.Desc.Name, inst.Module.CallCount())

	registerWasmCommands(pm, demo)

	return demo
}

func registerWasmCommands(pm *plugin.Manager, demo *wasmDemo) {
	_ = pm.Register(&plugin.Descriptor{
		Name:    "wasm-status",
		Version: "1.0.0",
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Reg.RegisterCommand("", "/wasm").Handle(func(c *eventctx.Context) error {
				mgr := demo.manager
				names := mgr.List()
				if len(names) == 0 {
					return replyCtx(c, "没有已加载的 WASM 插件")
				}

				var b strings.Builder
				b.WriteString(fmt.Sprintf("WASM 插件 (%d):\n", len(names)))
				for _, name := range names {
					inst := mgr.Get(name)
					if inst == nil || inst.Module == nil {
						continue
					}
					b.WriteString(fmt.Sprintf(
						"  %s v%s\n    calls=%d  uptime=%v\n",
						inst.Desc.Name,
						inst.Desc.Version,
						inst.Module.CallCount(),
						inst.Module.Uptime().Round(1e9),
					))
				}
				b.WriteString("\n/wasmhello  问候\n/wasmcount  调用计数\n/wasmecho   回显\n/wasmstore  存储演示\n/wasmhost   宿主信息\n/wasmreload 重载")
				return replyCtx(c, b.String())
			})

			ctx.Reg.RegisterCommand("", "/wasmreload").Handle(func(c *eventctx.Context) error {
				if err := demo.manager.Unregister(context.Background(), "showcase-wasm"); err != nil {
					return replyCtx(c, fmt.Sprintf("卸载失败: %v", err))
				}

				desc := &wasm.Descriptor{
					Name:    "showcase-wasm",
					Version: "1.0.0",
					Commands: []wasm.CommandDef{
						{EventType: "", Command: "/wasmhello"},
						{EventType: "", Command: "/wasmping"},
						{EventType: "", Command: "/wasmcount"},
						{EventType: "", Command: "/wasmecho"},
						{EventType: "", Command: "/wasmstore"},
						{EventType: "", Command: "/wasmhost"},
					},
				}
				inst, err := demo.manager.Instantiate(context.Background(), desc, demoWasm)
				if err != nil {
					return replyCtx(c, fmt.Sprintf("重载失败: %v", err))
				}
				return replyCtx(c, fmt.Sprintf("WASM 插件已重载 (calls=%d)", inst.Module.CallCount()))
			})
			return nil, nil
		},
	})
}

// ── 内存存储（宿主侧 KV 存储） ────────────────────────────────────────────────

type memoryStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func (s *memoryStore) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok
}

func (s *memoryStore) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}
