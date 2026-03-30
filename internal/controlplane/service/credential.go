package service

import (
	"encoding/json"
	"strings"
)

func extractAPIKey(credentialJSON string) string {
	var m map[string]string
	if json.Unmarshal([]byte(credentialJSON), &m) == nil {
		// aksk 模式必须保留完整 JSON，不能抽成 api_key
		switch strings.ToLower(strings.TrimSpace(m["auth_type"])) {
		case "aksk", "iam", "hmac":
			return credentialJSON
		}
		if key, ok := m["api_key"]; ok && key != "" {
			return key
		}
	}
	return credentialJSON
}
