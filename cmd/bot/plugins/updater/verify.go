package updater

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// parseChecksums 解析 goreleaser 生成的 checksums.txt：
// 每行 `<sha256 hex>  <文件名>`（分隔符为两个空格，文件名可能含空格）。
func parseChecksums(r io.Reader) (map[string]string, error) {
	sums := make(map[string]string)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("checksums.txt 格式错误: %q", line)
		}
		sumHex := fields[0]
		name := strings.TrimSpace(strings.TrimPrefix(line, sumHex))
		if len(sumHex) != sha256.Size*2 {
			return nil, fmt.Errorf("checksums.txt 哈希长度错误: %q", line)
		}
		if _, err := hex.DecodeString(sumHex); err != nil {
			return nil, fmt.Errorf("checksums.txt 哈希非十六进制: %q", line)
		}
		sums[name] = strings.ToLower(sumHex)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("读取 checksums.txt 失败: %w", err)
	}
	return sums, nil
}

// verifyFileSHA256 校验文件的 sha256 与期望值一致。
func verifyFileSHA256(path, wantHex string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("计算文件哈希失败: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantHex) {
		return fmt.Errorf("sha256 校验失败：期望 %s，实际 %s", wantHex, got)
	}
	return nil
}
