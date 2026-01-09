package extensionimpl

import (
	"fmt"

	"github.com/KomeiDiSanXian/remilia/command"
)

// CommandArgsCacheV2 is a typed extension cache container.
//
// It is intended to be stored in Context V2 typed extensions (reflect.Type keyed).
// Pointer stability is guaranteed by storing the parsed args pointer.
//
// NOTE: This type is exported for cross-package access (extension package).
// Users should NOT depend on it.
type CommandArgsCacheV2 struct {
	Args *command.CommandArgs
	Err  error
}

// ParseCommandV2 parses command arguments with caching via typed extensions.
//
// Contract (V2-only):
//   - Read: typed cache (getExt)
//   - Write: typed cache (setExt)
//   - Behavior: if content is empty, returns error "empty message content".
//   - Cache guarantees pointer-stable results on subsequent calls.
func ParseCommandV2(
	getExt func() (*CommandArgsCacheV2, bool),
	setExt func(v *CommandArgsCacheV2),
	content string,
) (*command.CommandArgs, error) {
	if v, ok := getExt(); ok && v != nil {
		return v.Args, v.Err
	}

	var args *command.CommandArgs
	var err error
	if content == "" {
		err = fmt.Errorf("empty message content")
	} else {
		args, err = command.ParseCommandLine(content)
	}

	setExt(&CommandArgsCacheV2{Args: args, Err: err})
	return args, err
}
