package fsm_test

import (
	"fmt"
	"time"

	fsm "github.com/KomeiDiSanXian/remilia/core/fsm"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

type testEvent struct {
	platform string
	content  string
	chatID   string
	kind     platform.EventKind
}

func (e *testEvent) Platform() string                          { return e.platform }
func (e *testEvent) ID() string                                { return "" }
func (e *testEvent) Kind() platform.EventKind                  { return e.kind }
func (e *testEvent) Sender() platform.UserInfo                 { return platform.UserInfo{ID: "user1"} }
func (e *testEvent) Chat() platform.ChatInfo                   { return platform.ChatInfo{ID: e.chatID} }
func (e *testEvent) Content() string                           { return e.content }
func (e *testEvent) Attachments() []platform.InboundAttachment { return nil }
func (e *testEvent) Timestamp() time.Time                      { return time.Time{} }

func ctx(content string) *corectx.Context {
	evt := &testEvent{platform: "test", content: content, chatID: "ch1", kind: platform.EventKindPrivateMessage}
	return corectx.NewContextFromEvent(evt, &platform.NoopSender{})
}

func Example_registerForm() {
	mgr := fsm.NewManager(nil)

	formFSM := &fsm.FSM{
		Name:    "signup",
		Initial: "idle",
		Events: []fsm.Event{
			{Name: "start", From: "idle", To: "ask_name",
				Match: func(ctx *corectx.Context) bool { return ctx.GetMessageContent() == "/signup" },
				Action: func(ctx *fsm.FSMContext) error {
					ctx.Reply(platform.TextMessage("请输入您的姓名："))
					return nil
				}},
			{Name: "input_name", From: "ask_name", To: "ask_age",
				Match: func(ctx *corectx.Context) bool { return ctx.GetMessageContent() != "" },
				Action: func(ctx *fsm.FSMContext) error {
					ctx.Data["name"] = ctx.GetMessageContent()
					ctx.Reply(platform.TextMessage(fmt.Sprintf("您好 %s，请输入年龄：", ctx.Data["name"])))
					return nil
				}},
			{Name: "input_age", From: "ask_age", To: "done",
				Match: func(ctx *corectx.Context) bool { return ctx.GetMessageContent() != "" },
				Action: func(ctx *fsm.FSMContext) error {
					ctx.Data["age"] = ctx.GetMessageContent()
					ctx.Reply(platform.TextMessage(fmt.Sprintf("注册成功！姓名：%s，年龄：%s", ctx.Data["name"], ctx.Data["age"])))
					return nil
				}},
			{Name: "cancel", From: "*", To: "idle",
				Match: func(ctx *corectx.Context) bool { return ctx.GetMessageContent() == "/cancel" },
				Action: func(ctx *fsm.FSMContext) error {
					ctx.Reply(platform.TextMessage("已取消注册"))
					return nil
				}},
		},
		Timeout: 5 * time.Minute,
	}

	desc := &fsm.FSMDescriptor{Name: "signup", FSM: formFSM}
	if err := mgr.Register(desc); err != nil {
		panic(err)
	}

	eng := mgr.GetEngine()

	sessionID := "ch1"
	eng.StartSession(ctx("/signup"), "signup", sessionID)

	state, ok, _ := eng.TryTransition(ctx("/signup"), sessionID)
	fmt.Printf("after /signup: state=%s ok=%v\n", state, ok)

	state, ok, _ = eng.TryTransition(ctx("Alice"), sessionID)
	fmt.Printf("after name: state=%s ok=%v\n", state, ok)

	state, ok, _ = eng.TryTransition(ctx("25"), sessionID)
	fmt.Printf("after age: state=%s ok=%v\n", state, ok)

	// Output:
	// after /signup: state=ask_name ok=true
	// after name: state=ask_age ok=true
	// after age: state=done ok=true
}
