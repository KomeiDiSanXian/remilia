package context

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/command"
)

// commandArgsCache is a typed extension cache container for parsed command arguments.
// It is stored in Context typed extensions (reflect.Type keyed).
// Pointer stability is guaranteed by storing the parsed args pointer.
type commandArgsCache struct {
	Args *command.Args
	Err  error
}

// CommandExtension provides optional Context extensions for command parsing.
//
// It wraps *Context instead of adding more methods to Context.
// This helps keep Context focused on core functionality.
type CommandExtension struct {
	ctx *Context
}

// WithCommand wraps ctx to use command-related extensions.
func WithCommand(ctx *Context) CommandExtension {
	return CommandExtension{ctx: ctx}
}

// ParseCommand parses command arguments from ctx.GetMessageContent() with caching.
// The cache is stored in typed extensions.
func (c CommandExtension) ParseCommand() (*command.Args, error) {
	if c.ctx == nil {
		return nil, nil
	}

	return parseCommandWithCache(
		func() (*commandArgsCache, bool) {
			return c.ctx.Ext().GetTyped[*commandArgsCache]()
		},
		func(v *commandArgsCache) {
			c.ctx.Ext().SetTyped(v)
		},
		c.ctx.GetMessageContent(),
	)
}

// parseCommandWithCache parses command arguments with caching via typed extensions.
//
// Contract:
//   - Read: typed cache (getExt)
//   - Write: typed cache (setExt)
//   - Behavior: if content is empty, returns error "empty message content".
//   - Cache guarantees pointer-stable results on subsequent calls.
func parseCommandWithCache(
	getExt func() (*commandArgsCache, bool),
	setExt func(v *commandArgsCache),
	content string,
) (*command.Args, error) {
	if v, ok := getExt(); ok && v != nil {
		return v.Args, v.Err
	}

	var args *command.Args
	var err error
	if content == "" {
		err = fmt.Errorf("empty message content")
	} else {
		args, err = command.ParseCommandLine(content)
	}

	setExt(&commandArgsCache{Args: args, Err: err})
	return args, err
}

// ParseCommand is a functional-style helper, equivalent to WithCommand(ctx).ParseCommand().
func ParseCommand(ctx *Context) (*command.Args, error) {
	return WithCommand(ctx).ParseCommand()
}
