package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"

	jwt "github.com/golang-jwt/jwt/v5"
)

type JWTManager struct {
	privateKey *rsa.PrivateKey
	keyID      string
}

func NewJWTManagerFromEnv() (*JWTManager, error) {
	pemValue := os.Getenv("AUTH_JWT_PRIVATE_KEY_PEM")
	if pemValue == "" {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, err
		}
		return &JWTManager{privateKey: key, keyID: "dev-key-1"}, nil
	}
	block, _ := pem.Decode([]byte(pemValue))
	if block == nil {
		return nil, errors.New("invalid PEM private key")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, err
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("private key is not RSA")
		}
		key = rsaKey
	}
	return &JWTManager{privateKey: key, keyID: "active-key-1"}, nil
}

func (m *JWTManager) PublicKeyPEM() (string, error) {
	pubASN1, err := x509.MarshalPKIXPublicKey(&m.privateKey.PublicKey)
	if err != nil {
		return "", err
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubASN1})
	return string(block), nil
}

func (m *JWTManager) KeyID() string { return m.keyID }

func (m *JWTManager) SigningMethod() *jwt.SigningMethodRSA { return jwt.SigningMethodRS256 }

func (m *JWTManager) PublicKey() *rsa.PublicKey {
	if m == nil || m.privateKey == nil {
		return nil
	}
	return &m.privateKey.PublicKey
}

func (m *JWTManager) PrivateKey() *rsa.PrivateKey {
	if m == nil {
		return nil
	}
	return m.privateKey
}
