package dto

import (
	"encoding/json"

	"github.com/KomeiDiSanXian/remilia/openapi/intents"
)

// OperationCode is the operation code of the payload
type OperationCode byte

const (
	Dispatch OperationCode = iota
	Heartbeat
	Identify
	_
	_
	_
	Resume
	Reconnect
	_
	InvalidSession
	Hello
	HeartbeatACK
	HTTPCallbackACK
	HTTPCallbackValidation
)

// OperationCodeName is the name of the operation code
var OperationCodeName = map[OperationCode]string{
	Dispatch:               "Dispatch",
	Heartbeat:              "Heartbeat",
	Identify:               "Identify",
	Resume:                 "Resume",
	Reconnect:              "Reconnect",
	InvalidSession:         "InvalidSession",
	Hello:                  "Hello",
	HeartbeatACK:           "HeartbeatACK",
	HTTPCallbackACK:        "HTTPCallbackACK",
	HTTPCallbackValidation: "HTTPCallbackValidation",
}

// Payload is a struct that holds the payload of a request
type Payload struct {
	ID        EventID         `json:"id,omitempty"` // ID of the event
	Operation OperationCode   `json:"op"`           // Operation code
	Detail    json.RawMessage `json:"d,omitempty"`  // Detail of the event
	Sequence  uint64          `json:"s,omitempty"`  // Sequence number
	Type      EventType       `json:"t,omitempty"`  // Type of the event
	Raw       []byte          `json:"-"`            // Raw is original payload bytes
}

// Clone creates a deep copy of the Payload
func (p *Payload) Clone() *Payload {
	return &Payload{
		ID:        p.ID,
		Operation: p.Operation,
		Detail:    p.Detail,
		Sequence:  p.Sequence,
		Type:      p.Type,
		Raw:       p.Raw,
	}
}

// Decode parses the Detail field of the Payload into the provided struct
func (p *Payload) Decode(v any) error {
	if p.Detail == nil {
		return nil // No detail to parse
	}
	if err := json.Unmarshal(p.Detail, v); err != nil {
		return err
	}
	return nil
}

// IdentifyPayload is the struct for identify payload
type IdentifyPayload struct {
	Token   string          `json:"token"`
	Intents intents.Intents `json:"intents"`
	Shard   [2]byte         `json:"shard"`
}

// ReadyEvent is the struct for ready event
type ReadyEvent struct {
	Version   int    `json:"version"`
	SessionID string `json:"session_id"`
	User      User   `json:"user"`
}

// User is the struct for user
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	IsBot    bool   `json:"bot"`
}
