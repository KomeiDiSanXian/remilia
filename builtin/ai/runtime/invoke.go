// invoke.go — 动作调用器：把"动作如何被执行"从统一 Execute 字段里分离。
//
// 模型看到的工具在协议层完全一致（都是 function/tool），但运行时语义有三种：
//   - 普通动作：真执行 Go 回调
//   - 真实命令：合成事件触发命令 handler 并捕获回复
//   - Skill：启动子代理 LLM 循环
//
// 让一个 Execute 字段同时承担这三种语义，是工具难以抽象与重构的直接原因。
// 现在由调用方解析出来源后选择对应调用器，调用器只负责"调用并产出结果"，
// 来源解析与策略闸门仍在装配侧。
//
// 调用器对插件的依赖被反转为能力端口：[CommandCatalog] 与 [SkillRunner] 各自
// 只有一个最小方法，插件实例只出现在装配点的适配器里。
package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/execution"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// ActionResult 一次动作调用的结果。
//
// Err 非空表示"模型可见的失败"：此时 Text 是可读的错误说明，会照常回填给模型
// 让它自行纠错。Err 只承载失败语义（供重试预算与反思引导使用），
// 底层错误链已被格式化进 Text，不再单独暴露。
type ActionResult struct {
	// Text 回填给模型的文本。
	Text string
	// Err 非空表示本次调用失败。
	Err error
}

// FuncInvoker 调用工具自身的 Execute 回调（普通动作）。
type FuncInvoker struct {
	// Name 动作名（用于失败文案与指标标签）。
	Name string
	// Args 模型给出的参数。
	Args map[string]any
	// Fn 工具自身的 Execute 回调。
	Fn func(context.Context, map[string]any) (string, error)
	// Record 观测回调：调用结束（无论成败）时上报动作名与错误，可为 nil。
	Record func(name string, err error)
}

// Invoke 真执行回调，并把失败格式化为模型可见文本。
func (i FuncInvoker) Invoke(ctx context.Context) ActionResult {
	result, err := i.Fn(ctx, i.Args)
	if i.Record != nil {
		i.Record(i.Name, err)
	}
	if err != nil {
		return ActionResult{Text: ToolFailureText(i.Name, err), Err: err}
	}
	return ActionResult{Text: result}
}

// SkillRunner 执行一个 Skill 的子代理循环（能力端口）。
//
// 端口只回答"把这个 Skill 跑完并给出最终文本"。子代理自己的 Prompt、工具集、
// 轮次与模型都留在装配侧，调用器因此不依赖插件实例。
type SkillRunner interface {
	// Run 跑完技能的子代理循环并返回最终文本。
	Run(ctx context.Context, skill toolkit.Skill, args map[string]any) (string, error)
}

// SkillInvoker 启动子代理 LLM 循环（Skill 动作）。
type SkillInvoker struct {
	// Name 动作名。
	Name string
	// Skill 被调用的技能。
	Skill toolkit.Skill
	// Args 模型给出的参数。
	Args map[string]any
	// Runner 子代理循环的能力端口。
	Runner SkillRunner
}

// Invoke 执行技能；失败时按技能语义格式化错误文本。
func (i SkillInvoker) Invoke(ctx context.Context) ActionResult {
	result, err := i.Runner.Run(ctx, i.Skill, i.Args)
	if err != nil {
		return ActionResult{
			Text: fmt.Sprintf("错误: 技能 %q 执行失败: %v", i.Name, err),
			Err:  err,
		}
	}
	return ActionResult{Text: result}
}

// CommandCatalog 真实命令目录（能力端口）：按动作名执行已注册命令并捕获其回复。
//
// 端口只回答"这个动作名对应哪条真实命令、跑完说了什么"。命令表、合成事件与
// 平台同步器都留在装配侧：合成事件的重放逻辑在 execution.RunCommand，
// 命令模式与事件处理器由装配侧的适配器提供。
type CommandCatalog interface {
	// Run 以合成事件触发名为 name 的真实命令，返回捕获到的回复文本。
	Run(ctx *eventctx.Context, name string, args map[string]any, cs *execution.CaptureSender) string
}

// CommandInvoker 通过合成事件触发真实命令 handler 并捕获其回复。
//
// 只返回捕获到的文本；为空表示该工具没有对应真实命令，调用方据此回退到
// 普通动作路径（与既有回退语义一致）。串行化（syncer 非线程安全）由调用方
// 在调用前后加锁，不在调用器内部实现。
type CommandInvoker struct {
	// Catalog 命令目录能力端口。
	Catalog CommandCatalog
	// Ctx 事件上下文（合成事件以它为基础）。
	Ctx *eventctx.Context
	// Name 动作名。
	Name string
	// Args 模型给出的参数。
	Args map[string]any
	// CS 捕获命令 handler 输出的发送器。
	CS *execution.CaptureSender
}

// Invoke 触发真实命令并返回捕获结果。
func (i CommandInvoker) Invoke(context.Context) ActionResult {
	return ActionResult{Text: i.Catalog.Run(i.Ctx, i.Name, i.Args, i.CS)}
}

// ToolFailureText 把工具执行错误格式化为模型可见的失败文本。
//
// 取消/超时归为"执行超时"，与既有文案逐字一致；其余保留原始错误说明。
func ToolFailureText(name string, err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("错误: 工具 %q 执行超时", name)
	}
	return fmt.Sprintf("错误: 工具 %q 执行失败: %v", name, err)
}
