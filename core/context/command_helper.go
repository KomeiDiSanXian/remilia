package context

import (
	"errors"
	"strings"

	"github.com/KomeiDiSanXian/remilia/command"
)

// OnCommandMatch creates a Rule that matches by the given enhanced command parser.
// If matched, the parsed result is cached in Context internal extensionState.
func OnCommandMatch(parser *command.Parser) Rule {
	return func(ctx *Context) bool {
		return ctx.MatchCommand(parser)
	}
}

// OnParseCommand creates a Rule that parses input against a command Definition.
//
// The prefix is auto-detected from the message content (leading non-alphanumeric
// characters), so it always matches whatever prefix was used in the OnCommand trigger.
// This eliminates the footgun of specifying prefix in two separate places.
//
// If matched, Arguments/Flags are parsed and the result stored via SetParsedCommand
// for later retrieval in the Handler.
//
// This is the rule used internally by RegisterCommandDefWithPrefix and OnCommandDef.
//
// Example:
//
//	def := &command.Definition{Name: "search", Arguments: []*command.Argument{...}}
//	engine.OnCommand("", "/search",
//	    context.OnParseCommand(def),
//	).Handle(func(ctx *context.Context) error {
//	    parsed := ctx.GetParsedCommand()
//	    keyword := parsed.GetString("keyword")
//	    return ctx.ReplyText("搜索: " + keyword)
//	})
func OnParseCommand(def *command.Definition) Rule {
	return func(ctx *Context) bool {
		content := strings.TrimSpace(ctx.GetMessageContent())
		prefix, _ := SplitCommandPattern(content)
		parsed, err := command.ParseFromDefinition(content, def, prefix)
		if err != nil {
			return false
		}
		ctx.SetParsedCommand(parsed)
		return true
	}
}

// ExecuteCommandDefinition executes the handler registered on the matched command definition.
// Must be used together with OnCommandMatch or OnParseCommand.
func ExecuteCommandDefinition(ctx *Context) error {
	parsed := ctx.GetParsedCommand()
	if parsed == nil || parsed.Definition == nil {
		return errors.New("no command parsed in context")
	}
	if parsed.Definition.Handler != nil {
		// command.Handler signature is `func(any)`, so we pass *Context.
		parsed.Definition.Handler(ctx)
	}
	return nil
}
