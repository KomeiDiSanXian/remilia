package updater

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"1.30.0", false},
		{"v1.30.0", false},
		{"V1.2.3", false},
		{"1.2", false},
		{"1", false},
		{"1.30.0-rc.1", false},
		{"1.30.0-beta.2+build.5", false},
		{"", true},
		{"abc", true},
		{"1.x.0", true},
		{"1.2.3.4", true},
		{"1.2-", true},
		{"-1.2.3", true},
		{"1.2.3-rc..1", true},
	}
	for _, tt := range tests {
		_, err := parseVersion(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseVersion(%q) err=%v, wantErr=%v", tt.in, err, tt.wantErr)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int // 正数: a>b; 负数: a<b; 0: 相等
	}{
		{"1.30.0", "1.29.0", 1},
		{"1.2.0", "1.2.1", -1},
		{"1.2.3", "1.2.3", 0},
		{"v1.2.3", "1.2.3", 0},
		{"2.0.0", "10.0.0", -1},
		{"1.10.0", "1.9.9", 1},
		{"1.0.0", "1.0.0-rc.1", 1}, // 正式版 > 预发布
		{"1.0.0-rc.1", "1.0.0-rc.2", -1},
		{"1.0.0-rc.1", "1.0.0-beta.1", 1}, // rc > beta
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-1", "1.0.0-alpha", -1}, // 数字标识符 < 字母数字
		{"1.0.0-rc.1", "1.0.0", -1},
	}
	for _, tt := range tests {
		va, errA := parseVersion(tt.a)
		vb, errB := parseVersion(tt.b)
		if errA != nil || errB != nil {
			t.Fatalf("parse failed: a=%v b=%v", errA, errB)
		}
		got := va.Compare(vb)
		pass := (got > 0 && tt.want > 0) || (got < 0 && tt.want < 0) || (got == 0 && tt.want == 0)
		if !pass {
			t.Errorf("Compare(%s, %s) = %d, want sign %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSameVersion(t *testing.T) {
	if !sameVersion("1.30.0", "v1.30.0") {
		t.Error("sameVersion should ignore v prefix")
	}
	if sameVersion("1.30.0", "1.30.1") {
		t.Error("sameVersion must reject different versions")
	}
}
