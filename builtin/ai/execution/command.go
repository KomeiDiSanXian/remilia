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
// 返回空串表示没有可执行的命令——动作名未登记命令模式，或原事件不含平台事件；
// 调用方据此回退到动作自身的执行路径，与既有回退语义一致。
func RunCommand(origCtx *eventctx.Context, toolName string, args map[string]any, cs *CaptureSender, patterns CommandPatterns, processor EventProcessor) string {
	pattern, ok := patterns.Pattern(toolName)
	if !ok {
		return ""
	}

	originalEvent := origCtx.GetPlatformEvent()
	if originalEvent == nil {
		return ""
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
	return cs.CapturedText
}
