// command.go — 真实命令通道：把一次动作调用重放成合成命令事件并取回回复。
//
// 插件把命令包装成动作后，模型调用该动作时真正要执行的应是原始命令 handler，
// 而不是包装器里的占位函数。这里通过合成事件重放一次命令输入，并用
// [CaptureSender] 截获 handler 输出：AI 自行总结后再回复用户，
// 避免用户看到"工具结果"与"最终回复"两条消息。
//
// 需要的两项外部数据由本包作为消费方声明最小端口，装配点再适配：
//   - [CommandPatterns]：目录 owner 的只读视图（动作名 → 命令模式）
//   - [EventProcessor]：运行时装配的事件处理器

package execution

import (
	"errors"
	"fmt"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// CommandPatterns 动作名到完整命令模式的只读映射。
// 发现阶段写入、执行阶段只读，锁与映射本身都留在实现侧。
type CommandPatterns interface {
	// Pattern 返回该动作名对应的命令模式；不存在时 ok=false。
	Pattern(toolName string) (string, bool)
}

// EventProcessor 触发合成事件。
//
// 只声明命令通道用到的那一个方法：必须等 handler 执行完毕才能读回捕获到的回复。
type EventProcessor interface {
	// ProcessPlatformEventSync 同步处理事件，强制 handler 在当前 goroutine 执行。
	ProcessPlatformEventSync(event platform.Event, sender platform.Sender, caps ...platform.Capabilities)
}

// RunCommand 通过合成事件执行动作对应的真实命令，返回捕获到的回复文本。
//
// 来源解析由调用方完成：只有命令模式已登记的动作才会走到这里。返回 error 只
// 表示"命中了命令却无法重放"（动作未登记命令模式——编程错误；原事件不含平台
// 事件——该上下文无法触发命令）。返回空文本是合法结果：命令执行成功但 handler
// 没有产生文本（例如只发附件），调用方必须如实回填，不得据此回退到动作自身的
// 占位 Execute。
func RunCommand(origCtx *eventctx.Context, toolName string, args map[string]any, cs *CaptureSender, patterns CommandPatterns, processor EventProcessor) (string, error) {
	pattern, ok := patterns.Pattern(toolName)
	if !ok {
		return "", fmt.Errorf("动作 %q 没有登记命令模式", toolName)
	}
	if processor == nil {
		return "", fmt.Errorf("动作 %q 缺少事件处理器，无法触发真实命令", toolName)
	}
	if origCtx == nil {
		return "", fmt.Errorf("动作 %q 缺少事件上下文，无法触发真实命令", toolName)
	}

	originalEvent := origCtx.GetPlatformEvent()
	if originalEvent == nil {
		return "", errors.New("当前上下文缺少平台事件，无法触发真实命令")
	}

	if rawArgs, ok := args["arguments"].(string); ok && rawArgs != "" {
		if IsSafeCommandArg(rawArgs) {
			pattern += " " + rawArgs
		}
	}

	evt := platform.NewSyntheticEvent(
		originalEvent.Kind(),
		pattern,
		platform.WithSyntheticSender(originalEvent.Sender()),
		platform.WithSyntheticChat(originalEvent.Chat()),
	)
	processor.ProcessPlatformEventSync(evt, cs)
	return cs.CapturedText, nil
}
