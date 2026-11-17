// snapshot_hash.go — 快照指纹的稳定摘要实现。
//
// 指纹只有一个用途：判断"本轮上下文与上一轮是否完全相同"，从而决定是否
// 需要重写会话历史。它**不是**安全摘要，因此用 FNV-1a 就够——不引入哈希库
// 依赖，也不需要抗碰撞性。真正重要的是稳定：同一个字节串在任何进程、任何
// 顺序下都必须得到同一个值（FNV 满足，且没有 map 遍历）。

package promptctx

import (
	"hash/fnv"
	"strconv"
)

// itoa 是 strconv.Itoa 的短名（指纹拼接处会反复调用）。
func itoa(n int) string { return strconv.Itoa(n) }

// fingerprintHex 返回字节串的 FNV-1a 64 位十六进制表示（16 字符定长）。
func fingerprintHex(s string) string {
	h := fnv.New64a()
	// Write 对 *fnv 永不返回错误。
	_, _ = h.Write([]byte(s))
	return strconv.FormatUint(h.Sum64(), 16)
}
