package wechat

// wxcrypt.go — 企业微信回调的签名与 AES 加解密。
//
// 企业微信（含智能机器人）的回调协议是自研的「AES-256-CBC + SHA1 签名」：
//
//	AESKey = Base64_Decode(EncodingAESKey + "=")   // 32 字节
//	IV     = AESKey[:16]
//	明文   = random(16B) + msg_len(4B, 大端) + msg + receiveid
//	msg_encrypt = Base64(AES_Encrypt(明文))
//	msg_signature = sha1(sort(token, timestamp, nonce, msg_encrypt))
//
// 两处必须按企业微信的"非标准"细节实现，否则会出现"验签通过但解密乱码"：
//
//  1. 填充是 PKCS#7 但块大小取 **32**（不是 AES 的 16），且填充值可能落在
//     1..32 之间。用标准 16 字节填充去解，末尾会残留最多 16 个填充字节，
//     表现为 JSON 解析失败或图片尾部损坏。
//  2. 智能机器人场景 receiveid 为**空字符串**（自建应用才是 corpid）。
//     校验时按空串比对，写成"必须等于 BotID"会全量失败。
//
// 已知错误码语义（官方加解密库约定）：-40001 签名错误、-40005 receiveid 不符、
// -40007 AES 解密失败。本实现用包内 error 表达，靠 errors.Is 区分。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/jsonx"
)

// wecomPadBlockSize 是企业微信 PKCS#7 填充的块大小。
//
// 官方文档写的是"填充至 32 字节的倍数"。AES 分组是 16 字节，32 恰好是两倍，
// 因此密文长度依然是 16 的倍数，cipher.BlockMode 可以直接处理。
const wecomPadBlockSize = 32

// randomPrefixLen 是明文密文头部随机串的长度。
const randomPrefixLen = 16

// msgLenFieldLen 是明文密文头部 msg_len 字段的长度（4 字节，网络字节序）。
const msgLenFieldLen = 4

var (
	// errSignature 表示 msg_signature 校验失败（凭证不匹配或报文被篡改）。
	errSignature = errors.New("wechat: 回调签名校验失败")
	// errReceiveID 表示解密后的 receiveid 与期望值不符。
	errReceiveID = errors.New("wechat: 回调 receiveid 校验失败")
	// errDecrypt 表示 AES 解密失败（密文损坏、密钥不对或长度非法）。
	errDecrypt = errors.New("wechat: 回调 AES 解密失败")
)

// WXCrypt 负责企业微信回调的验签、解密与被动回复加密。
//
// 同一套密钥同时用于消息体和媒体文件（文件用每条消息自带的 aeskey，见
// [DecryptMedia]）。实例可并发使用：内部不保存可变状态。
type WXCrypt struct {
	token     string
	key       []byte
	receiveID string
}

// newWXCrypt 构造加解密器。
//
// receiveID 传空字符串即可：企业内部智能机器人的回调 receiveid 为空，
// 自建应用才是 corpid。传非空时解密后会强制比对，用于兼容自建应用场景。
func newWXCrypt(token, encodingAESKey, receiveID string) (*WXCrypt, error) {
	if encodingAESKey == "" {
		return nil, fmt.Errorf("wechat: encoding_aes_key 不能为空")
	}
	key, err := decodeAESKey(encodingAESKey)
	if err != nil {
		return nil, err
	}
	return &WXCrypt{
		token:     token,
		key:       key,
		receiveID: receiveID,
	}, nil
}

// decodeAESKey 把 43 位 EncodingAESKey 还原成 32 字节 AESKey。
//
// 企业微信给的是去掉尾部 "=" 的 Base64，必须补齐后解码；长度不足 32 字节
// （例如误填了 Token）时直接报错，而不是留到解密阶段才暴露。
func decodeAESKey(encodingAESKey string) ([]byte, error) {
	s := strings.TrimSpace(encodingAESKey)
	if strings.ContainsAny(s, "\r\n \t") {
		return nil, fmt.Errorf("wechat: encoding_aes_key 含空白字符（请去掉换行）")
	}
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("wechat: encoding_aes_key 不是合法 Base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("wechat: encoding_aes_key 解码后应为 32 字节，实际 %d 字节", len(key))
	}
	return key, nil
}

// Signature 计算回调签名：sha1(sort(token, timestamp, nonce, encrypt))。
func (c *WXCrypt) Signature(timestamp, nonce, encrypt string) string {
	return signature(c.token, timestamp, nonce, encrypt)
}

// Verify 校验回调签名。
//
// 比较使用常量时间比较：签名由 token 派生，逐字节短路会泄露与真实签名的
// 公共前缀长度。
func (c *WXCrypt) Verify(msgSignature, timestamp, nonce, encrypt string) error {
	want := signature(c.token, timestamp, nonce, encrypt)
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(msgSignature)), []byte(want)) != 1 {
		return errSignature
	}
	return nil
}

// signature 实现官方的 sha1(sort(token,timestamp,nonce,encrypt))。
func signature(token, timestamp, nonce, encrypt string) string {
	parts := []string{token, timestamp, nonce, encrypt}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

// VerifyURL 处理 GET echostr 验证：验签并解出明文。
//
// 企业微信要求 1 秒内原样返回明文（不加引号、不带 BOM、不带换行）。
func (c *WXCrypt) VerifyURL(msgSignature, timestamp, nonce, echostr string) (string, error) {
	if err := c.Verify(msgSignature, timestamp, nonce, echostr); err != nil {
		return "", err
	}
	msg, _, err := c.decrypt(echostr)
	if err != nil {
		return "", err
	}
	return msg, nil
}

// DecryptMessage 验签并解密一条回调报文，返回明文（JSON 字符串）。
func (c *WXCrypt) DecryptMessage(msgSignature, timestamp, nonce, encrypt string) (string, error) {
	if err := c.Verify(msgSignature, timestamp, nonce, encrypt); err != nil {
		return "", err
	}
	msg, _, err := c.decrypt(encrypt)
	if err != nil {
		return "", err
	}
	return msg, nil
}

// EncryptMessage 加密一条被动回复，返回可直接写入响应体的 JSON。
//
// nonce 必须复用回调 URL 里的 nonce（官方要求），timestamp 由调用方生成。
func (c *WXCrypt) EncryptMessage(reply, nonce, timestamp string) (string, error) {
	encrypt, err := c.encrypt(reply)
	if err != nil {
		return "", err
	}
	sig := c.Signature(timestamp, nonce, encrypt)
	// 字段名按智能机器人 JSON 协议（msgsignature，全小写无下划线）。
	body, err := jsonx.Marshal(struct {
		Encrypt      string `json:"encrypt"`
		MsgSignature string `json:"msgsignature"`
		Timestamp    string `json:"timestamp"`
		Nonce        string `json:"nonce"`
	}{encrypt, sig, timestamp, nonce})
	if err != nil {
		return "", fmt.Errorf("wechat: 构造加密回复失败: %w", err)
	}
	return string(body), nil
}

// decrypt 解密并解析 receiveid。
func (c *WXCrypt) decrypt(encrypt string) (msg string, receiveID string, err error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encrypt))
	if err != nil {
		return "", "", fmt.Errorf("%w: Base64 解码失败: %v", errDecrypt, err)
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return "", "", fmt.Errorf("%w: 密文长度 %d 不是 %d 的整数倍", errDecrypt, len(raw), aes.BlockSize)
	}

	plain, err := decryptCBC(c.key, raw)
	if err != nil {
		return "", "", err
	}
	plain, err = unpad(plain, wecomPadBlockSize)
	if err != nil {
		return "", "", err
	}
	if len(plain) < randomPrefixLen+msgLenFieldLen {
		return "", "", fmt.Errorf("%w: 明文长度 %d 过短", errDecrypt, len(plain))
	}
	body := plain[randomPrefixLen:]
	msgLen := int(binary.BigEndian.Uint32(body[:msgLenFieldLen]))
	body = body[msgLenFieldLen:]
	if msgLen < 0 || msgLen > len(body) {
		return "", "", fmt.Errorf("%w: msg_len=%d 超出明文长度 %d", errDecrypt, msgLen, len(body))
	}
	msg = string(body[:msgLen])
	// receiveid 转成字符串再做常量时间比较：长度不同时 ConstantTimeCompare
	// 会直接返回 0，不会泄露内容，但提前统一类型避免调用方误用 []byte。
	receiveID = string(body[msgLen:])
	if c.receiveID != "" && receiveID != c.receiveID {
		return "", "", fmt.Errorf("%w: 期望 %q，实际 %q", errReceiveID, c.receiveID, receiveID)
	}
	return msg, receiveID, nil
}

// encrypt 按企业微信明文结构加密。
func (c *WXCrypt) encrypt(msg string) (string, error) {
	msgBytes := []byte(msg)
	plain := make([]byte, 0, randomPrefixLen+msgLenFieldLen+len(msgBytes)+len(c.receiveID)+wecomPadBlockSize)
	prefix := make([]byte, randomPrefixLen)
	if _, err := rand.Read(prefix); err != nil {
		return "", fmt.Errorf("wechat: 生成随机前缀失败: %w", err)
	}
	plain = append(plain, prefix...)
	var lenBuf [msgLenFieldLen]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(msgBytes)))
	plain = append(plain, lenBuf[:]...)
	plain = append(plain, msgBytes...)
	plain = append(plain, c.receiveID...)

	plain = pad(plain, wecomPadBlockSize)
	out, err := encryptCBC(c.key, plain)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(out), nil
}

// DecryptMedia 用单条消息自带的 aeskey 解密媒体文件。
//
// 与回调报文不同，媒体链接的 aeskey **每条链接唯一**，且不再做 receiveid 校验
// （文件流里没有这层结构，直接是密文本体）。aeskey 同样是去掉 "=" 的 Base64。
//
// aesKey 为空时原样返回 data：部分老报文/未加密资源会直接给明文，
// 此时强行解密只会把好数据弄坏。
func DecryptMedia(data []byte, aesKey string) ([]byte, error) {
	if aesKey == "" {
		return data, nil
	}
	key, err := decodeAESKey(aesKey)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	// 密文必须对齐到 16 字节：WXBizMsgCrypt 的各类 SDK 在遇到不对齐数据时
	// 会先补零再解密，这里保持一致，避免个别实现产出的密文整体解不出来。
	if rem := len(data) % aes.BlockSize; rem != 0 {
		padded := make([]byte, len(data)+aes.BlockSize-rem)
		copy(padded, data)
		data = padded
	}
	plain, err := decryptCBC(key, data)
	if err != nil {
		return nil, err
	}
	// 媒体填充同样是 32 字节块；填充值非法时（说明其实没填充）原样返回。
	if unpadded, err := unpad(plain, wecomPadBlockSize); err == nil {
		return unpadded, nil
	}
	return plain, nil
}

// decryptCBC 做 AES-256-CBC 解密，IV 取 key 前 16 字节。
func decryptCBC(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errDecrypt, err)
	}
	if len(data)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("%w: 数据长度 %d 不是块大小的整数倍", errDecrypt, len(data))
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, key[:block.BlockSize()]).CryptBlocks(out, data)
	return out, nil
}

// encryptCBC 做 AES-256-CBC 加密，IV 取 key 前 16 字节。
func encryptCBC(key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("wechat: 初始化 AES: %w", err)
	}
	if len(plain)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("wechat: 明文长度 %d 不是块大小的整数倍", len(plain))
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key[:block.BlockSize()]).CryptBlocks(out, plain)
	return out, nil
}

// pad 按 PKCS#7 填充到 size 的整数倍（size 可为 32）。
//
// 与标准 PKCS#7 一致：**总是**追加填充，即使长度已经是 size 的整数倍
// （此时追加整整一个块）。少追加这一块会让明文长度恰好对齐的消息在解密侧
// 把尾部数据当成填充剥掉。
func pad(data []byte, size int) []byte {
	n := size - len(data)%size
	padding := make([]byte, n)
	for i := range padding {
		padding[i] = byte(n)
	}
	return append(data, padding...)
}

// unpad 去掉 PKCS#7 填充。
//
// 企业微信的填充块大小是 32，因此填充值可能大于 16：标准库的
// cipher.NewCBCDecrypter+CryptBlocks 不会替我们处理，必须自己按 1..32 校验。
func unpad(data []byte, size int) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: 明文为空", errDecrypt)
	}
	n := int(data[len(data)-1])
	if n == 0 || n > size || n > len(data) {
		return nil, fmt.Errorf("%w: 非法填充长度 %d", errDecrypt, n)
	}
	for _, b := range data[len(data)-n:] {
		if int(b) != n {
			return nil, fmt.Errorf("%w: 填充字节不一致", errDecrypt)
		}
	}
	return data[:len(data)-n], nil
}
