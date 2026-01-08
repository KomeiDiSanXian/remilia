package extensionimpl

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/command"
)

// Internal cache key. Keep stable for compatibility.
const StateKeyCommandArgs = "_remilia_internal_command_args"

type commandArgsCache struct {
	args *command.CommandArgs
	err  error
}

// ParseCommand parses command arguments with caching.
//
// Contract:
//   - Cache is stored in Context internal state via getInternal/setInternal.
//   - If content is empty, returns error "empty message content".
//   - Cache guarantees pointer-stable results on subsequent calls.
func ParseCommand(
	getInternal func(key string) (any, bool),
	setInternal func(key string, value any),
	content string,
) (*command.CommandArgs, error) {
	if val, ok := getInternal(StateKeyCommandArgs); ok {
		if cache, ok := val.(*commandArgsCache); ok {
			return cache.args, cache.err
		}
	}

	var args *command.CommandArgs
	var err error
	if content == "" {
		err = fmt.Errorf("empty message content")
	} else {
		args, err = command.ParseCommandLine(content)
	}

	setInternal(StateKeyCommandArgs, &commandArgsCache{args: args, err: err})
	return args, err
}
