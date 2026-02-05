package remilia

import "errors"

var (
	// ErrAdapterRequired indicates that an adapter is required to build a bot
	ErrAdapterRequired = errors.New("adapter is required")

	// ErrEngineRequired indicates that an engine is required
	ErrEngineRequired = errors.New("engine is required")

	// ErrBotInfoRequired indicates that bot info is required for certain operations
	ErrBotInfoRequired = errors.New("bot info is required")

	// ErrInvalidConfig indicates invalid configuration
	ErrInvalidConfig = errors.New("invalid configuration")
)
