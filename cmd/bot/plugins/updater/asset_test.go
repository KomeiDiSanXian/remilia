package updater

import (
	"testing"
)

func TestExpectedAssetName(t *testing.T) {
	tests := []struct {
		goos, goarch, goarm string
		want                string
	}{
		{"linux", "amd64", "", "remilia_Linux_x86_64.tar.gz"},
		{"linux", "386", "", "remilia_Linux_i386.tar.gz"},
		{"linux", "arm64", "", "remilia_Linux_arm64.tar.gz"},
		{"linux", "arm", "7", "remilia_Linux_armv7.tar.gz"},
		{"linux", "arm", "6", "remilia_Linux_armv6.tar.gz"},
		{"linux", "arm", "", "remilia_Linux_armv7.tar.gz"}, // 缺省按 v7
		{"linux", "riscv64", "", "remilia_Linux_riscv64.tar.gz"},
		{"windows", "amd64", "", "remilia_Windows_x86_64.zip"},
		{"windows", "arm64", "", "remilia_Windows_arm64.zip"},
		{"darwin", "amd64", "", "remilia_Darwin_x86_64.tar.gz"},
		{"darwin", "arm64", "", "remilia_Darwin_arm64.tar.gz"},
	}
	for _, tt := range tests {
		got := expectedAssetName(tt.goos, tt.goarch, tt.goarm)
		if got != tt.want {
			t.Errorf("expectedAssetName(%s,%s,%s) = %q, want %q",
				tt.goos, tt.goarch, tt.goarm, got, tt.want)
		}
	}
}

func TestSplitRepo(t *testing.T) {
	owner, repo := splitRepo("KomeiDiSanXian/remilia")
	if owner != "KomeiDiSanXian" || repo != "remilia" {
		t.Errorf("split = %s/%s", owner, repo)
	}
	owner, repo = splitRepo("")
	if owner == "" || repo == "" {
		t.Error("empty config should fall back to default repo")
	}
	owner, repo = splitRepo("  bad   ")
	if owner == "" || repo == "" {
		t.Error("malformed config should fall back to default repo")
	}
}

func TestBackupVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/data/remilia.old.v1.29.0", "1.29.0"},
		{"/data/remilia.old.1.28.0", "1.28.0"},
		{"/data/remilia", "unknown"},
	}
	for _, c := range cases {
		if got := backupVersion(c.in); got != c.want {
			t.Errorf("backupVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
