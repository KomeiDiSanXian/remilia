package cooldown_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/cooldown"
)

func TestCooldown_Allow(t *testing.T) {
	p := cooldown.NewPlugin()

	// 首次调用应该允许
	if !p.Allow("user1", "cmd", time.Second) {
		t.Fatal("first call should be allowed")
	}
	// 立即再次调用应该被拦截
	if p.Allow("user1", "cmd", time.Second) {
		t.Fatal("second call within cooldown should be blocked")
	}
}

func TestCooldown_DifferentUsers(t *testing.T) {
	p := cooldown.NewPlugin()

	if !p.Allow("userA", "cmd", time.Minute) {
		t.Fatal("userA first call should be allowed")
	}
	// 不同用户不受影响
	if !p.Allow("userB", "cmd", time.Minute) {
		t.Fatal("userB should not be affected by userA's cooldown")
	}
}

func TestCooldown_Remaining(t *testing.T) {
	p := cooldown.NewPlugin()
	p.Allow("user1", "cmd", time.Minute)

	r := p.Remaining("user1", "cmd", time.Minute)
	if r <= 0 || r > time.Minute {
		t.Fatalf("unexpected remaining: %v", r)
	}
}

func TestCooldown_Reset(t *testing.T) {
	p := cooldown.NewPlugin()
	p.Allow("user1", "cmd", time.Minute)
	p.Reset("user1", "cmd")

	// 重置后应该可以再次调用
	if !p.Allow("user1", "cmd", time.Minute) {
		t.Fatal("after reset, call should be allowed")
	}
}

func TestCooldown_GlobalAllow(t *testing.T) {
	p := cooldown.NewPlugin()

	if !p.GlobalAllow("broadcast", time.Second) {
		t.Fatal("first global call should be allowed")
	}
	if p.GlobalAllow("broadcast", time.Second) {
		t.Fatal("second global call should be blocked")
	}
}

func TestCooldown_CleanExpired(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := cooldown.NewPlugin()
		p.Allow("user1", "cmd", time.Millisecond)
		time.Sleep(5 * time.Millisecond)

		n := p.CleanExpired(0)
		if n != 1 {
			t.Fatalf("expected 1 expired entry, got %d", n)
		}
	})
}

func TestCooldown_Descriptor(t *testing.T) {
	desc := cooldown.New()
	if desc == nil {
		t.Fatal("descriptor should not be nil")
	}
	if desc.Name != "cooldown" {
		t.Fatalf("unexpected name: %s", desc.Name)
	}
}
