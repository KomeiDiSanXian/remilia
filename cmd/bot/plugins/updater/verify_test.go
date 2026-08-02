package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseChecksums(t *testing.T) {
	content := `# goreleaser
1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef  remilia_Linux_x86_64.tar.gz
fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321  remilia_Windows_x86_64.zip

ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100  file with spaces.bin
`
	sums, err := parseChecksums(strings.NewReader(content))
	if err != nil {
		t.Fatalf("parseChecksums: %v", err)
	}
	if len(sums) != 3 {
		t.Fatalf("got %d entries, want 3", len(sums))
	}
	if sums["remilia_Linux_x86_64.tar.gz"] != "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef" {
		t.Error("linux entry wrong")
	}
	if _, ok := sums["file with spaces.bin"]; !ok {
		t.Error("name with spaces not parsed")
	}
}

func TestParseChecksumsBadLines(t *testing.T) {
	for _, content := range []string{
		"nothex  file.bin\n",
		"abc file.bin\n",
		"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef  \n", // 缺文件名
	} {
		if _, err := parseChecksums(strings.NewReader(content)); err == nil {
			t.Errorf("parseChecksums(%q) should fail", content)
		}
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	data := []byte("remilia updater test payload")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])

	if err := verifyFileSHA256(path, want); err != nil {
		t.Errorf("verify should pass: %v", err)
	}
	if err := verifyFileSHA256(path, strings.Repeat("0", 64)); err == nil {
		t.Error("verify should fail on wrong hash")
	}
	if err := verifyFileSHA256(filepath.Join(dir, "missing"), want); err == nil {
		t.Error("verify should fail on missing file")
	}
}
