package scheduler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

// ExtractSessionKey resolves session key from request with two-tier fallback:
// 1. conversation_id from req.ExtraFields (client-provided)
// 2. hash(KeyID) from UserIdentity (authentication-based)
//
// Returns empty string if neither is available.
func ExtractSessionKey(c *gin.Context, req *core.ChatRequest) string {
	// Priority 1: conversation_id from client
	if req != nil && !req.ExtraFields.IsEmpty() {
		// Try 1a: conversation_id directly in top-level unknown fields
		convIDRaw := req.ExtraFields.Lookup("conversation_id")
		if len(convIDRaw) > 0 {
			var convID string
			if err := json.Unmarshal(convIDRaw, &convID); err == nil && convID != "" {
				return convID
			}
		}

		// Try 1b: conversation_id inside extra_fields object
		extraFieldsRaw := req.ExtraFields.Lookup("extra_fields")
		if len(extraFieldsRaw) > 0 {
			var extraObj map[string]json.RawMessage
			if err := json.Unmarshal(extraFieldsRaw, &extraObj); err == nil {
				if convIDRaw, ok := extraObj["conversation_id"]; ok {
					var convID string
					if err := json.Unmarshal(convIDRaw, &convID); err == nil && convID != "" {
						return convID
					}
				}
			}
		}
	}

	// Priority 2: hash(KeyID) from UserIdentity
	identityVal, exists := c.Get("identity")
	if !exists {
		return "" // No session key available
	}

	identity, ok := identityVal.(*model.UserIdentity)
	if !ok || identity.KeyID == "" {
		return ""
	}

	// Hash KeyID for privacy and length normalization
	h := sha256.Sum256([]byte(identity.KeyID))
	// Use first 16 bytes = 32 hex chars (sufficient uniqueness, Redis-friendly)
	return hex.EncodeToString(h[:16])
}
