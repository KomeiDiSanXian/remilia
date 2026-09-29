package catalog

import "testing"

func TestAvailabilitySemantics(t *testing.T) {
	cases := []struct {
		state      Availability
		inCatalog  bool
		selectable bool
		soft       bool
	}{
		{AvailabilityReady, true, true, false},
		{AvailabilityPolicy, true, false, false},
		{AvailabilityProvider, true, false, true},
		{AvailabilityDisconnected, true, true, true},
		{AvailabilityDisabled, true, false, false},
		{AvailabilityRemoved, false, false, false},
	}
	for _, c := range cases {
		if got := c.state.InCatalog(); got != c.inCatalog {
			t.Errorf("%s.InCatalog() = %v want %v", c.state, got, c.inCatalog)
		}
		if got := c.state.Selectable(); got != c.selectable {
			t.Errorf("%s.Selectable() = %v want %v", c.state, got, c.selectable)
		}
		if got := c.state.Soft(); got != c.soft {
			t.Errorf("%s.Soft() = %v want %v", c.state, got, c.soft)
		}
	}
}

func TestMembershipChanged(t *testing.T) {
	if MembershipChanged(AvailabilityDisconnected, AvailabilityReady) {
		t.Fatal("soft-unavailable recovery must not change membership")
	}
	if MembershipChanged(AvailabilityReady, AvailabilityDisabled) {
		t.Fatal("policy/disable switch must not change membership")
	}
	if !MembershipChanged(AvailabilityReady, AvailabilityRemoved) {
		t.Fatal("removal must change membership")
	}
	if !MembershipChanged(AvailabilityRemoved, AvailabilityReady) {
		t.Fatal("re-appearance must change membership")
	}
}
