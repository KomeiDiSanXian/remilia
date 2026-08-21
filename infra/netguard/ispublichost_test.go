package netguard

import "testing"

func TestIsPublicHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"8.8.8.8", true},
		{"127.0.0.1", false},
		{"192.168.1.1", false},
		{"169.254.1.1", false},
		{"::1", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsPublicHost(c.host); got != c.want {
			t.Errorf("IsPublicHost(%q)=%v, want %v", c.host, got, c.want)
		}
	}
}
