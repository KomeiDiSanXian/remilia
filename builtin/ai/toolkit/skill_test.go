package toolkit

import "testing"

func TestSkillKey(t *testing.T) {
	key := skillKey("owner1", "skill1")
	expected := "owner1\x00skill1"
	if key != expected {
		t.Errorf("expected %q, got %q", expected, key)
	}
}
