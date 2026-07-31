package webhook

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeriveKey_MatchesOfficialGenerateKey 验证重构后的 deriveKey 与腾讯官方
// SDK 的派生方式（ed25519.GenerateKey(strings.NewReader(seed))）逐字节一致。
// 覆盖：短 secret、恰好 32 字节、超长 secret。
func TestDeriveKey_MatchesOfficialGenerateKey(t *testing.T) {
	secrets := []string{
		"secret",
		"83yuqmieaXUROLIGECA8765433333345",
		strings.Repeat("a", ed25519.SeedSize),
		strings.Repeat("b", 40),
	}
	for _, secret := range secrets {
		t.Run("secret", func(t *testing.T) {
			got := deriveKey(secret)

			// 官方 seed 扩展规则：不断加倍直到 ≥ 32，再截断到 32 字节
			seed := secret
			for len(seed) < ed25519.SeedSize {
				seed = strings.Repeat(seed, 2)
			}
			seed = seed[:ed25519.SeedSize]

			officialPub, officialPri, err := ed25519.GenerateKey(strings.NewReader(seed))
			require.NoError(t, err)
			assert.Equal(t, officialPub, got.publicKey)
			assert.Equal(t, officialPri, got.privateKey)
		})
	}
}

// TestDeriveKey_Deterministic 验证同一 secret 派生出的密钥稳定一致。
func TestDeriveKey_Deterministic(t *testing.T) {
	a := deriveKey("same-secret")
	b := deriveKey("same-secret")
	assert.Equal(t, a.publicKey, b.publicKey)
	assert.Equal(t, a.privateKey, b.privateKey)
}

// TestConn_SignVerify_RoundTrip 验证 Sign/Verify 往返成功，篡改 body 后失败。
func TestConn_SignVerify_RoundTrip(t *testing.T) {
	c := NewWebhook(&dto.BotInfo{AppSecret: "test-secret"})

	header := http.Header{HeaderTimestamp: []string{strconv.FormatInt(time.Now().Unix(), 10)}}
	body := []byte(`{"op":0}`)

	sig, err := c.Sign(header, body)
	require.NoError(t, err)
	header.Set(HeaderSignature, hex.EncodeToString(sig))

	ok, err := c.Verify(header, body)
	require.NoError(t, err)
	assert.True(t, ok)

	// 篡改 body 后验签必须失败
	ok, err = c.Verify(header, []byte(`{"op":1}`))
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestConn_Verify_NoKey 验证 nil BotInfo / 空 AppSecret 时返回明确错误而非 panic。
func TestConn_Verify_NoKey(t *testing.T) {
	t.Run("nil info", func(t *testing.T) {
		c := NewWebhook(nil)
		_, err := c.Verify(http.Header{HeaderTimestamp: []string{strconv.FormatInt(time.Now().Unix(), 10)}}, []byte(`{}`))
		require.Error(t, err)
	})

	t.Run("empty secret", func(t *testing.T) {
		c := NewWebhook(&dto.BotInfo{AppSecret: ""})
		_, err := c.Verify(http.Header{HeaderTimestamp: []string{strconv.FormatInt(time.Now().Unix(), 10)}}, []byte(`{}`))
		require.Error(t, err)
	})
}

// TestConn_ConcurrentSignVerify 验证缓存的密钥在 net/http 并发请求下无数据竞争。
// 配合 go test -race 运行。
func TestConn_ConcurrentSignVerify(t *testing.T) {
	c := NewWithBuffer(&dto.BotInfo{AppSecret: "concurrent-secret"}, 10)

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			header := http.Header{HeaderTimestamp: []string{strconv.FormatInt(time.Now().Unix(), 10)}}
			body := []byte(`{}`)
			sig, err := c.Sign(header, body)
			if err != nil {
				t.Errorf("Sign failed: %v", err)
				return
			}
			header.Set(HeaderSignature, hex.EncodeToString(sig))
			ok, err := c.Verify(header, body)
			if err != nil {
				t.Errorf("Verify failed: %v", err)
				return
			}
			if !ok {
				t.Error("Verify returned false for a valid signature")
			}
		})
	}
	wg.Wait()
}

// TestDecodeSign 验证签名头解码与格式校验。
func TestDecodeSign(t *testing.T) {
	c := NewWebhook(nil)

	_, err := c.decodeSign("")
	assert.Error(t, err)

	_, err = c.decodeSign("zz")
	assert.Error(t, err)

	_, err = c.decodeSign("abcd")
	assert.Error(t, err)

	sig := make([]byte, ed25519.SignatureSize)
	_, _ = rand.Read(sig)
	sig[63] &= 0x1f // 清理高位，符合签名规范（实际签名的后 3 bit 固定为 0）
	got, err := c.decodeSign(hex.EncodeToString(sig))
	assert.NoError(t, err)
	assert.Equal(t, sig, got)
}

// TestCheckTimestampSkew 验证时间戳重放窗口：过期时间戳被拒，无法解析的格式保守放行。
func TestCheckTimestampSkew(t *testing.T) {
	assert.NoError(t, checkTimestampSkew(strconv.FormatInt(time.Now().Unix(), 10)))
	assert.NoError(t, checkTimestampSkew(time.Now().UTC().Format(time.RFC3339Nano)))

	old := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	assert.Error(t, checkTimestampSkew(old))

	assert.NoError(t, checkTimestampSkew("not-a-timestamp"))
}
