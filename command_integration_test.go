package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/tidwall/sjson"
)

func TestCommandIntegration(t *testing.T) {
	engine := NewEngine()
	parser := command.NewParser("/")

	executed := false
	var capturedArg string

	echoCmd := &command.Definition{
		Name:      "echo",
		Arguments: []*command.Argument{{Name: "text", Type: command.ArgTypeString, Required: true}},
		Handler: func(v any) {
			ctx := v.(*Context)
			executed = true
			parsed := ctx.GetParsedCommand()
			if parsed != nil {
				capturedArg = parsed.GetString("text")
			}
		},
	}
	parser.Register(echoCmd)

	engine.OnAny(OnCommandMatch(parser)).Handle(ExecuteCommandDefinition)

	content := "/echo hello"
	detail, _ := sjson.SetBytes([]byte("{}"), "content", content)
	event := &dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}

	ctx := NewContext(event, nil)
	engine.ProcessEvent(ctx)

	assert.True(t, executed, "Command handler should be executed")
	assert.Equal(t, "hello", capturedArg, "Argument should be parsed and accessible")
}

func TestCommandIntegration_SubCommand(t *testing.T) {
	engine := NewEngine()
	parser := command.NewParser("!")

	executed := ""

	adminCmd := &command.Definition{
		Name: "admin",
		SubCommands: []*command.Definition{
			{
				Name:      "ban",
				Arguments: []*command.Argument{{Name: "user", Type: command.ArgTypeString}},
				Handler: func(v any) {
					ctx := v.(*Context)
					executed = "ban"
					parsed := ctx.GetParsedCommand()
					if parsed != nil {
						executed += ":" + parsed.GetString("user")
					}
				},
			},
		},
	}
	parser.Register(adminCmd)

	engine.OnAny(OnCommandMatch(parser)).Handle(ExecuteCommandDefinition)

	content := "!admin ban alice"
	detail, _ := sjson.SetBytes([]byte("{}"), "content", content)
	event := &dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}

	ctx := NewContext(event, nil)
	engine.ProcessEvent(ctx)

	assert.Equal(t, "ban:alice", executed)
}

func TestCommandIntegration_MultipleParsers(t *testing.T) {
	engine := NewEngine()
	slashParser := command.NewParser("/")
	bangParser := command.NewParser("!")

	var slashExec, bangExec bool

	slashParser.Register(&command.Definition{Name: "hi", Handler: func(v any) { slashExec = true }})
	bangParser.Register(&command.Definition{Name: "hi", Handler: func(v any) { bangExec = true }})

	engine.OnAny(OnCommandMatch(slashParser)).Handle(ExecuteCommandDefinition)
	engine.OnAny(OnCommandMatch(bangParser)).Handle(ExecuteCommandDefinition)

	// /hi
	{
		detail, _ := sjson.SetBytes([]byte("{}"), "content", "/hi")
		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}, nil)
		engine.ProcessEvent(ctx)
		assert.True(t, slashExec)
		assert.False(t, bangExec)
	}
	// !hi
	{
		slashExec, bangExec = false, false
		detail, _ := sjson.SetBytes([]byte("{}"), "content", "!hi")
		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}, nil)
		engine.ProcessEvent(ctx)
		assert.True(t, bangExec)
		assert.False(t, slashExec)
	}
}
