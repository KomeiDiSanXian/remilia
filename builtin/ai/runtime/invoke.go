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

// Invoker 一次动作调用的执行器契约。三种执行语义（普通动作 / 真实命令 /
// Skill 子代理）各自实现它，调用方因此可以统一编排、观测与失败处理，
// 而不必逐个类型分支。
type Invoker interface {
	// Invoke 执行一次动作调用。ctx 为调用方给出的超时/取消上下文。
	Invoke(ctx context.Context) ActionResult
}

// RecordFunc 观测回调：一次调用结束（无论成败）时上报动作名与错误。
// 由装配侧注入（如指标采集），可为 nil。
type RecordFunc func(name string, err error)

// FuncInvoker 调用工具自身的 Execute 回调（普通动作）。
type FuncInvoker struct {
	// Name 动作名（用于失败文案与指标标签）。
	Name string
	// Args 模型给出的参数。
	Args map[string]any
	// Fn 工具自身的 Execute 回调。
	Fn func(context.Context, map[string]any) (string, error)
	// Record 观测回调：调用结束（无论成败）时上报动作名与错误，可为 nil。
	Record RecordFunc
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
	// Record 观测回调：调用结束（无论成败）时上报动作名与错误，可为 nil。
	Record RecordFunc
}

// Invoke 执行技能；失败时按技能语义格式化错误文本。
func (i SkillInvoker) Invoke(ctx context.Context) ActionResult {
	result, err := i.Runner.Run(ctx, i.Skill, i.Args)
	if i.Record != nil {
		i.Record(i.Name, err)
	}
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
	// 只有在命令模式已登记（来源解析由装配侧完成）时才会被调用。
	Run(ctx *eventctx.Context, name string, args map[string]any, cs *execution.CaptureSender) (string, error)
}

// CommandInvoker 通过合成事件触发真实命令 handler 并捕获其回复。
//
// 来源解析（这个动作名是否对应真实命令）由装配侧完成：只有登记了命令模式的
// 动作才构造本调用器，因此"没有真实命令"不是这里的分支。调用器只承担
// "跑命令并把结果如实汇报"：命令失败置 Err；命令跑通但没有文本（例如只发了
// 附件）回一句明确的"未返回文本"，绝不回退成动作自身的占位 Execute 冒充成功。
// 串行化（syncer 非线程安全）由调用方在调用前后加锁，不在调用器内部实现。
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
	// Record 观测回调：调用结束（无论成败）时上报动作名与错误，可为 nil。
	Record RecordFunc
}

// Invoke 触发真实命令并返回捕获结果。
func (i CommandInvoker) Invoke(context.Context) ActionResult {
	text, err := i.Catalog.Run(i.Ctx, i.Name, i.Args, i.CS)
	if i.Record != nil {
		i.Record(i.Name, err)
	}
	if err != nil {
		return ActionResult{Text: CommandFailureText(i.Name, err), Err: err}
	}
	if text == "" {
		// 命令确实执行了，只是没有产生文本：如实回填，不冒充成功也不回退。
		return ActionResult{Text: fmt.Sprintf("(命令 %q 已执行，未返回文本)", i.Name)}
	}
	return ActionResult{Text: text}
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

// CommandFailureText 把命令通道的失败格式化为模型可见的失败文本。
//
// 与 [ToolFailureText] 同构，只把主体从"工具"换成"命令"，便于区分失败来源。
func CommandFailureText(name string, err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("错误: 命令 %q 执行超时", name)
	}
	return fmt.Sprintf("错误: 命令 %q 执行失败: %v", name, err)
}

// 三种执行语义共用一个契约：调用方可以统一编排、观测与失败处理。
var (
	_ Invoker = FuncInvoker{}
	_ Invoker = SkillInvoker{}
	_ Invoker = CommandInvoker{}
)
