package qq

import (
	"encoding/json"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestIdentifyPayloadMarshaling(t *testing.T) {
	data := identifyData{
		Token:   "QQBot test_access_token",
		Intents: int(IntentGroupAndC2C | IntentGuilds),
		Shard:   [2]int{0, 1},
		Properties: identifyProperties{
			OS:      "linux",
			Browser: "remilia",
			Device:  "remilia",
		},
	}
	payload := wsPayload{
		Op:   dto.Identify,
		Data: mustMarshal(data),
	}

	raw, err := json.Marshal(payload)
	assert.NoError(t, err)

	var parsed wsPayload
	err = json.Unmarshal(raw, &parsed)
	assert.NoError(t, err)
	assert.Equal(t, dto.Identify, parsed.Op, "op should be Identify (2)")

	var decoded identifyData
	err = json.Unmarshal(parsed.Data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "QQBot test_access_token", decoded.Token)
	assert.Equal(t, [2]int{0, 1}, decoded.Shard)
	assert.Equal(t, "linux", decoded.Properties.OS)
	assert.Equal(t, "remilia", decoded.Properties.Browser)
}

func TestResumePayloadMarshaling(t *testing.T) {
	data := resumeData{
		Token:     "QQBot test_access_token",
		SessionID: "session_abc123",
		Seq:       42,
	}
	payload := wsPayload{
		Op:   dto.Resume,
		Data: mustMarshal(data),
	}

	raw, err := json.Marshal(payload)
	assert.NoError(t, err)

	var parsed wsPayload
	err = json.Unmarshal(raw, &parsed)
	assert.NoError(t, err)
	assert.Equal(t, dto.Resume, parsed.Op, "op should be Resume (6)")

	var decoded resumeData
	err = json.Unmarshal(parsed.Data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "session_abc123", decoded.SessionID)
	assert.Equal(t, uint64(42), decoded.Seq)
}

func TestHeartbeatPayloadMarshaling(t *testing.T) {
	// 有心跳序列号
	payload := wsPayload{
		Op:   dto.Heartbeat,
		Data: json.RawMessage("42"),
	}
	raw, err := json.Marshal(payload)
	assert.NoError(t, err)

	var parsed wsPayload
	err = json.Unmarshal(raw, &parsed)
	assert.NoError(t, err)
	assert.Equal(t, dto.Heartbeat, parsed.Op)
	assert.Equal(t, "42", string(parsed.Data))

	// 无线索号（首次连接）
	payload2 := wsPayload{Op: dto.Heartbeat}
	raw2, _ := json.Marshal(payload2)
	var parsed2 wsPayload
	json.Unmarshal(raw2, &parsed2)
	assert.Equal(t, dto.Heartbeat, parsed2.Op)
	assert.Empty(t, string(parsed2.Data), "first heartbeat should have no data")
}

func TestHelloPayloadParsing(t *testing.T) {
	raw := `{"op":10,"d":{"heartbeat_interval":45000}}`
	var p wsPayload
	err := json.Unmarshal([]byte(raw), &p)
	assert.NoError(t, err)
	assert.Equal(t, dto.Hello, p.Op)

	var h helloData
	err = json.Unmarshal(p.Data, &h)
	assert.NoError(t, err)
	assert.Equal(t, 45000, h.HeartbeatInterval)
}

func TestDispatchEventPayloadParsing(t *testing.T) {
	// READY 事件
	raw := `{
		"op":0,"s":1,"t":"READY",
		"d":{"version":1,"session_id":"sess_001","user":{"id":"bot001","username":"TestBot","bot":true}}
	}`
	var p wsPayload
	err := json.Unmarshal([]byte(raw), &p)
	assert.NoError(t, err)
	assert.Equal(t, dto.Dispatch, p.Op)
	assert.Equal(t, uint64(1), p.S)
	assert.Equal(t, "READY", p.T)

	var ready dto.ReadyEvent
	err = json.Unmarshal(p.Data, &ready)
	assert.NoError(t, err)
	assert.Equal(t, "sess_001", ready.SessionID)
	assert.Equal(t, "bot001", ready.User.ID)
	assert.Equal(t, "TestBot", ready.User.Username)
	assert.True(t, ready.User.IsBot)

	// C2C 消息事件
	raw2 := `{
		"op":0,"s":2,"t":"C2C_MESSAGE_CREATE",
		"d":{"content":"hello","author":{"id":"u001","username":"User"}}
	}`
	var p2 wsPayload
	json.Unmarshal([]byte(raw2), &p2)
	assert.Equal(t, "C2C_MESSAGE_CREATE", p2.T)
	assert.Equal(t, uint64(2), p2.S)
}

func TestReconnectAndInvalidSessionParsing(t *testing.T) {
	// Reconnect
	var reconnect wsPayload
	json.Unmarshal([]byte(`{"op":7}`), &reconnect)
	assert.Equal(t, dto.Reconnect, reconnect.Op)

	// InvalidSession
	var invalid wsPayload
	json.Unmarshal([]byte(`{"op":9}`), &invalid)
	assert.Equal(t, dto.InvalidSession, invalid.Op)
}

func TestHeartbeatAckParsing(t *testing.T) {
	var ack wsPayload
	json.Unmarshal([]byte(`{"op":11}`), &ack)
	assert.Equal(t, dto.HeartbeatACK, ack.Op)
}
