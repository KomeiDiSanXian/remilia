package dto

import (
	"encoding/json"
)

// OperationCode 消息载荷的操作码
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

// OperationCodeName 操作码名称映射
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

// Payload 保存请求载荷数据的结构体
type Payload struct {
	ID        EventID         `json:"id,omitempty"` // 事件 ID
	Operation OperationCode   `json:"op"`           // 操作码
	Detail    json.RawMessage `json:"d,omitempty"`  // 事件详情
	Sequence  uint64          `json:"s,omitempty"`  // 序列号
	Type      EventType       `json:"t,omitempty"`  // 事件类型
	Raw       []byte          `json:"-"`            // 原始载荷字节
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
