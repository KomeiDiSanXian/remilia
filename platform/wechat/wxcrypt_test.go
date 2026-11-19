package wechat

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ────────────────────────────────────────────────────────────────────────────
// 加解密
// ────────────────────────────────────────────────────────────────────────────

// testAESKey 是 43 位 EncodingAESKey（官方要求 a-zA-Z0-9 共 62 选 43 位）。
const testAESKey = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"

// newTestCrypt 构造一个测试用加解密器。
func newTestCrypt(t *testing.T) *WXCrypt {
	t.Helper()
	c, err := newWXCrypt("test-token", testAESKey, "")
	if err != nil {
		t.Fatalf("newWXCrypt: %v", err)
	}
	return c
}

func TestWXCryptEncryptDecryptRoundTrip(t *testing.T) {
	c := newTestCrypt(t)
	const plain = `{"msgtype":"text","text":{"content":"@RobotA hello"}}`

	encrypted, err := c.encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, receiveID, err := c.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("明文不一致:\n got=%q\nwant=%q", got, plain)
	}
	if receiveID != "" {
		t.Fatalf("智能机器人的 receiveid 应为空，实际 %q", receiveID)
	}
}

// 中文明文跨块边界：确保 32 字节填充不会切坏 UTF-8。
func TestWXCryptRoundTripMultibyte(t *testing.T) {
	c := newTestCrypt(t)
	for _, size := range []int{1, 15, 16, 17, 31, 32, 33, 100} {
		plain := ""
		for range size {
			plain += "汉"
		}
		encrypted, err := c.encrypt(plain)
		if err != nil {
			t.Fatalf("size=%d encrypt: %v", size, err)
		}
		got, _, err := c.decrypt(encrypted)
		if err != nil {
			t.Fatalf("size=%d decrypt: %v", size, err)
		}
		if got != plain {
			t.Fatalf("size=%d 明文不一致", size)
		}
	}
}

func TestWXCryptSignature(t *testing.T) {
	c := newTestCrypt(t)
	// 签名 = sha1(sort(token, timestamp, nonce, encrypt))
	const ts, nonce, enc = "13500001234", "123412323", "ENC"
	sig := c.Signature(ts, nonce, enc)
	if len(sig) != 40 {
		t.Fatalf("sha1 hex 长度应为 40，实际 %d", len(sig))
	}
	if err := c.Verify(sig, ts, nonce, enc); err != nil {
		t.Fatalf("正确签名应通过: %v", err)
	}
	if err := c.Verify("deadbeef", ts, nonce, enc); err == nil {
		t.Fatal("错误签名应被拒绝")
	}
	// 大小写不敏感：官方给的是小写，但校验不应因大小写失败。
	if err := c.Verify(upper(sig), ts, nonce, enc); err != nil {
		t.Fatalf("大写签名应通过: %v", err)
	}
}

func upper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}

func TestWXCryptVerifyURL(t *testing.T) {
	c := newTestCrypt(t)
	const echo = "hello-echo"
	enc, err := c.encrypt(echo)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	const ts, nonce = "1409659813", "1372623149"
	sig := c.Signature(ts, nonce, enc)

	got, err := c.VerifyURL(sig, ts, nonce, enc)
	if err != nil {
		t.Fatalf("VerifyURL: %v", err)
	}
	if got != echo {
		t.Fatalf("echostr 明文 = %q, want %q", got, echo)
	}
}

func TestWXCryptBadKey(t *testing.T) {
	if _, err := newWXCrypt("t", "short", ""); err == nil {
		t.Fatal("非 43 位密钥应报错")
	}
	if _, err := newWXCrypt("t", "", ""); err == nil {
		t.Fatal("空密钥应报错")
	}
	// 带换行的密钥：常见的手工复制错误，必须给出可读错误而不是解密乱码。
	if _, err := newWXCrypt("t", testAESKey[:20]+"\n"+testAESKey[20:], ""); err == nil {
		t.Fatal("含换行的密钥应报错")
	}
}

func TestWXCryptDecryptWrongKey(t *testing.T) {
	c1 := newTestCrypt(t)
	const other = "ZYXWVUTSRQPONMLKJIHGFEDCBA9876543210zyxwvut"
	c2, err := newWXCrypt("test-token", other, "")
	if err != nil {
		t.Fatalf("newWXCrypt: %v", err)
	}
	enc, err := c1.encrypt("secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, _, err := c2.decrypt(enc); err == nil {
		t.Fatal("用错误密钥解密应失败")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 配置
// ────────────────────────────────────────────────────────────────────────────

func TestConfigResolveMode(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"默认 ws", Config{}, ModeWS},
		{"显式 ws", Config{Mode: ModeWS}, ModeWS},
		{"显式 webhook", Config{Mode: ModeWebhook}, ModeWebhook},
		{"有 secret 走 ws", Config{Secret: "s"}, ModeWS},
		{"只有 token 走 webhook", Config{Token: "t"}, ModeWebhook},
		{"只有 aeskey 走 webhook", Config{EncodingAESKey: "k"}, ModeWebhook},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ResolveMode(); got != tt.want {
				t.Fatalf("ResolveMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewAdapterValidation(t *testing.T) {
	if _, err := NewAdapter(Config{}); err == nil {
		t.Fatal("缺少 BotID 应报错")
	}
	if _, err := NewAdapter(Config{BotID: "bot"}); err == nil {
		t.Fatal("ws 模式缺少 Secret 应报错")
	}
	if _, err := NewAdapter(Config{BotID: "bot", Secret: "s"}); err != nil {
		t.Fatalf("合法 ws 配置不应报错: %v", err)
	}
	if _, err := NewAdapter(Config{BotID: "bot", Mode: ModeWebhook, Token: "t"}); err == nil {
		t.Fatal("webhook 模式缺少 encoding_aes_key 应报错")
	}
	if _, err := NewAdapter(Config{
		BotID: "bot", Mode: ModeWebhook, Token: "t", EncodingAESKey: testAESKey,
	}); err == nil {
		t.Fatal("webhook 模式缺少 listen_addr 应报错")
	}
	a, err := NewAdapter(Config{
		BotID:          "bot",
		Mode:           ModeWebhook,
		Token:          "t",
		EncodingAESKey: testAESKey,
		Webhook:        WebhookConfig{ListenAddr: ":0"},
	})
	if err != nil {
		t.Fatalf("合法 webhook 配置不应报错: %v", err)
	}
	if a.Capabilities().FileUpload {
		t.Fatal("webhook 模式不应声明 FileUpload（没有素材上传接口）")
	}
}

func TestAdapterCapabilities(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	caps := a.Capabilities()
	if !caps.Markdown || !caps.Buttons || !caps.FileUpload {
		t.Fatalf("长连接模式应支持 markdown/buttons/file_upload: %+v", caps)
	}
	if caps.MessageEdit || caps.MessageDelete || caps.Reactions {
		t.Fatalf("企业微信不支持编辑/撤回/表情回应: %+v", caps)
	}
	if caps.MaxTextLength != 20480 {
		t.Fatalf("MaxTextLength = %d, want 20480", caps.MaxTextLength)
	}
	// 能力声明与可选接口必须一致（与 platform/consistency_test.go 同口径）。
	sender := a.Sender()
	if _, ok := sender.(platform.MessageEditor); ok {
		t.Fatal("未声明 MessageEdit 却实现了 MessageEditor")
	}
	if _, ok := sender.(platform.MessageDeleter); ok {
		t.Fatal("未声明 MessageDelete 却实现了 MessageDeleter")
	}
	if _, ok := sender.(platform.ReactionSender); ok {
		t.Fatal("未声明 Reactions 却实现了 ReactionSender")
	}
	if _, ok := sender.(platform.TypingNotifier); ok {
		t.Fatal("未声明 TypingIndicator 却实现了 TypingNotifier")
	}
	if _, ok := sender.(platform.SessionNotifier); !ok {
		t.Fatal("应实现 SessionNotifier（长连接支持主动推送）")
	}
}

func TestAdapterBotIdentity(t *testing.T) {
	a, err := NewAdapter(Config{BotID: "bot-123", Secret: "s"})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if got := a.BotID(); got != "bot-123" {
		t.Fatalf("BotID() = %q", got)
	}
	if got := a.BotName(); got != "" {
		t.Fatalf("BotName() 应为空（平台无资料接口），实际 %q", got)
	}
	detail := a.HealthDetail()
	if detail["connection"] != ModeWS {
		t.Fatalf("HealthDetail connection = %v", detail["connection"])
	}
}
