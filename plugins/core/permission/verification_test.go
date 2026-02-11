package permission

import (
	"testing"
	"time"
)

func TestVerificationManager_GenerateCode(t *testing.T) {
	vm := NewVerificationManager()

	t.Run("generate code successfully", func(t *testing.T) {
		code, err := vm.GenerateCode("admin", 30*time.Minute, 0)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if code == "" {
			t.Error("Expected non-empty code")
		}

		if len(code) != 6 {
			t.Errorf("Expected code length 6, got %d", len(code))
		}
	})

	t.Run("code is unique", func(t *testing.T) {
		codes := make(map[string]bool)
		for i := 0; i < 100; i++ {
			code, err := vm.GenerateCode("admin", 30*time.Minute, 0)
			if err != nil {
				t.Fatalf("Failed to generate code: %v", err)
			}

			if codes[code] {
				t.Errorf("Generated duplicate code: %s", code)
			}
			codes[code] = true
		}
	})
}

func TestVerificationManager_VerifyCode(t *testing.T) {
	vm := NewVerificationManager()

	t.Run("verify valid code", func(t *testing.T) {
		code, _ := vm.GenerateCode("admin", 30*time.Minute, 0)

		role, success, err := vm.VerifyCode(code, "user123")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if !success {
			t.Error("Expected verification success")
		}

		if role != "admin" {
			t.Errorf("Expected role 'admin', got '%s'", role)
		}
	})

	t.Run("verify invalid code", func(t *testing.T) {
		_, success, err := vm.VerifyCode("INVALID", "user123")
		if err == nil {
			t.Error("Expected error for invalid code")
		}

		if success {
			t.Error("Expected verification failure")
		}
	})

	t.Run("one-time code can only be used once", func(t *testing.T) {
		code, _ := vm.GenerateCode("admin", 30*time.Minute, 0)

		// First use
		_, success, err := vm.VerifyCode(code, "user1")
		if err != nil || !success {
			t.Fatal("First use should succeed")
		}

		// Second use should fail
		_, success, err = vm.VerifyCode(code, "user2")
		if err == nil {
			t.Error("Expected error for second use of one-time code")
		}

		if success {
			t.Error("Expected verification failure for second use")
		}
	})

	t.Run("multi-use code respects max uses", func(t *testing.T) {
		code, _ := vm.GenerateCode("admin", 30*time.Minute, 2)

		// First use
		_, success, _ := vm.VerifyCode(code, "user1")
		if !success {
			t.Error("First use should succeed")
		}

		// Second use
		_, success, _ = vm.VerifyCode(code, "user2")
		if !success {
			t.Error("Second use should succeed")
		}

		// Third use should fail
		_, success, err := vm.VerifyCode(code, "user3")
		if err == nil {
			t.Error("Expected error for exceeding max uses")
		}

		if success {
			t.Error("Expected verification failure for exceeding max uses")
		}
	})

	t.Run("unlimited use code", func(t *testing.T) {
		code, _ := vm.GenerateCode("admin", 30*time.Minute, -1)

		// Use multiple times
		for i := 0; i < 10; i++ {
			_, success, err := vm.VerifyCode(code, "user"+string(rune(i)))
			if err != nil {
				t.Fatalf("Use %d failed: %v", i, err)
			}

			if !success {
				t.Errorf("Use %d should succeed", i)
			}
		}
	})

	t.Run("expired code cannot be used", func(t *testing.T) {
		code, _ := vm.GenerateCode("admin", 1*time.Millisecond, 0)

		// Wait for expiration
		time.Sleep(10 * time.Millisecond)

		_, success, err := vm.VerifyCode(code, "user123")
		if err == nil {
			t.Error("Expected error for expired code")
		}

		if success {
			t.Error("Expected verification failure for expired code")
		}
	})
}

func TestVerificationManager_RevokeCode(t *testing.T) {
	vm := NewVerificationManager()

	t.Run("revoke existing code", func(t *testing.T) {
		code, _ := vm.GenerateCode("admin", 30*time.Minute, 0)

		err := vm.RevokeCode(code)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		// Try to use revoked code
		_, success, err := vm.VerifyCode(code, "user123")
		if err == nil {
			t.Error("Expected error for revoked code")
		}

		if success {
			t.Error("Expected verification failure for revoked code")
		}
	})

	t.Run("revoke non-existent code", func(t *testing.T) {
		err := vm.RevokeCode("NONEXIST")
		if err == nil {
			t.Error("Expected error for non-existent code")
		}
	})
}

func TestVerificationManager_ListCodes(t *testing.T) {
	vm := NewVerificationManager()

	t.Run("list empty codes", func(t *testing.T) {
		codes := vm.ListCodes()
		if len(codes) != 0 {
			t.Errorf("Expected 0 codes, got %d", len(codes))
		}
	})

	t.Run("list active codes", func(t *testing.T) {
		// Generate some codes
		vm.GenerateCode("admin", 30*time.Minute, 0)
		vm.GenerateCode("user", 1*time.Hour, 1)
		vm.GenerateCode("moderator", 2*time.Hour, -1)

		codes := vm.ListCodes()
		if len(codes) != 3 {
			t.Errorf("Expected 3 codes, got %d", len(codes))
		}
	})

	t.Run("expired codes not listed", func(t *testing.T) {
		vm := NewVerificationManager()

		// Generate an expired code
		vm.GenerateCode("admin", 1*time.Millisecond, 0)
		time.Sleep(10 * time.Millisecond)

		codes := vm.ListCodes()
		if len(codes) != 0 {
			t.Errorf("Expected 0 codes (expired), got %d", len(codes))
		}
	})
}

func TestVerificationManager_CleanupExpired(t *testing.T) {
	vm := NewVerificationManager()

	t.Run("cleanup removes expired codes", func(t *testing.T) {
		// Generate some codes
		vm.GenerateCode("admin", 1*time.Millisecond, 0)
		vm.GenerateCode("user", 1*time.Millisecond, 0)
		vm.GenerateCode("moderator", 30*time.Minute, 0) // Not expired

		// Wait for expiration
		time.Sleep(10 * time.Millisecond)

		count := vm.CleanupExpired()
		if count != 2 {
			t.Errorf("Expected 2 expired codes, got %d", count)
		}

		// Only 1 code should remain
		codes := vm.ListCodes()
		if len(codes) != 1 {
			t.Errorf("Expected 1 remaining code, got %d", len(codes))
		}
	})
}

func TestVerificationManager_GetCodeInfo(t *testing.T) {
	vm := NewVerificationManager()

	t.Run("get existing code info", func(t *testing.T) {
		code, _ := vm.GenerateCode("admin", 30*time.Minute, 2)

		info, err := vm.GetCodeInfo(code)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if info.Code != code {
			t.Errorf("Expected code '%s', got '%s'", code, info.Code)
		}

		if info.Role != "admin" {
			t.Errorf("Expected role 'admin', got '%s'", info.Role)
		}

		if info.MaxUses != 2 {
			t.Errorf("Expected max uses 2, got %d", info.MaxUses)
		}

		if info.UseCount != 0 {
			t.Errorf("Expected use count 0, got %d", info.UseCount)
		}
	})

	t.Run("get non-existent code info", func(t *testing.T) {
		_, err := vm.GetCodeInfo("NONEXIST")
		if err == nil {
			t.Error("Expected error for non-existent code")
		}
	})
}

func TestGenerateRandomCode(t *testing.T) {
	t.Run("generates correct length", func(t *testing.T) {
		for length := 4; length <= 10; length++ {
			code, err := generateRandomCode(length)
			if err != nil {
				t.Fatalf("Failed to generate code: %v", err)
			}

			if len(code) != length {
				t.Errorf("Expected length %d, got %d", length, len(code))
			}
		}
	})

	t.Run("uses safe charset", func(t *testing.T) {
		const safeCharset = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

		for i := 0; i < 100; i++ {
			code, _ := generateRandomCode(6)

			for _, ch := range code {
				found := false
				for _, valid := range safeCharset {
					if ch == valid {
						found = true
						break
					}
				}

				if !found {
					t.Errorf("Code contains invalid character: %c", ch)
				}
			}
		}
	})
}
