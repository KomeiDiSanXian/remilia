package context

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

func makeFakeContext() *Context {
	return NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
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
