package miniapp

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"testing"
)

func TestNewCrypto(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("1234567890123456"))
	c, err := NewCrypto(key)
	if err != nil {
		t.Fatalf("NewCrypto failed: %v", err)
	}
	if c == nil {
		t.Fatal("NewCrypto returned nil")
	}
}

func TestNewCryptoInvalidBase64(t *testing.T) {
	_, err := NewCrypto("not-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestNewCryptoInvalidKeyLength(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err := NewCrypto(key)
	if !errors.Is(err, ErrInvalidKeyLength) {
		t.Errorf("expected ErrInvalidKeyLength, got %v", err)
	}
}

func TestNewCryptoValidLengths(t *testing.T) {
	for _, l := range []int{16, 24, 32} {
		key := base64.StdEncoding.EncodeToString(make([]byte, l))
		c, err := NewCrypto(key)
		if err != nil {
			t.Errorf("NewCrypto with %d-byte key failed: %v", l, err)
		}
		if c == nil {
			t.Errorf("NewCrypto with %d-byte key returned nil", l)
		}
	}
}

func TestVerifySignature(t *testing.T) {
	sessionKey := []byte("1234567890123456")
	key := base64.StdEncoding.EncodeToString(sessionKey)
	c, _ := NewCrypto(key)

	rawData := `{"openId":"test"}`
	mac := hmac.New(sha1.New, sessionKey)
	mac.Write([]byte(rawData))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	err := c.VerifySignature(rawData, sig)
	if err != nil {
		t.Errorf("VerifySignature failed: %v", err)
	}
}

func TestVerifySignatureInvalid(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("1234567890123456"))
	c, _ := NewCrypto(key)

	err := c.VerifySignature(`{"openId":"test"}`, "aW52YWxpZA==")
	if err == nil {
		t.Error("expected error for invalid signature")
	}
}

func TestVerifySignatureBadBase64(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("1234567890123456"))
	c, _ := NewCrypto(key)

	err := c.VerifySignature("data", "!!!invalid-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64 signature")
	}
}

func TestDecryptDataInvalidEncryptedData(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("1234567890123456"))
	c, _ := NewCrypto(key)

	_, err := c.DecryptData("!!!invalid!!!", base64.StdEncoding.EncodeToString([]byte("1234567890123456")))
	if err == nil {
		t.Error("expected error for invalid base64 encryptedData")
	}
}

func TestDecryptDataInvalidIV(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("1234567890123456"))
	c, _ := NewCrypto(key)

	_, err := c.DecryptData(base64.StdEncoding.EncodeToString([]byte("test")), "!!!invalid!!!")
	if err == nil {
		t.Error("expected error for invalid base64 iv")
	}
}

func TestDecryptDataShortIV(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("1234567890123456"))
	c, _ := NewCrypto(key)

	iv := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err := c.DecryptData(base64.StdEncoding.EncodeToString([]byte("testdata")), iv)
	if !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("expected ErrDecryptFailed, got %v", err)
	}
}

func TestDecryptDataInvalidCiphertextLength(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("1234567890123456"))
	c, _ := NewCrypto(key)

	ct := base64.StdEncoding.EncodeToString([]byte("short"))
	iv := base64.StdEncoding.EncodeToString([]byte("1234567890123456"))
	_, err := c.DecryptData(ct, iv)
	if !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("expected ErrDecryptFailed, got %v", err)
	}
}

func TestPkcs7UnpadEmpty(t *testing.T) {
	got := pkcs7Unpad([]byte{})
	if got != nil {
		t.Error("expected nil for empty data")
	}
}

func TestPkcs7UnpadInvalidPadLen(t *testing.T) {
	got := pkcs7Unpad([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17})
	if got != nil {
		t.Error("expected nil for invalid padding")
	}
}

func TestPkcs7UnpadValid(t *testing.T) {
	data := bytesRepeat(0x10, 16)
	got := pkcs7Unpad(data)
	if got == nil {
		t.Fatal("expected non-nil for valid padding")
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %d bytes", len(got))
	}
}

func TestPkcs7UnpadCorrect(t *testing.T) {
	// 3 bytes of "abc" + 13 bytes of padding (0x0D)
	data := append([]byte("abc"), bytesRepeat(0x0D, 13)...)
	got := pkcs7Unpad(data)
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if string(got) != "abc" {
		t.Errorf("expected %q, got %q", "abc", string(got))
	}
}

func TestPkcs7UnpadCorrupted(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 17}
	got := pkcs7Unpad(data)
	if got != nil {
		t.Error("expected nil for corrupted padding (padLen > BlockSize)")
	}
}

func TestPkcs7UnpadCorruptedMismatch(t *testing.T) {
	data := []byte{0x05, 0x05, 0x05, 0x05, 0x05, 0x04}
	got := pkcs7Unpad(data)
	if got != nil {
		t.Error("expected nil for corrupted padding (bytes don't match padLen)")
	}
}

func TestErrors(t *testing.T) {
	if ErrDecryptFailed.Error() != "miniapp: decrypt failed" {
		t.Errorf("unexpected error message: %q", ErrDecryptFailed.Error())
	}
	if ErrInvalidSignature.Error() != "miniapp: invalid signature" {
		t.Errorf("unexpected error message: %q", ErrInvalidSignature.Error())
	}
	if ErrInvalidKeyLength.Error() != "miniapp: invalid session key length" {
		t.Errorf("unexpected error message: %q", ErrInvalidKeyLength.Error())
	}
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
