package dto

import (
	"encoding/json"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/intents"
	"github.com/tidwall/gjson"
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

// Decode parses the Detail field of the Payload into the provided struct.
//
// For the most common event types (C2CMessageCreateEvent,
// GroupAtMessageCreateEvent) a zero-allocation gjson fast path is used.
// All other types fall back to json.Unmarshal.
func (p *Payload) Decode(v any) error {
	if p.Detail == nil {
		return nil
	}

	// Fast paths for the two highest-frequency event types.
	// gjson.GetBytes never allocates for scalar fields and only allocates
	// string values, which is unavoidable regardless of the parser used.
	switch dst := v.(type) {
	case *C2CMessageCreateEvent:
		if !gjson.ValidBytes(p.Detail) {
			return json.Unmarshal(p.Detail, v) // let stdlib return the parse error
		}
		decodeMessageCreateEvent(p.Detail, &dst.MessageCreateEvent)
		return nil
	case *GroupAtMessageCreateEvent:
		if !gjson.ValidBytes(p.Detail) {
			return json.Unmarshal(p.Detail, v)
		}
		decodeMessageCreateEvent(p.Detail, &dst.MessageCreateEvent)
		dst.GroupOpenID = gjson.GetBytes(p.Detail, "group_openid").String()
		return nil
	}

	// Generic fallback.
	return json.Unmarshal(p.Detail, v)
}

// decodeMessageCreateEvent fills a MessageCreateEvent using gjson field
// extraction.  gjson.GetBytes operates directly on the []byte slice without
// converting to string, so only the extracted string values are allocated —
// the same allocation budget as json.Unmarshal but without the reflection
// overhead.
func decodeMessageCreateEvent(data []byte, e *MessageCreateEvent) {
	e.ID = EventID(gjson.GetBytes(data, "id").String())
	e.Content = gjson.GetBytes(data, "content").String()
	e.Timestamp = gjson.GetBytes(data, "timestamp").String()
	e.Author.ID = gjson.GetBytes(data, "author.id").String()
	e.Author.MemberOpenID = gjson.GetBytes(data, "author.member_openid").String()
	e.Author.UnionOpenID = gjson.GetBytes(data, "author.union_openid").String()
	e.Author.UserOpenID = gjson.GetBytes(data, "author.user_openid").String()

	// Attachments: only allocate when the array is present and non-empty.
	if arr := gjson.GetBytes(data, "attachments"); arr.IsArray() {
		results := arr.Array()
		if len(results) > 0 {
			e.Attachments = make([]Attachment, 0, len(results))
			for _, r := range results {
				b := r.Raw
				e.Attachments = append(e.Attachments, Attachment{
					Type:         gjson.Get(b, "content_type").String(),
					FileName:     gjson.Get(b, "filename").String(),
					Height:       int(gjson.Get(b, "height").Int()),
					Width:        int(gjson.Get(b, "width").Int()),
					Size:         int(gjson.Get(b, "size").Int()),
					URL:          gjson.Get(b, "url").String(),
					VoiceWavURL:  gjson.Get(b, "voice_wav_url").String(),
					AsrReferText: gjson.Get(b, "asr_refer_text").String(),
				})
			}
		}
	}
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
