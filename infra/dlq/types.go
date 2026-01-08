package dlq

import "github.com/KomeiDiSanXian/remilia/openapi/dto"

// DeadLetterItem represents a dead letter entry.
//
// NOTE: Although this package is an infrastructure package, it depends only on the
// DTO layer (openapi/dto) and does not depend on the core remilia package.
// This avoids circular dependencies.
type DeadLetterItem struct {
	Event   *dto.Payload
	Err     error
	Attempt int
	Source  string
}

type DeadLetterConsumer interface {
	Consume(item DeadLetterItem)
}
