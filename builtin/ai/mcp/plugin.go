// Package mcp 把外部工具服务器（Model Context Protocol）接入为 Remilia AI 的
// 工具来源。
//
// 本子系统自持其配置（见 config.go）与生命周期：连接、重连、工具列表刷新、
// 安全边界（command/env 白名单、SSRF/TLS 防护）全部在此完成。对 AI 而言它只是
// 一个 [toolkit.ToolProvider]：AI 通过既有的工具发现机制拉取工具，运行时变化
// 经 [toolkit.ToolChangeNotifier] 通知目录重新同步。
//
// 安全：外部服务器是不可信对端；其工具注解只作策略推导的输入建议，权威策略
// 仍是 Remilia 的动作策略（审批/权限），且所有调用都走 AI 的选择与放行管线。
//
// 执行路径：MCP 工具通过在工具上绑定的富结果回调执行，经 AI 既有的
// FuncInvoker 统一调度——`runtime`/`decision` 因此完全不依赖本包，不存在
// "AI 直连 MCP client"的第二条调用路径。策略闸门（目录选择 → ActionPolicy →
// 审批/RBAC → Invoker）对内置、命令、技能与外部来源一视同仁：本包只把
// "怎么连、怎么调"收敛在内部，绝不自行决定"能不能调"。
package mcp

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Plugin 外部工具服务器插件的服务对象。
type Plugin struct {
	cfg *Config
	mgr *Manager
}

// New 创建外部工具服务器插件的描述符。
//
// 插件服务实现 [toolkit.ToolProvider] 与 [toolkit.ToolChangeNotifier]，
// 由 AI 插件在容器冻结后自动发现。未配置任何服务器时为零工具来源（无副作用）。
func New() *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "mcp",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "接入外部工具服务器（Model Context Protocol），为 AI 提供扩展工具",
			Category:    "功能",
			Tags:        []string{"ai", "mcp", "工具", "外部"},
			HelpText: `外部工具服务器（MCP）— 把外部进程/服务提供的工具接入 AI。

配置（config.yaml 的 plugins.mcp 节）示例：
  plugins:
    mcp:
      allowed_commands: ["npx", "/usr/local/bin/my-mcp-server"]
      servers:
        - name: files
          transport: stdio
          command: npx
          args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
          require_approval: true
        - name: remote
          transport: http
          url: https://tools.example.com/mcp
          headers:
            Authorization: "Bearer ${MCP_TOKEN}"

安全说明：
  - 外部服务器视为不可信对端；其声明的工具默认需要审批。
  - stdio 命令必须在 allowed_commands 白名单内；只透传白名单环境变量。
  - http 地址强制 https（回环地址可显式放开），连接前做 SSRF 校验。`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			cfg, err := Load(ctx.Config.GetAll())
			if err != nil {
				return nil, err
			}
			mgr, err := NewManager(cfg)
			if err != nil {
				return nil, err
			}
			p := &Plugin{cfg: cfg, mgr: mgr}
			// 与插件生命周期绑定：框架在 Teardown 前取消 context，连接随之关闭。
			ctx.Spawn(func(runCtx context.Context) {
				mgr.Start(runCtx)
				<-runCtx.Done()
				mgr.Close()
			})
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			if p, ok := ctx.API.(*Plugin); ok {
				p.mgr.Close()
			}
			return nil
		},
	}
}

// ListTools 返回当前可暴露的工具（实现 [toolkit.ToolProvider]）。
func (p *Plugin) ListTools() []toolkit.Tool {
	if p == nil || p.mgr == nil {
		return nil
	}
	return p.mgr.ListTools()
}

// OnToolsChanged 注册工具集合变化回调（实现 [toolkit.ToolChangeNotifier]）。
func (p *Plugin) OnToolsChanged(fn func()) {
	if p == nil || p.mgr == nil {
		return
	}
	p.mgr.OnToolsChanged(fn)
}
