package cooldown

import (
	"testing"
	"time"
)

func TestGroupAllow(t *testing.T) {
	p := NewPlugin()

	// 群 A 第一次允许
	if !p.GroupAllow("group-A", "news", time.Minute) {
		t.Fatal("expected GroupAllow to return true on first call")
	}
	// 群 A 再次请求：冷却中
	if p.GroupAllow("group-A", "news", time.Minute) {
		t.Fatal("expected GroupAllow to return false within cooldown")
	}
	// 群 B 不受群 A 影响
	if !p.GroupAllow("group-B", "news", time.Minute) {
		t.Fatal("expected GroupAllow(group-B) to return true independently")
	}
}

func TestGroupRemaining(t *testing.T) {
	p := NewPlugin()
	dur := time.Minute

	p.GroupAllow("g1", "cmd", dur)
	rem := p.GroupRemaining("g1", "cmd", dur)
	if rem <= 0 || rem > dur {
		t.Fatalf("unexpected remaining: %v", rem)
	}
	// 未消耗的群
	if p.GroupRemaining("g2", "cmd", dur) != 0 {
		t.Fatal("expected 0 remaining for unstarted group cooldown")
	}
}

func TestGroupReset(t *testing.T) {
	p := NewPlugin()

	p.GroupAllow("g1", "cmd", time.Minute)
	p.GroupReset("g1", "cmd")
	// 重置后可再次触发
	if !p.GroupAllow("g1", "cmd", time.Minute) {
		t.Fatal("expected GroupAllow to succeed after GroupReset")
	}
}

func TestPolicy_UserLimit(t *testing.T) {
	p := NewPlugin()
	policy := Policy{
		Command:   "sign",
		UserLimit: time.Minute,
	}
	// 利用底层 Allow 验证 UserLimit 效果
	if !p.Allow("user1", policy.Command, policy.UserLimit) {
		t.Fatal("expected first call to succeed")
	}
	if p.Allow("user1", policy.Command, policy.UserLimit) {
		t.Fatal("expected second call to be in cooldown")
	}
	// user2 不受影响
	if !p.Allow("user2", policy.Command, policy.UserLimit) {
		t.Fatal("expected user2 first call to succeed")
	}
}

func TestPolicy_GroupLimit(t *testing.T) {
	p := NewPlugin()
	policy := Policy{
		Command:    "news",
		GroupLimit: 30 * time.Second,
	}
	// 模拟群级效果
	if !p.GroupAllow("g1", policy.Command, policy.GroupLimit) {
		t.Fatal("expected first group call to succeed")
	}
	if p.GroupAllow("g1", policy.Command, policy.GroupLimit) {
		t.Fatal("expected second group call to be in cooldown")
	}
}

func TestPolicy_GlobalLimit(t *testing.T) {
	p := NewPlugin()
	policy := Policy{
		Command:     "broadcast",
		GlobalLimit: time.Hour,
	}
	if !p.GlobalAllow(policy.Command, policy.GlobalLimit) {
		t.Fatal("expected first global call to succeed")
	}
	if p.GlobalAllow(policy.Command, policy.GlobalLimit) {
		t.Fatal("expected second global call to be in cooldown")
	}
}
