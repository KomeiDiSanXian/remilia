package permission

import (
	"fmt"
	"testing"
)

func TestAccessControlList_SetMode(t *testing.T) {
	acl := NewAccessControlList()

	t.Run("default mode is disabled", func(t *testing.T) {
		mode := acl.GetMode()
		if mode != ModeDisabled {
			t.Errorf("Expected default mode ModeDisabled, got %v", mode)
		}
	})

	t.Run("set blacklist mode", func(t *testing.T) {
		acl.SetMode(ModeBlacklist)
		mode := acl.GetMode()
		if mode != ModeBlacklist {
			t.Errorf("Expected ModeBlacklist, got %v", mode)
		}
	})

	t.Run("set whitelist mode", func(t *testing.T) {
		acl.SetMode(ModeWhitelist)
		mode := acl.GetMode()
		if mode != ModeWhitelist {
			t.Errorf("Expected ModeWhitelist, got %v", mode)
		}
	})
}

func TestAccessControlList_Add_Remove(t *testing.T) {
	acl := NewAccessControlList()

	t.Run("add user", func(t *testing.T) {
		acl.Add("user1", "test user")
		if !acl.Contains("user1") {
			t.Error("User should be in the list")
		}
	})

	t.Run("add user without note", func(t *testing.T) {
		acl.Add("user2", "")
		if !acl.Contains("user2") {
			t.Error("User should be in the list")
		}
	})

	t.Run("remove existing user", func(t *testing.T) {
		removed := acl.Remove("user1")
		if !removed {
			t.Error("Should return true for existing user")
		}
		if acl.Contains("user1") {
			t.Error("User should not be in the list")
		}
	})

	t.Run("remove non-existing user", func(t *testing.T) {
		removed := acl.Remove("nonexistent")
		if removed {
			t.Error("Should return false for non-existing user")
		}
	})
}

func TestAccessControlList_IsAllowed_Disabled(t *testing.T) {
	acl := NewAccessControlList()
	acl.SetMode(ModeDisabled)
	acl.Add("user1", "")

	t.Run("all users allowed in disabled mode", func(t *testing.T) {
		allowed, _ := acl.IsAllowed("user1")
		if !allowed {
			t.Error("User should be allowed in disabled mode")
		}

		allowed, _ = acl.IsAllowed("user2")
		if !allowed {
			t.Error("User should be allowed in disabled mode")
		}
	})
}

func TestAccessControlList_IsAllowed_Blacklist(t *testing.T) {
	acl := NewAccessControlList()
	acl.SetMode(ModeBlacklist)
	acl.Add("user1", "banned user")

	t.Run("blacklisted user denied", func(t *testing.T) {
		allowed, reason := acl.IsAllowed("user1")
		if allowed {
			t.Error("Blacklisted user should be denied")
		}
		if reason == "" {
			t.Error("Reason should not be empty")
		}
	})

	t.Run("non-blacklisted user allowed", func(t *testing.T) {
		allowed, reason := acl.IsAllowed("user2")
		if !allowed {
			t.Error("Non-blacklisted user should be allowed")
		}
		if reason != "" {
			t.Error("Reason should be empty for allowed user")
		}
	})
}

func TestAccessControlList_IsAllowed_Whitelist(t *testing.T) {
	acl := NewAccessControlList()
	acl.SetMode(ModeWhitelist)
	acl.Add("user1", "allowed user")

	t.Run("whitelisted user allowed", func(t *testing.T) {
		allowed, reason := acl.IsAllowed("user1")
		if !allowed {
			t.Error("Whitelisted user should be allowed")
		}
		if reason != "" {
			t.Error("Reason should be empty for allowed user")
		}
	})

	t.Run("non-whitelisted user denied", func(t *testing.T) {
		allowed, reason := acl.IsAllowed("user2")
		if allowed {
			t.Error("Non-whitelisted user should be denied")
		}
		if reason == "" {
			t.Error("Reason should not be empty")
		}
	})
}

func TestAccessControlList_List(t *testing.T) {
	acl := NewAccessControlList()

	t.Run("empty list", func(t *testing.T) {
		users := acl.List()
		if len(users) != 0 {
			t.Errorf("Expected 0 users, got %d", len(users))
		}
	})

	t.Run("list with users", func(t *testing.T) {
		acl.Add("user1", "note1")
		acl.Add("user2", "note2")
		acl.Add("user3", "")

		users := acl.List()
		if len(users) != 3 {
			t.Errorf("Expected 3 users, got %d", len(users))
		}

		// Check if all users are in the list
		userMap := make(map[string]bool)
		for _, user := range users {
			userMap[user.UserID] = true
		}

		if !userMap["user1"] || !userMap["user2"] || !userMap["user3"] {
			t.Error("Not all users found in list")
		}
	})
}

func TestAccessControlList_Count(t *testing.T) {
	acl := NewAccessControlList()

	t.Run("count empty list", func(t *testing.T) {
		count := acl.Count()
		if count != 0 {
			t.Errorf("Expected count 0, got %d", count)
		}
	})

	t.Run("count with users", func(t *testing.T) {
		acl.Add("user1", "")
		acl.Add("user2", "")
		acl.Add("user3", "")

		count := acl.Count()
		if count != 3 {
			t.Errorf("Expected count 3, got %d", count)
		}
	})
}

func TestAccessControlList_Clear(t *testing.T) {
	acl := NewAccessControlList()
	acl.Add("user1", "")
	acl.Add("user2", "")
	acl.Add("user3", "")

	t.Run("clear list", func(t *testing.T) {
		count := acl.Clear()
		if count != 3 {
			t.Errorf("Expected cleared count 3, got %d", count)
		}

		if acl.Count() != 0 {
			t.Errorf("List should be empty after clear")
		}
	})
}

func TestAccessControlList_Note(t *testing.T) {
	acl := NewAccessControlList()

	t.Run("get note", func(t *testing.T) {
		acl.Add("user1", "test note")
		note := acl.GetNote("user1")
		if note != "test note" {
			t.Errorf("Expected 'test note', got '%s'", note)
		}
	})

	t.Run("set note", func(t *testing.T) {
		acl.Add("user2", "")
		acl.SetNote("user2", "updated note")
		note := acl.GetNote("user2")
		if note != "updated note" {
			t.Errorf("Expected 'updated note', got '%s'", note)
		}
	})

	t.Run("get note for non-existing user", func(t *testing.T) {
		note := acl.GetNote("nonexistent")
		if note != "" {
			t.Error("Note should be empty for non-existing user")
		}
	})
}

func TestAccessControlList_Stats(t *testing.T) {
	acl := NewAccessControlList()

	t.Run("stats for empty list", func(t *testing.T) {
		stats := acl.Stats()
		if stats.UserCount != 0 {
			t.Errorf("Expected user count 0, got %d", stats.UserCount)
		}
		if stats.Mode != ModeDisabled {
			t.Errorf("Expected mode ModeDisabled, got %v", stats.Mode)
		}
	})

	t.Run("stats with users", func(t *testing.T) {
		acl.SetMode(ModeBlacklist)
		acl.Add("user1", "")
		acl.Add("user2", "")

		stats := acl.Stats()
		if stats.UserCount != 2 {
			t.Errorf("Expected user count 2, got %d", stats.UserCount)
		}
		if stats.Mode != ModeBlacklist {
			t.Errorf("Expected mode ModeBlacklist, got %v", stats.Mode)
		}
	})
}

func TestListMode_String(t *testing.T) {
	tests := []struct {
		mode     ListMode
		expected string
	}{
		{ModeDisabled, "禁用"},
		{ModeBlacklist, "黑名单"},
		{ModeWhitelist, "白名单"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			str := tt.mode.String()
			if str != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, str)
			}
		})
	}
}

func TestAccessControlList_Concurrent(t *testing.T) {
	acl := NewAccessControlList()
	acl.SetMode(ModeBlacklist)

	// Test concurrent add and check
	t.Run("concurrent operations", func(t *testing.T) {
		done := make(chan bool)

		// Add users concurrently
		for i := 0; i < 10; i++ {
			go func(id int) {
				userID := fmt.Sprintf("user%d", id)
				acl.Add(userID, "concurrent test")
				done <- true
			}(i)
		}

		// Wait for all additions
		for i := 0; i < 10; i++ {
			<-done
		}

		// Check count
		count := acl.Count()
		if count != 10 {
			t.Errorf("Expected count 10, got %d", count)
		}

		// Check concurrent access
		for i := 0; i < 10; i++ {
			go func(id int) {
				userID := fmt.Sprintf("user%d", id)
				allowed, _ := acl.IsAllowed(userID)
				if allowed {
					t.Error("User should be denied in blacklist mode")
				}
				done <- true
			}(i)
		}

		// Wait for all checks
		for i := 0; i < 10; i++ {
			<-done
		}
	})
}
