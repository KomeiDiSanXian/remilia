package remilia

import "github.com/KomeiDiSanXian/remilia/command"

// OnCommandMatch creates a Rule that matches by the given enhanced command parser.
// If matched, the parsed result is cached in Context internal state.
func OnCommandMatch(parser *command.CommandParser) Rule {
	return func(ctx *Context) bool { return ctx.MatchCommand(parser) }
}

// ExecuteCommandDefinition executes the handler registered on the matched command definition.
// Must be used together with OnCommandMatch.
func ExecuteCommandDefinition(ctx *Context) {
	parsed := ctx.GetParsedCommand()
	if parsed == nil || parsed.Definition == nil {
		return
	}
	if parsed.Definition.Handler != nil {
		// command.Handler signature is `func(any)`, so we pass *Context.
		parsed.Definition.Handler(ctx)
	}
}
