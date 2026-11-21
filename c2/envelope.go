package c2

// 信封：AES-256-GCM。密钥由植入端与平台共享的 base64 32 字节随机种子表示；
// 握手时明文帧只带 token 与该种子，之后所有任务/结果帧都走密文。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

type Envelope struct {
	key []byte // 32 字节 AES 密钥
}

// NewKeyBase64 生成一枚新的随机信封密钥（base64 编码，供植入端内嵌）。
func NewKeyBase64() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// ParseKey 解析握手带来的 base64 密钥。
func ParseKey(s string) (*Envelope, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("c2 密钥不是合法 base64: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("c2 密钥长度必须是 32 字节，实际 %d", len(b))
	}
	return &Envelope{key: b}, nil
}

// NewEnvelope 由口令种子派生信封（便于运维从 token 派生，可复现）。
func NewEnvelope(seed string) *Envelope {
	sum := sha256.Sum256([]byte("artex/c2/envelope/v1\x00" + seed))
	return &Envelope{key: sum[:]}
}

func (e *Envelope) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal 输出 nonce|ciphertext。
func (e *Envelope) Seal(plain []byte) ([]byte, error) {
	g, err := e.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, plain, nil), nil
}

// Open 校验并解出明文。
func (e *Envelope) Open(blob []byte) ([]byte, error) {
	g, err := e.gcm()
	if err != nil {
		return nil, err
	}
	if len(blob) < g.NonceSize() {
		return nil, fmt.Errorf("c2 密文过短")
	}
	plain, err := g.Open(nil, blob[:g.NonceSize()], blob[g.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("c2 解密失败（密钥不符或被篡改）: %w", err)
	}
	return plain, nil
}
