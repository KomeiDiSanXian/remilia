package verifycode_test

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/verifycode"
)

func TestVerifyCode_GenerateAndVerify(t *testing.T) {
	var grantedUser, grantedRole string
	p := verifycode.NewPlugin(func(userID, role string) error {
		grantedUser = userID
		grantedRole = role
		return nil
	})

	code, err := p.Generate(verifycode.CodeConfig{
		Role:    "vip",
		TTL:     time.Hour,
		MaxUses: 1,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if code == "" {
		t.Fatal("code should not be empty")
	}

	role, err := p.Verify("user123", code)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if role != "vip" {
		t.Fatalf("expected role 'vip', got %q", role)
	}
	if grantedUser != "user123" || grantedRole != "vip" {
		t.Fatal("OnVerifyHook not called correctly")
	}
}

func TestVerifyCode_OneTimeUse(t *testing.T) {
	p := verifycode.NewPlugin(nil)

	code, _ := p.Generate(verifycode.CodeConfig{Role: "guest", TTL: time.Hour, MaxUses: 0})

	// 第一次成功
	if _, err := p.Verify("user1", code); err != nil {
		t.Fatalf("first verify should succeed: %v", err)
	}
	// 第二次应该失败（一次性）
	if _, err := p.Verify("user2", code); err == nil {
		t.Fatal("second verify of one-time code should fail")
	}
}

func TestVerifyCode_CannotReuseByUser(t *testing.T) {
	p := verifycode.NewPlugin(nil)
	code, _ := p.Generate(verifycode.CodeConfig{Role: "vip", TTL: time.Hour, MaxUses: -1})

	// 同一用户不能重复使用
	if _, err := p.Verify("user1", code); err != nil {
		t.Fatalf("first verify should succeed: %v", err)
	}
	if _, err := p.Verify("user1", code); err == nil {
		t.Fatal("same user should not be able to use code twice")
	}
}

func TestVerifyCode_Revoke(t *testing.T) {
	p := verifycode.NewPlugin(nil)
	code, _ := p.Generate(verifycode.CodeConfig{Role: "admin", TTL: time.Hour})

	p.Revoke(code)

	if _, err := p.Verify("user1", code); err == nil {
		t.Fatal("revoked code should fail verification")
	}
}

func TestVerifyCode_Expired(t *testing.T) {
	p := verifycode.NewPlugin(nil)
	code, _ := p.Generate(verifycode.CodeConfig{Role: "guest", TTL: time.Millisecond})

	time.Sleep(5 * time.Millisecond)

	if _, err := p.Verify("user1", code); err == nil {
		t.Fatal("expired code should fail verification")
	}
}

func TestVerifyCode_ListValid(t *testing.T) {
	p := verifycode.NewPlugin(nil)
	p.Generate(verifycode.CodeConfig{Role: "a", TTL: time.Hour})
	p.Generate(verifycode.CodeConfig{Role: "b", TTL: time.Hour})

	list := p.ListValid()
	if len(list) != 2 {
		t.Fatalf("expected 2 valid codes, got %d", len(list))
	}
}

func TestVerifyCode_GC(t *testing.T) {
	p := verifycode.NewPlugin(nil)
	p.Generate(verifycode.CodeConfig{Role: "x", TTL: time.Millisecond})
	time.Sleep(5 * time.Millisecond)

	n := p.GC()
	if n != 1 {
		t.Fatalf("expected 1 expired code removed by GC, got %d", n)
	}
}

func TestVerifyCode_Descriptor(t *testing.T) {
	desc := verifycode.New(nil)
	if desc == nil || desc.Name != "verifycode" {
		t.Fatal("invalid descriptor")
	}
}
