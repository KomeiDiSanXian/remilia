package outbound

// architecture_test.go — 出站策略唯一入口的守卫。
//
// infra/netguard 提供策略原语（URL 准入、拨号校验、重定向判定），本包负责
// 把它们装配成"按目标来源分层"的客户端。业务包若直接引用 netguard，就会
// 重新长出"每个组件自己拼一套策略"的分叉——这正是此前 websearch / sauce /
// aimage / AI 附件各写一份、误伤与漏防并存的成因：
//
//   - sauce 把受信媒体名单挂在共享 Transport 上，同一 Transport 的其它路径
//     一并变松；
//   - aimage 自己实现"同源即可信"，AI 附件却只有配置名单；
//   - 策略误伤只留一行 Debug 日志，运维看到的是"功能坏了"。
//
// 因此这里把"谁能引用 netguard"钉死：只有本包。新增出站路径请用 [New] 选
// 一个 [Class]，而不是绕过策略或另起一套校验。
//
// 测试文件不在扫描范围内：单测可以继续直接断言 netguard 的原语行为。

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// netguardImport 是被限制引用的策略原语包。
const netguardImport = `"github.com/KomeiDiSanXian/remilia/infra/netguard"`

// netguardAllowedImporters 是允许引用 netguard 的目录（相对仓库根）。
var netguardAllowedImporters = map[string]bool{
	filepath.Join("infra", "outbound"): true,
}

// TestNetguardOnlyUsedByOutbound 扫描仓库 Go 源码（不含测试文件），禁止
// infra/outbound 之外的业务代码直接引用 netguard。
func TestNetguardOnlyUsedByOutbound(t *testing.T) {
	root := filepath.Join("..", "..")
	skipDirs := map[string]bool{
		".git": true, "vendor": true, "node_modules": true,
		"dashboarddist": true, "testdata": true, "docs": true,
		"dashboard": true, "desktop": true, "site": true,
	}

	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if netguardAllowedImporters[filepath.Dir(rel)] {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(data), netguardImport) {
			offenders = append(offenders, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描源码失败: %v", err)
	}

	for _, file := range offenders {
		t.Errorf("%s 直接引用了 infra/netguard：出站策略请改用 infra/outbound（按目标来源选 Untrusted/TrustedOrigin/OperatorEndpoint），不要另起一套校验", file)
	}
}
