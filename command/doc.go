package command

// Package command provides the "enhanced command" system (CommandParser/CommandDefinition)
// built on top of remilia's raw command parsing (ParseCommandLine/CommandArgs).
//
// This package exists to avoid API splitting/confusion in the root remilia package.
// The root package keeps backward-compatible re-exports.
