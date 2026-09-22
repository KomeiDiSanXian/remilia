package ai

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

func TestNewSkillRegistry(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	if r == nil {
		t.Fatal("NewSkillRegistry returned nil")
	}
}

func TestSkillRegistryAddAndGet(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	skill := toolkit.Skill{
		Name:        "test_skill",
		OwnerID:     toolkit.OwnerSystem,
		Description: "a test skill",
		Prompt:      "You are a test skill",
		Enabled:     true,
	}

	err := r.Add(skill)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, ok := r.GetSystem("test_skill")
	if !ok {
		t.Fatal("expected to find skill")
	}
	if got.Name != "test_skill" {
		t.Errorf("expected name %q, got %q", "test_skill", got.Name)
	}
	if got.Description != "a test skill" {
		t.Errorf("expected description %q, got %q", "a test skill", got.Description)
	}
}

func TestSkillRegistryAddDuplicate(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Add(toolkit.Skill{Name: "dup", OwnerID: toolkit.OwnerSystem})
	err := r.Add(toolkit.Skill{Name: "dup", OwnerID: toolkit.OwnerSystem})
	if err == nil {
		t.Error("expected error for duplicate skill")
	}
}

func TestSkillRegistryAddEmptyName(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	err := r.Add(toolkit.Skill{Name: "", OwnerID: toolkit.OwnerSystem})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestSkillRegistryAddEmptyOwner(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	err := r.Add(toolkit.Skill{Name: "test", OwnerID: ""})
	if err == nil {
		t.Error("expected error for empty owner")
	}
}

func TestSkillRegistryGetSystem(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Add(toolkit.Skill{Name: "sys_skill", OwnerID: toolkit.OwnerSystem})

	_, ok := r.GetSystem("sys_skill")
	if !ok {
		t.Error("expected to find system skill")
	}
	_, ok = r.GetSystem("nonexistent")
	if ok {
		t.Error("expected false for nonexistent skill")
	}
}

func TestSkillRegistryGetByOwner(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Add(toolkit.Skill{Name: "user_skill", OwnerID: "user123"})

	_, ok := r.GetByOwner("user123", "user_skill")
	if !ok {
		t.Error("expected to find user skill")
	}
	_, ok = r.GetByOwner("other_user", "user_skill")
	if ok {
		t.Error("other user should not see this skill")
	}
}

func TestSkillRegistrySetEnabled(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Add(toolkit.Skill{Name: "togglable", OwnerID: "user1", Enabled: true})

	s, err := r.SetEnabled("user1", "togglable", false)
	if err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}
	if s.Enabled {
		t.Error("skill should be disabled")
	}
	if s.Name != "togglable" {
		t.Errorf("name changed unexpectedly")
	}
}

func TestSkillRegistrySetEnabledNotFound(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	_, err := r.SetEnabled("user1", "nonexistent", false)
	if err == nil {
		t.Error("expected error for nonexistent skill")
	}
}

func TestSkillRegistryList(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Add(toolkit.Skill{Name: "s1", OwnerID: toolkit.OwnerSystem})
	r.Add(toolkit.Skill{Name: "s2", OwnerID: toolkit.OwnerSystem})

	skills := r.List()
	if len(skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(skills))
	}
}

func TestSkillRegistryListByOwner(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Add(toolkit.Skill{Name: "a", OwnerID: "user1"})
	r.Add(toolkit.Skill{Name: "b", OwnerID: "user1"})
	r.Add(toolkit.Skill{Name: "c", OwnerID: "user2"})

	user1Skills := r.ListByOwner("user1")
	if len(user1Skills) != 2 {
		t.Errorf("expected 2 skills for user1, got %d", len(user1Skills))
	}
	user2Skills := r.ListByOwner("user2")
	if len(user2Skills) != 1 {
		t.Errorf("expected 1 skill for user2, got %d", len(user2Skills))
	}
	noSkills := r.ListByOwner("nobody")
	if len(noSkills) != 0 {
		t.Errorf("expected 0 skills for unknown user, got %d", len(noSkills))
	}
}

func TestSkillRegistryRemove(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Add(toolkit.Skill{Name: "removable", OwnerID: "user1"})

	err := r.Remove("removable", "user1")
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	_, ok := r.GetByOwner("user1", "removable")
	if ok {
		t.Error("skill should be removed")
	}
}

func TestSkillRegistryRemoveNotFound(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	err := r.Remove("nonexistent", "user1")
	if err == nil {
		t.Error("expected error for removing nonexistent skill")
	}
}

func TestSkillRegistryRemoveWrongOwner(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Add(toolkit.Skill{Name: "skill_a", OwnerID: "user1"})
	err := r.Remove("skill_a", "user2")
	if err == nil {
		t.Error("expected error when removing someone else's skill")
	}
}

func TestSkillRegistryIncrementUsage(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Add(toolkit.Skill{Name: "counter", OwnerID: toolkit.OwnerSystem, UsageCount: 0})

	r.IncrementUsage(toolkit.OwnerSystem, "counter")
	r.IncrementUsage(toolkit.OwnerSystem, "counter")
	r.IncrementUsage(toolkit.OwnerSystem, "counter")

	s, _ := r.GetSystem("counter")
	if s.UsageCount != 3 {
		t.Errorf("expected usage count 3, got %d", s.UsageCount)
	}
}

func TestSkillRegistryIncrementUsageNotFound(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.IncrementUsage("nobody", "nothing") // should not panic
}

func TestSkillRegistryPromote(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Add(toolkit.Skill{Name: "u_user_skill", OwnerID: "user1", Prompt: "test"})

	err := r.Promote("u_user_skill", "user1")
	if err != nil {
		t.Fatalf("Promote failed: %v", err)
	}

	_, userOk := r.GetByOwner("user1", "u_user_skill")
	if userOk {
		t.Error("promoted skill should not remain in user's skills")
	}

	sys, sysOk := r.GetSystem("user_skill")
	if !sysOk {
		t.Error("promoted skill should be available as system skill")
	}
	if sys.OwnerID != toolkit.OwnerSystem {
		t.Errorf("expected OwnerSystem, got %q", sys.OwnerID)
	}
	if sys.Prompt != "test" {
		t.Errorf("prompt should be preserved, got %q", sys.Prompt)
	}
}

func TestSkillRegistryPromoteConflict(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Add(toolkit.Skill{Name: "u_conflict", OwnerID: "user1"})
	r.Add(toolkit.Skill{Name: "conflict", OwnerID: toolkit.OwnerSystem})

	err := r.Promote("u_conflict", "user1")
	if err == nil {
		t.Error("expected error when system skill already exists with same name")
	}
}

func TestSkillRegistryPromoteNotFound(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	err := r.Promote("nonexistent", "user1")
	if err == nil {
		t.Error("expected error for promoting nonexistent skill")
	}
}

func TestSkillRegistryRegister(t *testing.T) {
	r := toolkit.NewSkillRegistry()
	r.Register(toolkit.Skill{Name: "reg_skill", OwnerID: toolkit.OwnerSystem})
	r.Register(toolkit.Skill{Name: "reg_skill", OwnerID: toolkit.OwnerSystem}) // duplicate should not panic

	_, ok := r.GetSystem("reg_skill")
	if !ok {
		t.Error("expected to find registered skill")
	}
}
