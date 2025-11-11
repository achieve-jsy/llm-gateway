package handler

import (
	"encoding/base64"
	"net/http"

	"github.com/example/llm-gateway/internal/auth/crypto"
	"github.com/gin-gonic/gin"
)

type JWKSHandler struct {
	manager *crypto.JWTManager
}

func NewJWKSHandler() *JWKSHandler {
	mgr, _ := crypto.NewJWTManagerFromEnv()
	return &JWKSHandler{manager: mgr}
}

func encodeBigIntBytes(v []byte) string {
	return base64.RawURLEncoding.EncodeToString(v)
}

func (h *JWKSHandler) GetJWKS(c *gin.Context) {
	if h.manager == nil {
		c.JSON(http.StatusOK, gin.H{"keys": []gin.H{}})
		return
	}
	pub := h.manager.PublicKey()
	if pub == nil {
		c.JSON(http.StatusOK, gin.H{"keys": []gin.H{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"keys": []gin.H{{
			"kty": "RSA",
			"kid": h.manager.KeyID(),
			"alg": "RS256",
			"use": "sig",
			"n":   encodeBigIntBytes(pub.N.Bytes()),
			"e":   encodeBigIntBytes([]byte{1, 0, 1}),
		}},
	})
}
