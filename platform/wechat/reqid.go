package wechat

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// newReqID 生成一个可用于关联请求与回执的 req_id。
//
// 采用 "<cmd>-<16 字节随机十六进制>"：企业微信只要求"由开发者自行生成、
// 不重复"（上行帧不超过 256 字节），带上前缀能让长连接抓包时一眼看出
// 这条回执对应哪个命令。
func newReqID(cmd string) (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("wechat: 生成 req_id 失败: %w", err)
	}
	return cmd + "-" + hex.EncodeToString(buf[:]), nil
}
