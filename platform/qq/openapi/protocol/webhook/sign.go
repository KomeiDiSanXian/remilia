package webhook

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

const (
	HeaderSignature = "X-Signature-Ed25519"
	HeaderTimestamp = "X-Signature-Timestamp"
)

type ed25519Key struct {
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
}

func (c *Conn) genSeed() (string, error) {
	if c.info.AppSecret == "" {
		return "", fmt.Errorf("app secret is empty")
	}
	seed := c.info.AppSecret
	for len(seed) < ed25519.SeedSize {
		seed = strings.Repeat(seed, 2)
	}
	return seed[:ed25519.SeedSize], nil
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

func (c *Conn) buildMsg(timestamp string, body []byte) ([]byte, error) {
	if timestamp == "" {
		return nil, fmt.Errorf("timestamp is empty")
	}
	var msg bytes.Buffer
	_, _ = msg.WriteString(timestamp)
	_, _ = msg.Write(body)
	return msg.Bytes(), nil
}

func (c *Conn) genKey() (*ed25519Key, error) {
	seed, err := c.genSeed()
	if err != nil {
		return nil, err
	}
	pub, pri, err := ed25519.GenerateKey(strings.NewReader(seed[:ed25519.SeedSize]))
	if err != nil {
		return nil, fmt.Errorf("generate key failed: %w", err)
	}
	return &ed25519Key{
		publicKey:  pub,
		privateKey: pri,
	}, nil
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
