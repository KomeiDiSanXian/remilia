package execution

import "testing"

func TestIsSafeCommandArg(t *testing.T) {
	tests := []struct {
		arg  string
		want bool
	}{
		{"hello world", true},
		{"", true},
		{"abc123", true},
		{"\t", true},
		{"\n", false},
		{"\x00", false},
		{"\x1b", false},
		{"\x7F", false},
		{"中文", false},
		{string(make([]byte, 4097)), false},
	}
	for _, tt := range tests {
		got := IsSafeCommandArg(tt.arg)
		if got != tt.want {
			t.Errorf("IsSafeCommandArg(%q) = %v, want %v", tt.arg, got, tt.want)
		}
	}
}

func TestParseToolPermission(t *testing.T) {
	cases := []struct {
		in           string
		wantRes, act string
	}{
		{"bilibili.manage", "bilibili", "manage"},
		{"bilibili:manage", "bilibili", "manage"},
		{"bilibili", "bilibili", "*"},
		{"*", "*", "*"},
	}
	for _, tt := range cases {
		res, act := ParseToolPermission(tt.in)
		if res != tt.wantRes || act != tt.act {
			t.Errorf("ParseToolPermission(%q) = (%q,%q), want (%q,%q)", tt.in, res, act, tt.wantRes, tt.act)
		}
	}
}
