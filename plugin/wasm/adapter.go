package wasm

import (
	"context"
	"fmt"
	"maps"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// ToDescriptor 将 WASM 插件描述符包装为标准 plugin.Descriptor，
// 使 WASM 插件可注册到 plugin.Manager，参与统一生命周期和依赖注入。
//
// 优势：
//   - WASM 插件可通过 Deps 字段声明对 Go 插件的依赖
//   - WASM 插件可通过 ctx.Config 接收 YAML 配置
//   - WASM 插件参与 StartAll/StopAll 生命周期管理
//   - WASM 插件的命令注册也参与 DryRun 依赖推断
//
// 使用示例：
//
//	wasmBytes, _ := os.ReadFile("plugins/myplugin.wasm")
//	pm.Register(wasm.ToDescriptor(&wasm.Descriptor{
//	    Name:    "mywasm",
//	    Version: "1.0.0",
//	    Deps:    []string{"storage"},
//	    Commands: []wasm.CommandDef{{Command: "/hello"}},
//	}, wasmBytes, nil))
//
// hostRegistry 为 nil 时使用默认宿主函数集。
func ToDescriptor(wdesc *Descriptor, wasmBytes []byte, hostRegistry *HostFuncRegistry) *plugin.Descriptor {
	if wdesc.Name == "" {
		panic("wasm: Descriptor.Name is required for ToDescriptor")
	}

	return &plugin.Descriptor{
		Name:    wdesc.Name,
		Version: wdesc.Version,
		Deps:    wdesc.Deps,
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			if ctx.DryRun {
				return nil, nil
			}

			// 先构建沙箱，把内存页限制传给独占运行时，使 ResourceLimit.MemoryPages 真正生效
			sandbox := NewSandbox(wdesc.EffectiveResourceLimit())
			rt, err := NewRuntime(context.Background(), hostRegistry, sandbox.memoryPages)
			if err != nil {
				return nil, fmt.Errorf("wasm(%q): runtime init: %w", wdesc.Name, err)
			}

			mod, err := rt.InstantiateModule(context.Background(), wdesc.Name, wasmBytes, sandbox)
			if err != nil {
				rt.Close(context.Background())
				return nil, fmt.Errorf("wasm(%q): instantiate: %w", wdesc.Name, err)
			}

			cfgMap := make(map[string]any)
			if ctx.Config != nil {
				cfgMap = ctx.Config.GetAll()
			}
			maps.Copy(cfgMap, wdesc.Config)
			rt.SetModuleConfig(wdesc.Name, cfgMap)

			for _, cmd := range wdesc.Commands {
				// 注意：不要在这里调用 matcher.SetGroup("wasm:"+name)。
				// ctx.Reg.RegisterCommand 已通过 engine.SetMatcherGroup 把 matcher
				// 正确索引到插件名分组下；raw SetGroup 只改 matcher 字段、不更新
				// engine 的 groupIndex，会导致卸载时 RemoveGroup(插件名) 一个都删
				// 不掉——已 Close 的模块留下幽灵命令持续报错（历史 bug）。
				matcher := ctx.Reg.RegisterCommand(cmd.EventType, cmd.Command)

				modRef := mod
				matcher.Handle(func(ectx *corectx.Context) error {
					eventTLV := NewTLVBuilder().
						WriteString("c", ectx.GetMessageContent()).
						WriteString("s", ectx.GetSenderID()).
						WriteString("p", ectx.GetEventPlatform()).
						WriteString("i", ectx.GetChatInfo().ID).
						WriteString("t", chatTypeString(ectx.GetChatInfo().IsGroup)).
						WriteString("e", ectx.GetPlatformEvent().ID()).
						Bytes()

					to := modRef.callTimeout()
					callCtx, cancel := context.WithTimeout(context.Background(), to)
					defer cancel()

					respTLV, err := modRef.CallHandle(callCtx, eventTLV)
					if err != nil {
						ectx.Reply(platform.TextMessage(fmt.Sprintf("插件执行错误: %v", err)))
						return nil
					}
					if respTLV != nil {
						if reply := NewTLVReader(respTLV).ReadString("r"); reply != "" {
							ectx.Reply(platform.TextMessage(reply))
						}
					}
					return nil
				})
			}

			if err := mod.CallInit(context.Background()); err != nil {
				mod.Close(context.Background())
				rt.Close(context.Background())
				return nil, fmt.Errorf("wasm(%q): init: %w", wdesc.Name, err)
			}

			return mod, nil
		},
		Teardown: func(tctx *plugin.TeardownContext) error {
			if mod, ok := tctx.API.(*Module); ok {
				mod.Close(context.Background())
				// ToDescriptor 为每个插件创建独占的 Runtime，
				// 必须一并关闭并清理配置条目，否则每次重载泄漏一个 wazero Runtime。
				if mod.runtime != nil {
					mod.runtime.RemoveModuleConfig(mod.name)
					if err := mod.runtime.Close(context.Background()); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
}
