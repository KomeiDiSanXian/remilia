package main

import (
	"fmt"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/fsm"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// buildSignupFSM 构建 FSM 多步骤注册表单。
//
// FSM 定义自包含：启动事件（idle→ask_name）由 Router 的 TryStartSession 自动检测，
// 开发者无需手动注册 Engine 命令或管理 fsm.Manager 引用。
//
// 设计要点：
//   - cancel 放首位：通配符 From:* 必须优先于具体状态事件，
//     否则 input_name/input_age 的 Match(TrimSpace != "") 会先匹配
//   - input_age 无 To：To 为空表示终态，Action 执行后框架自动结束会话
//   - ctx.EndSession()：与 To 为空构成"双保险"，无论哪边都确保会话结束
func buildSignupFSM() *fsm.FSM {
	return &fsm.FSM{
		Name:    "signup",
		Initial: "idle",
		Timeout: 3 * time.Minute,
		Events: []fsm.Event{
			{
				Name: "cancel", From: "*", To: "idle",
				Match: func(ctx *eventctx.Context) bool {
					return strings.TrimSpace(ctx.GetMessageContent()) == "/cancel"
				},
				Action: func(ctx *fsm.FSMContext) error {
					_, e := ctx.Reply(platform.TextMessage("已取消注册"))
					ctx.EndSession()
					return e
				},
			},
			{
				Name: "start", From: "idle", To: "ask_name",
				Match: func(ctx *eventctx.Context) bool {
					return strings.TrimSpace(ctx.GetMessageContent()) == "/fsmsignup"
				},
				Action: func(ctx *fsm.FSMContext) error {
					_, e := ctx.Reply(platform.TextMessage("欢迎注册！请输入您的昵称："))
					return e
				},
			},
			{
				Name: "input_name", From: "ask_name", To: "ask_age",
				Match: func(ctx *eventctx.Context) bool {
					return strings.TrimSpace(ctx.GetMessageContent()) != ""
				},
				Action: func(ctx *fsm.FSMContext) error {
					ctx.Data["name"] = strings.TrimSpace(ctx.GetMessageContent())
					_, e := ctx.Reply(platform.TextMessage(fmt.Sprintf("你好 %s！请输入年龄：", ctx.Data["name"])))
					return e
				},
			},
			{
				// To 为空 → 终态：Action 执行后框架自动 EndSession
				Name: "input_age", From: "ask_age",
				Match: func(ctx *eventctx.Context) bool {
					return strings.TrimSpace(ctx.GetMessageContent()) != ""
				},
				Action: func(ctx *fsm.FSMContext) error {
					ctx.Data["age"] = strings.TrimSpace(ctx.GetMessageContent())
					_, e := ctx.Reply(platform.TextMessage(fmt.Sprintf("注册成功！昵称：%s，年龄：%s",
						ctx.Data["name"], ctx.Data["age"])))
					ctx.EndSession()
					return e
				},
			},
		},
	}
}
