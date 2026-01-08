package extension

import (
	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/command"
	"github.com/KomeiDiSanXian/remilia/internal/extensionimpl"
)

// Command provides optional Context extensions.
//
// It intentionally wraps *remilia.Context instead of adding more methods to remilia.Context.
// This helps keep remilia.Context focused on core (event + std context + state).
type Command struct {
	ctx *remilia.Context
}

// WithCommand wraps ctx to use command-related extensions.
func WithCommand(ctx *remilia.Context) Command {
	return Command{ctx: ctx}
}

// ParseCommand parses command arguments from ctx.GetMessageContent() with caching.
func (c Command) ParseCommand() (*command.CommandArgs, error) {
	if c.ctx == nil {
		return nil, nil
	}
	return extensionimpl.ParseCommand(
		c.ctx.InternalGet,
		c.ctx.InternalSet,
		c.ctx.GetMessageContent(),
	)
}

// ParseCommand is a functional-style helper, equivalent to WithCommand(ctx).ParseCommand().
func ParseCommand(ctx *remilia.Context) (*command.CommandArgs, error) {
	return WithCommand(ctx).ParseCommand()
}
