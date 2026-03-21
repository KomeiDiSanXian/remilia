package dlq

import (
	"encoding/json"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// DeadLetterError is a simplified error representation for dead letter serialization.
type DeadLetterError struct {
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
}

// MarshalPlatformEventItem 序列化平台无关的死信队列条目。
func MarshalPlatformEventItem(item Item[platform.Event]) ([]byte, error) {
	errMsg := ""
	if item.Err != nil {
		errMsg = item.Err.Error()
	}

	rec := PlatformDeadLetterRecord{
		Error: DeadLetterError{
			Message: errMsg,
			Source:  item.Source,
			Attempt: item.Attempt,
		},
	}

	if item.Data != nil {
		e := item.Data
		rec.Event = &PlatformDeadLetterEvent{
			Platform:      e.Platform(),
			Kind:          string(e.Kind()),
			RawType:       platform.RawType(e),
			ChatID:        e.Chat().ID,
			SenderID:      e.Sender().ID,
			TimestampUnix: e.Timestamp().UnixMilli(),
		}
	}

	return json.Marshal(rec)
}

// PlatformDeadLetterRecord 是平台无关死信条目的 JSON 表示。
type PlatformDeadLetterRecord struct {
	Event *PlatformDeadLetterEvent `json:"event"`
	Error DeadLetterError          `json:"error"`
}

// PlatformDeadLetterEvent 记录来自 platform.Event 的可识别字段。
type PlatformDeadLetterEvent struct {
	Platform      string `json:"platform"`
	Kind          string `json:"kind"`
	RawType       string `json:"raw_type"`
	ChatID        string `json:"chat_id,omitempty"`
	SenderID      string `json:"sender_id,omitempty"`
	TimestampUnix int64  `json:"timestamp_unix,omitempty"`
}
