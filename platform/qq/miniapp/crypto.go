// Package miniapp 提供 QQ 频道小程序（MiniApp）开放数据加解密与签名验证工具。
//
// 适用场景：
//   - 小程序前端通过 qq.getGuildInfo() 等接口获取加密数据后，
//     将 encryptedData / iv / signature 传给开发者后台，
//     后台使用本包进行签名验证和数据解密。
//
// 参考文档：
//   - 开放数据域加密：
//     https://bot.q.qq.com/wiki/develop/api-v2/server-inter/channel/miniapp/opendata.html
//   - QQ 游戏开放数据签名与解密：
//     https://q.qq.com/wiki/develop/game/frame/open-ability/signature.html
package miniapp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
)

var (
	ErrDecryptFailed    = errors.New("miniapp: decrypt failed")
	ErrInvalidSignature = errors.New("miniapp: invalid signature")
	ErrInvalidKeyLength = errors.New("miniapp: invalid session key length")
)

// Crypto 是 QQ 频道小程序开放数据的加解密器。
//
// 使用前必须设置 SessionKey（通过 QQ 登录流程获取）。
type Crypto struct {
	sessionKey []byte
}

// NewCrypto 创建一个使用 sessionKey 的加解密器。
// sessionKey 为 QQ 登录流程中获取的会话密钥（base64 编码字符串）。
func NewCrypto(sessionKey string) (*Crypto, error) {
	key, err := base64.StdEncoding.DecodeString(sessionKey)
	if err != nil {
		return nil, err
	}
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}
	return &Crypto{sessionKey: key}, nil
}

// VerifySignature 验证原始数据 rawData 的签名是否与 signature 匹配。
//
//   - rawData：小程序前端传来的原始数据字符串
//   - signature：小程序前端传来的签名（base64 编码）
//   - 内部使用 sessionKey 通过 HMAC-SHA1 重新计算签名并比对
//
// 参考：https://q.qq.com/wiki/develop/game/frame/open-ability/signature.html#%E6%95%B0%E6%8D%AE%E7%AD%BE%E5%90%8D%E6%A0%A1%E9%AA%8C
func (c *Crypto) VerifySignature(rawData, signature string) error {
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return err
	}
	mac := hmac.New(sha1.New, c.sessionKey)
	mac.Write([]byte(rawData))
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return ErrInvalidSignature
	}
	return nil
}

// DecryptData 解密小程序前端返回的加密数据。
//
//   - encryptedData：小程序前端返回的加密数据（base64 编码）
//   - iv：对称解密算法初始向量（base64 编码）
//   - 返回解密后的原始 JSON 字节
//
// 解密算法：AES-128-CBC + PKCS7 填充。
//
// 参考：https://q.qq.com/wiki/develop/game/frame/open-ability/signature.html#%E6%95%B0%E6%8D%AE%E8%A7%A3%E5%AF%86
func (c *Crypto) DecryptData(encryptedData, iv string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedData)
	if err != nil {
		return nil, err
	}
	ivBytes, err := base64.StdEncoding.DecodeString(iv)
	if err != nil {
		return nil, err
	}
	if len(ivBytes) != aes.BlockSize {
		return nil, ErrDecryptFailed
	}
	block, err := aes.NewCipher(c.sessionKey)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, ErrDecryptFailed
	}
	mode := cipher.NewCBCDecrypter(block, ivBytes)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)
	plaintext = pkcs7Unpad(plaintext)
	if plaintext == nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

// pkcs7Unpad 移除 PKCS7 填充。
func pkcs7Unpad(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	padLen := int(data[len(data)-1])
	if padLen <= 0 || padLen > aes.BlockSize || padLen > len(data) {
		return nil
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if int(data[i]) != padLen {
			return nil
		}
	}
	return data[:len(data)-padLen]
}
