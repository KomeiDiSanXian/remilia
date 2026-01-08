package remilia

import (
	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/internal/extensionimpl"
)

// ParseCommand 解析命令参数（带缓存）。
//
// 说明：命令解析属于可选能力，推荐使用 package extension：
//   - extension.ParseCommand(ctx)
//   - extension.WithCommand(ctx).ParseCommand()
//
// 该方法为兼容入口，行为保持不变。
func (ctx *Context) ParseCommand() (*command.CommandArgs, error) {
	if ctx == nil {
		return nil, nil
	}
	return extensionimpl.ParseCommand(ctx.internalGet, ctx.internalSet, ctx.GetMessageContent())
}
