package context

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func makeFakeContext() *Context {
	return NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
}
func makeFakeContextWithContent(content string) *Context {
	detail, _ := json.Marshal(dto.C2CMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{Content: content},
	})
	return NewContext(&dto.Payload{Type: dto.C2CMessageCreate, Detail: detail}, nil)
}
func TestOnCooldown_AllowsFirst(t *testing.T) {
	rule := OnCooldown(1*time.Second, func(c *Context) string { return "cd_user_first" })
	c := makeFakeContext()
	if !rule(c) {
		t.Error("first call should be allowed")
	}
}
func TestOnCooldown_BlocksSecond(t *testing.T) {
	rule := OnCooldown(10*time.Second, func(c *Context) string { return "cd_user_second" })
	c := makeFakeContext()
	rule(c) // first call sets timestamp
	if rule(c) {
		t.Error("second call within cooldown should be blocked")
	}
}
func TestOnCooldown_AllowsAfterExpiry(t *testing.T) {
	rule := OnCooldown(40*time.Millisecond, func(c *Context) string { return "cd_user_expiry" })
	c := makeFakeContext()
	rule(c)
	time.Sleep(50 * time.Millisecond)
	if !rule(c) {
		t.Error("should be allowed after cooldown expiry")
	}
}
func TestOnCooldown_EmptyKeyAllows(t *testing.T) {
	rule := OnCooldown(1*time.Hour, func(c *Context) string { return "" })
	c := makeFakeContext()
	if !rule(c) {
		t.Error("empty key should always allow")
	}
}
func TestOnAtBot_Match(t *testing.T) {
	rule := OnAtBot("")
	c := makeFakeContextWithContent(`<qqbot-at-user id="bot1" />`)
	if !rule(c) {
		t.Error("should match @Bot")
	}
}
func TestOnAtBot_NoMatch(t *testing.T) {
	rule := OnAtBot("")
	c := makeFakeContextWithContent("hello world")
	if rule(c) {
		t.Error("should not match without @Bot tag")
	}
}
func TestOnAtBot_WithID_Match(t *testing.T) {
	rule := OnAtBot("bot99")
	c := makeFakeContextWithContent(`hello <qqbot-at-user id="bot99" /> world`)
	if !rule(c) {
		t.Error("should match specific bot ID")
	}
}
func TestOnAtBot_WithID_NoMatch(t *testing.T) {
	rule := OnAtBot("bot99")
	c := makeFakeContextWithContent(`<qqbot-at-user id="other" />`)
	if rule(c) {
		t.Error("should not match different bot ID")
	}
}
