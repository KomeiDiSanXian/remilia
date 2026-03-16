package dlq

import (
	"encoding/json"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// DeadLetterItem represents a dead letter entry.
//
// Deprecated: Use Item[platform.Event] (PlatformEventItem) for new code.
// DeadLetterItem is kept for backward compatibility with existing QQ consumers.
type DeadLetterItem struct {
	Event   *dto.Payload
	Err     error
	Attempt int
	Source  string
}

type DeadLetterConsumer interface {
	Consume(item DeadLetterItem)
}

// DeadLetterEvent is a lightweight event representation for persistence.
type DeadLetterEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// DeadLetterError is a simplified error representation for dead letter serialization.
type DeadLetterError struct {
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
}

// MarshalDeadLetterItem serializes a DeadLetterItem for persistence.
// It creates a simplified JSON representation with event info and error details.
func MarshalDeadLetterItem(item DeadLetterItem) ([]byte, error) {
	errMsg := ""
	if item.Err != nil {
		errMsg = item.Err.Error()
	}

	return json.Marshal(struct {
		Event *DeadLetterEvent `json:"event"`
		Error DeadLetterError  `json:"error"`
	}{
		Event: &DeadLetterEvent{
			ID:   string(item.Event.ID),
			Type: string(item.Event.Type),
		},
		Error: DeadLetterError{
			Message: errMsg,
			Source:  item.Source,
			Attempt: item.Attempt,
		},
	})
}

// MarshalPlatformEventItem 序列化平台无关的死信队列条目。
func MarshalPlatformEventItem(item Item[platform.Event]) ([]byte, error) {
	errMsg := ""
	if item.Err != nil {
		errMsg = item.Err.Error()
	}
	id := ""
	rawType := ""
	plat := ""
	if item.Data != nil {
		rawType = item.Data.RawType()
		plat = item.Data.Platform()
	}

	return json.Marshal(struct {
		Event *DeadLetterEvent `json:"event"`
		Error DeadLetterError  `json:"error"`
	}{
		Event: &DeadLetterEvent{ID: id, Type: plat + "/" + rawType},
		Error: DeadLetterError{Message: errMsg, Source: item.Source, Attempt: item.Attempt},
	})
}
