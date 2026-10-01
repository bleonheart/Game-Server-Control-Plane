package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

type Cipher struct {
	aead cipher.AEAD
}

func New(key []byte) (*Cipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(value string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := c.aead.Seal(nil, nonce, []byte(value), nil)
	return append(nonce, sealed...), nil
}

func (c *Cipher) Decrypt(value []byte) (string, error) {
	if len(value) < c.aead.NonceSize() {
		return "", fmt.Errorf("invalid encrypted value")
	}
	nonce := value[:c.aead.NonceSize()]
	plain, err := c.aead.Open(nil, nonce, value[c.aead.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
