package webhook

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderSignature = "X-Signature-Ed25519"
	HeaderTimestamp = "X-Signature-Timestamp"
)

type ed25519Key struct {
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
}

// deriveKey 按腾讯协议规则从 AppSecret 派生 Ed25519 密钥对。
//
// 协议规定的派生方式：把 AppSecret 不断加倍直到长度 ≥ ed25519.SeedSize，
// 再截断到 32 字节作为 Ed25519 的 seed。这是协议的一部分，不能用
// SHA-256 或其他 KDF 替代，否则签名与官方 SDK 不兼容。
//
// 这里用 ed25519.NewKeyFromSeed 明确表达"从 seed 恢复私钥"的意图，与官方
// ed25519.GenerateKey(strings.NewReader(seed)) 结果逐字节一致（Go 源码中
// GenerateKey 就是 ReadFull(rand, seed) 后调用 NewKeyFromSeed），只是不再把
// 确定性的密钥派生误用为 CSPRNG 接口。调用方保证 secret 非空。
func deriveKey(secret string) *ed25519Key {
	seed := secret
	for len(seed) < ed25519.SeedSize {
		seed += seed
	}
	seed = seed[:ed25519.SeedSize]
	privateKey := ed25519.NewKeyFromSeed([]byte(seed))
	return &ed25519Key{
		publicKey:  privateKey.Public().(ed25519.PublicKey),
		privateKey: privateKey,
	}
}

func (c *Conn) decodeSign(sign string) ([]byte, error) {
	if sign == "" {
		return nil, fmt.Errorf("signature is empty")
	}
	signBuf, err := hex.DecodeString(sign)
	if err != nil {
		return nil, fmt.Errorf("decode signature failed: %w", err)
	}
	if len(signBuf) != ed25519.SignatureSize || signBuf[63]&224 != 0 {
		return nil, fmt.Errorf("invalid signature")
	}
	return signBuf, nil
}

// signatureMaxSkew 是签名时间戳允许的最大偏差（前后各计）。
//
// 缺少这个窗口时，签名是永久有效的：任何一次被截获（日志、抓包、
// 中间代理）的合法回调都能被无限次重放，重复触发指令、重复入账。
// 取 5 分钟以容忍常见的服务器时钟漂移。
const signatureMaxSkew = 5 * time.Minute

func (c *Conn) buildMsg(timestamp string, body []byte) ([]byte, error) {
	if timestamp == "" {
		return nil, fmt.Errorf("timestamp is empty")
	}
	if err := checkTimestampSkew(timestamp); err != nil {
		return nil, err
	}
	var msg bytes.Buffer
	_, _ = msg.WriteString(timestamp)
	_, _ = msg.Write(body)
	return msg.Bytes(), nil
}

// checkTimestampSkew 校验签名时间戳是否落在允许的时间窗内。
//
// 同时兼容两种形态：Unix 秒（数字）与 RFC3339（含 Nano）。
// 两种都解析不了时保守放行——此处的职责是收窄重放窗口，
// 不应因为一个未预期的时间戳格式把正常回调全部挡在门外；
// 真正的真伪判定仍由后续的 Ed25519 校验负责。
func checkTimestampSkew(timestamp string) error {
	ts := strings.TrimSpace(timestamp)

	var t time.Time
	if n, err := strconv.ParseInt(ts, 10, 64); err == nil {
		// 13 位视为毫秒，10 位视为秒；避免把毫秒时间戳误判为遥远的未来
		// 而把所有正常回调全部拒掉。
		if len(ts) >= 13 {
			t = time.UnixMilli(n)
		} else {
			t = time.Unix(n, 0)
		}
	} else if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		t = parsed
	} else {
		return nil
	}

	skew := time.Since(t)
	if skew < 0 {
		skew = -skew
	}
	if skew > signatureMaxSkew {
		return fmt.Errorf("signature timestamp out of range: skew=%v, max=%v",
			skew.Truncate(time.Second), signatureMaxSkew)
	}
	return nil
}

// genKey 返回缓存的 Ed25519 密钥对。
//
// AppSecret 在构造时即固定（见 NewWithBuffer），因此密钥只派生一次，
// 避免每个请求都重新走一遍 seed 扩展 + 密钥派生。
//
// key 为 nil 表示没有可用密钥：NewWebhookServerAdapter / SimpleWebhookAdapter
// 都明确支持传入 nil BotInfo。缺少此判断时，任何带合法长度签名头的匿名请求
// 都会在空指针上 panic（net/http 逐连接 recover，于是退化为可被外部无限触发
// 的日志风暴 + 连接重置，适配器 100% 不可用）。
func (c *Conn) genKey() (*ed25519Key, error) {
	if c.key != nil {
		return c.key, nil
	}
	if c.info == nil {
		return nil, fmt.Errorf("bot info is nil")
	}
	return nil, fmt.Errorf("app secret is empty")
}

// Verify verifies the signature of the request
func (c *Conn) Verify(header http.Header, body []byte) (bool, error) {
	key, err := c.genKey()
	if err != nil {
		return false, err
	}
	sign, err := c.decodeSign(header.Get(HeaderSignature))
	if err != nil {
		return false, err
	}
	msg, err := c.buildMsg(header.Get(HeaderTimestamp), body)
	if err != nil {
		return false, err
	}
	return ed25519.Verify(key.publicKey, msg, sign), nil
}

// Sign generates the signature of the request
func (c *Conn) Sign(header http.Header, body []byte) ([]byte, error) {
	key, err := c.genKey()
	if err != nil {
		return nil, err
	}
	msg, err := c.buildMsg(header.Get(HeaderTimestamp), body)
	if err != nil {
		return nil, err
	}
	sign := ed25519.Sign(key.privateKey, msg)
	return sign, nil
}
