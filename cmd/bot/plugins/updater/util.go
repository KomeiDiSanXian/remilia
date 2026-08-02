package updater

import (
	"os"
	"os/exec"
	"strings"

	"github.com/KomeiDiSanXian/remilia"
)

// CurrentVersion 返回当前运行版本（ldflags 注入的 remilia.Version，不含 v 前缀）。
func CurrentVersion() string {
	return strings.TrimPrefix(remilia.Version, "v")
}

// sameVersion 比较两个版本字符串是否相等（容忍 v 前缀差异）。
func sameVersion(a, b string) bool {
	va, errA := parseVersion(a)
	vb, errB := parseVersion(b)
	if errA == nil && errB == nil {
		return va.Compare(vb) == 0
	}
	return strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v")
}

// newExecCommand 创建执行命令的薄封装（便于测试替换）。
var newExecCommand = func(path string, args ...string) *exec.Cmd {
	return exec.Command(path, args...)
}

// envWithout 返回继承自当前进程、但排除指定键的环境变量列表。
func envWithout(keys ...string) []string {
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		drop := false
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				drop = true
				break
			}
		}
		if !drop {
			env = append(env, kv)
		}
	}
	return env
}

// inContainer 探测是否运行在容器内：/.dockerenv 存在或显式环境变量声明。
func inContainer() bool {
	if os.Getenv("REMILIA_IN_CONTAINER") == "1" {
		return true
	}
	_, err := os.Stat("/.dockerenv")
	return err == nil
}
