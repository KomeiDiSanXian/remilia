// idtool.go — "按会话内 ID 操作一条记录"这类动作的生成器。
//
// 待办（todo_done / todo_remove）与提醒（cancel_reminder）的动作定义、参数
// Schema 与错误分支逐行同构，差别只有"拿哪个能力端口""写什么文案"。
// 这里把共同结构收敛为一处，各动作只声明差异，避免三个 Execute 回调各自
// 演进而出现"同一个错误在不同工具里文案不同"的漂移。
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// sessionIDAction 描述一个"以会话为作用域、按 ID 操作单条记录"的动作。
type sessionIDAction struct {
	// Name 动作名。
	Name string
	// Description 面向模型的说明。
	Description string
	// IDHint 参数说明里的 ID 示例来源（如 "todo_list"）。
	IDHint string
	// IDExample ID 形态举例（如 "T1"）。
	IDExample string
	// Unavailable 该能力未装配时的错误文案。
	Unavailable string
	// Available 报告该能力在当前调用里是否可用（端口是否为 nil）。
	Available func(inv *toolkit.Invocation) bool
	// MissingID 未给出 ID 时的错误文案。
	MissingID string
	// Apply 执行操作，命中返回 true。inv 与 Available 拿到的是同一份调用上下文。
	Apply func(inv *toolkit.Invocation, src toolkit.ToolSource, id string) bool
	// Done 命中时的结果文案。
	Done func(id string) string
	// NotFound 未命中时的结果文案。
	NotFound func(id string) string
}

// build 生成动作定义。
func (a sessionIDAction) build() toolkit.Tool {
	return toolkit.Tool{
		Name:        a.Name,
		Categories:  []string{toolkit.CategoryGeneral},
		Description: a.Description,
		Parameters: protocol.ToolParamSchema{
			Type: "object",
			Properties: map[string]protocol.ToolParamSchema{
				"id": {Type: "string", Description: a.idDescription()},
			},
			Required: []string{"id"},
		},
		Execute: a.execute,
	}
}

// idDescription 拼出参数说明：形态 + 来源。
func (a sessionIDAction) idDescription() string {
	return fmt.Sprintf("%s ID（如 %s，见 %s）", a.idNoun(), a.IDExample, a.IDHint)
}

// idNoun 从"未给出 ID"的文案里取主体名词，避免再声明一个字段。
//
// 文案约定为"请指定……ID（见 xxx）"，取 "请指定" 与 "ID" 之间的部分即可；
// 取不到时回退为空串（参数说明仍是可读的）。
func (a sessionIDAction) idNoun() string {
	s := a.MissingID
	if i := strings.Index(s, "请指定"); i >= 0 {
		s = s[i+len("请指定"):]
	}
	if i := strings.Index(s, "ID"); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// execute 是三个动作共用的执行体。
func (a sessionIDAction) execute(_ context.Context, inv *toolkit.Invocation, args map[string]any) (toolkit.ToolResult, error) {
	src, ok := toolkit.ToolSourceFromInvocation(inv)
	if !ok {
		return toolkit.ToolResult{}, errors.New("当前调用缺少会话来源，无法定位记录")
	}
	if !a.Available(inv) {
		return toolkit.ToolResult{}, errors.New(a.Unavailable)
	}
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return toolkit.ToolResult{}, errors.New(a.MissingID)
	}
	if a.Apply(inv, src, id) {
		return toolkit.TextResult(a.Done(id)), nil
	}
	return toolkit.TextResult(a.NotFound(id)), nil
}
