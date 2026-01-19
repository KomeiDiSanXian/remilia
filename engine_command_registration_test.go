package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/tidwall/sjson"
)

func TestEngine_RegisterCommand(t *testing.T) {
	e := NewEngine()

	executed := false
	var capturedArgs string

	cmd := &command.Definition{
		Name: "echo",
		Arguments: []*command.Argument{
			{Name: "msg", Type: command.ArgTypeString, Required: true},
		},
		Handler: func(v any) {
			ctx := v.(*Context)
			executed = true
			parsed := ctx.GetParsedCommand()
			if parsed != nil {
				capturedArgs = parsed.GetString("msg")
			}
		},
	}

	e.RegisterCommand(cmd)

	content := "/echo hello_world"
	detail, _ := sjson.SetBytes([]byte("{}"), "content", content)
	event := &dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}
	ctx := NewContext(event, nil)

	e.ProcessEvent(ctx)

	assert.True(t, executed)
	assert.Equal(t, "hello_world", capturedArgs)
}

func TestEngine_RegisterCommandWithPrefix(t *testing.T) {
	e := NewEngine()

	executed := false
	cmd := &command.Definition{
		Name: "ping",
		Handler: func(_ any) {
			executed = true
		},
	}

	e.RegisterCommandWithPrefix("!", cmd)

	content := "!ping"
	detail, _ := sjson.SetBytes([]byte("{}"), "content", content)
	event := &dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}
	ctx := NewContext(event, nil)

	e.ProcessEvent(ctx)

	assert.True(t, executed)
}

func TestEngine_RegisterCommand_ValidationFailure(t *testing.T) {
	e := NewEngine()

	executed := false
	cmd := &command.Definition{
		Name: "must_int",
		Arguments: []*command.Argument{
			{Name: "val", Type: command.ArgTypeInt, Required: true},
		},
		Handler: func(_ any) {
			executed = true
		},
	}

	e.RegisterCommand(cmd)

	content := "/must_int abc"
	detail, _ := sjson.SetBytes([]byte("{}"), "content", content)
	event := &dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}
	ctx := NewContext(event, nil)

	e.ProcessEvent(ctx)

	assert.False(t, executed)
}
